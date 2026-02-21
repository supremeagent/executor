package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mylxsw/asteria/log"
	"github.com/supremeagent/executor/pkg/executor"
	"github.com/supremeagent/executor/pkg/executor/claude"
	"github.com/supremeagent/executor/pkg/executor/codex"
	"github.com/supremeagent/executor/pkg/store"
	"github.com/supremeagent/executor/pkg/streaming"
)

var ErrPromptRequired = errors.New("prompt is required")
var ErrResumeUnavailable = errors.New("resume state unavailable for this session")

// ClientOptions configures SDK client behavior.
type ClientOptions struct {
	Registry      *executor.Registry
	StreamManager *streaming.Manager
	EventStore    store.EventStore
	Hooks         executor.Hooks
	Transformers  map[string]executor.EventTransformer
}

// Client is the SDK entry point for executing and managing tasks.
type Client struct {
	registry   *executor.Registry
	stream     *streaming.Manager
	store      store.EventStore
	hooks      executor.Hooks
	transforms map[string]executor.EventTransformer

	sessionsMu sync.RWMutex
	sessions   map[string]executor.Session
	requests   map[string]executor.ExecuteRequest
	resumeInfo map[string]sessionResumeInfo
}

type sessionResumeInfo struct {
	ClaudeSessionID   string
	CodexConversation string
	CodexRolloutPath  string
}

type storeCloser interface {
	Close()
}

// New creates an SDK client with built-in executors registered.
func New() *Client {
	registry := executor.NewRegistry()
	registry.Register(string(executor.ExecutorClaudeCode), claude.NewFactory())
	registry.Register(string(executor.ExecutorCodex), codex.NewFactory())

	return NewWithOptions(ClientOptions{
		Registry:      registry,
		StreamManager: streaming.NewManager(),
		EventStore:    store.NewMemoryEventStore(),
	})
}

// NewWithOptions creates an SDK client with custom dependencies.
func NewWithOptions(opts ClientOptions) *Client {
	if opts.Registry == nil {
		opts.Registry = executor.NewRegistry()
	}
	if opts.StreamManager == nil {
		opts.StreamManager = streaming.NewManager()
	}
	if opts.EventStore == nil {
		opts.EventStore = store.NewMemoryEventStore()
	}

	transforms := defaultEventTransformers()
	for name, tf := range opts.Transformers {
		if tf != nil {
			transforms[name] = tf
		}
	}

	return &Client{
		registry:   opts.Registry,
		stream:     opts.StreamManager,
		store:      opts.EventStore,
		hooks:      opts.Hooks,
		transforms: transforms,
		sessions:   make(map[string]executor.Session),
		requests:   make(map[string]executor.ExecuteRequest),
		resumeInfo: make(map[string]sessionResumeInfo),
	}
}

// NewWithRegistry creates an SDK client using custom registry and stream manager.
func NewWithRegistry(registry *executor.Registry, streamMgr *streaming.Manager) *Client {
	return NewWithOptions(ClientOptions{Registry: registry, StreamManager: streamMgr})
}

// RegisterExecutor registers a custom executor type.
func (c *Client) RegisterExecutor(name string, factory executor.Factory) {
	c.registry.Register(name, factory)
}

// Execute starts a new task.
func (c *Client) Execute(ctx context.Context, req executor.ExecuteRequest) (executor.ExecuteResponse, error) {
	if req.Prompt == "" {
		return executor.ExecuteResponse{}, ErrPromptRequired
	}
	if req.Executor == "" {
		req.Executor = executor.ExecutorClaudeCode
	}

	sessionID := uuid.New().String()
	opts := executor.Options{
		WorkingDir:                 req.WorkingDir,
		Model:                      req.Model,
		Plan:                       req.Plan,
		DangerouslySkipPermissions: !req.Plan,
		Approvals:                  req.Plan || (req.AskForApproval != "" && req.AskForApproval != "never"),
		Sandbox:                    req.Sandbox,
		Env:                        req.Env,
		AskForApproval:             req.AskForApproval,
	}

	exec, err := c.registry.CreateSession(sessionID, string(req.Executor), opts)
	if err != nil {
		return executor.ExecuteResponse{}, err
	}

	if err := exec.Start(ctx, req.Prompt, opts); err != nil {
		_ = exec.Close()
		c.registry.RemoveSession(sessionID)
		return executor.ExecuteResponse{}, err
	}

	if c.hooks.OnSessionStart != nil {
		c.hooks.OnSessionStart(ctx, sessionID, req)
	}

	now := time.Now()
	c.upsertSession(executor.Session{
		SessionID: sessionID,
		Title:     truncateTitle(req.Prompt, 36),
		Status:    executor.SessionStatusRunning,
		Executor:  req.Executor,
		CreatedAt: now,
		UpdatedAt: now,
	})
	c.setSessionRequest(sessionID, req)

	go c.pipeSessionLogs(sessionID, string(req.Executor), exec)

	return executor.ExecuteResponse{SessionID: sessionID, Status: "running"}, nil
}

func (c *Client) pipeSessionLogs(sessionID, executorName string, exec executor.Executor) {
	done := false
	defer func() {
		if !done {
			c.updateSessionStatus(sessionID, executor.SessionStatusInterrupted)
		}
		_ = exec.Close()
		c.registry.RemoveSession(sessionID)
		if c.hooks.OnSessionEnd != nil {
			c.hooks.OnSessionEnd(context.Background(), sessionID)
		}
	}()

	for logEntry := range exec.Logs() {
		c.captureResumeState(sessionID, executorName, logEntry)
		evt := c.transformEvent(sessionID, executorName, logEntry)
		storedEvt, err := c.store.Append(context.Background(), evt)
		if err != nil {
			if c.hooks.OnStoreError != nil {
				c.hooks.OnStoreError(context.Background(), sessionID, evt, err)
			}
			log.Errorf("store append failed: session=%s type=%s err=%v", sessionID, logEntry.Type, err)
			continue
		}
		if c.hooks.OnEventStored != nil {
			c.hooks.OnEventStored(context.Background(), storedEvt)
		}

		c.touchSession(sessionID, storedEvt)
		c.stream.AppendLog(sessionID, streaming.LogEntry{Type: storedEvt.Type, Content: storedEvt})
		if storedEvt.Type == "done" {
			done = true
			c.updateSessionStatus(sessionID, executor.SessionStatusDone)
			return
		}
	}
}

// PauseTask interrupts a running task.
func (c *Client) PauseTask(sessionID string) error {
	exec, ok := c.registry.GetSession(sessionID)
	if !ok {
		return executor.ErrSessionNotFound
	}
	if err := exec.Interrupt(); err != nil {
		return err
	}

	c.updateSessionStatus(sessionID, executor.SessionStatusInterrupted)
	return nil
}

// ContinueTask continues a paused/running task with a message.
func (c *Client) ContinueTask(ctx context.Context, sessionID string, message string) error {
	if message == "" {
		message = "continue"
	}

	if exec, ok := c.registry.GetSession(sessionID); ok {
		if err := exec.SendMessage(ctx, message); err != nil {
			return err
		}
		c.updateSessionStatus(sessionID, executor.SessionStatusRunning)
		return nil
	}

	req, resume, ok := c.getSessionRuntime(sessionID)
	if !ok {
		return executor.ErrSessionNotFound
	}
	opts := executor.Options{
		WorkingDir:                 req.WorkingDir,
		Model:                      req.Model,
		Plan:                       req.Plan,
		DangerouslySkipPermissions: !req.Plan,
		Approvals:                  req.Plan || (req.AskForApproval != "" && req.AskForApproval != "never"),
		Sandbox:                    req.Sandbox,
		Env:                        req.Env,
		AskForApproval:             req.AskForApproval,
	}

	switch req.Executor {
	case executor.ExecutorClaudeCode:
		if resume.ClaudeSessionID == "" {
			return ErrResumeUnavailable
		}
		opts.ResumeSessionID = resume.ClaudeSessionID
	case executor.ExecutorCodex:
		if resume.CodexConversation == "" && resume.CodexRolloutPath == "" {
			return ErrResumeUnavailable
		}
		opts.ResumeSessionID = resume.CodexConversation
		opts.ResumePath = resume.CodexRolloutPath
	default:
		return fmt.Errorf("resume unsupported for executor %s", req.Executor)
	}

	exec, err := c.registry.CreateSession(sessionID, string(req.Executor), opts)
	if err != nil {
		return err
	}
	if err := exec.Start(ctx, message, opts); err != nil {
		_ = exec.Close()
		c.registry.RemoveSession(sessionID)
		return err
	}
	go c.pipeSessionLogs(sessionID, string(req.Executor), exec)

	c.updateSessionStatus(sessionID, executor.SessionStatusRunning)
	return nil
}

// ResumeTask is an alias for ContinueTask.
func (c *Client) ResumeTask(ctx context.Context, sessionID string, message string) error {
	return c.ContinueTask(ctx, sessionID, message)
}

// RespondControl answers executor approval/control requests.
func (c *Client) RespondControl(ctx context.Context, sessionID string, response executor.ControlResponse) error {
	exec, ok := c.registry.GetSession(sessionID)
	if !ok {
		return executor.ErrSessionNotFound
	}
	return exec.RespondControl(ctx, response)
}

// SessionRunning reports whether a session is still active.
func (c *Client) SessionRunning(sessionID string) bool {
	_, ok := c.registry.GetSession(sessionID)
	return ok
}

// ListEvents reads persisted session events.
func (c *Client) ListEvents(ctx context.Context, sessionID string, afterSeq uint64, limit int) ([]executor.Event, error) {
	return c.store.List(ctx, sessionID, store.ListOptions{AfterSeq: afterSeq, Limit: limit})
}

// GetSessionEvents returns stored events for a session.
func (c *Client) GetSessionEvents(sessionID string) ([]executor.Event, bool) {
	events, err := c.ListEvents(context.Background(), sessionID, 0, 0)
	if err != nil {
		return nil, false
	}
	if len(events) == 0 && !c.SessionRunning(sessionID) {
		return nil, false
	}
	return events, true
}

// ListSessions returns all known sessions sorted by update time (desc).
func (c *Client) ListSessions(_ context.Context) []executor.Session {
	c.sessionsMu.RLock()
	list := make([]executor.Session, 0, len(c.sessions))
	for _, session := range c.sessions {
		list = append(list, session)
	}
	c.sessionsMu.RUnlock()

	sort.Slice(list, func(i, j int) bool {
		return list[i].UpdatedAt.After(list[j].UpdatedAt)
	})

	return list
}

func (c *Client) touchSession(sessionID string, evt executor.Event) {
	status := executor.SessionStatusRunning
	if evt.Type == "done" {
		status = executor.SessionStatusDone
	}
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now()
	}
	c.sessionsMu.Lock()
	defer c.sessionsMu.Unlock()

	session, ok := c.sessions[sessionID]
	if !ok {
		title := sessionID
		if value, ok := evt.Content.(string); ok && value != "" {
			title = truncateTitle(value, 36)
		}
		c.sessions[sessionID] = executor.Session{
			SessionID: sessionID,
			Title:     title,
			Status:    status,
			Executor:  executor.ExecutorType(evt.Executor),
			CreatedAt: evt.Timestamp,
			UpdatedAt: evt.Timestamp,
		}
		return
	}
	session.UpdatedAt = evt.Timestamp
	if evt.Executor != "" {
		session.Executor = executor.ExecutorType(evt.Executor)
	}
	session.Status = status
	c.sessions[sessionID] = session
}

func (c *Client) updateSessionStatus(sessionID string, status executor.SessionStatus) {
	c.sessionsMu.Lock()
	defer c.sessionsMu.Unlock()

	session, ok := c.sessions[sessionID]
	if !ok {
		return
	}

	session.Status = status
	session.UpdatedAt = time.Now()
	c.sessions[sessionID] = session
}

func (c *Client) upsertSession(session executor.Session) {
	c.sessionsMu.Lock()
	defer c.sessionsMu.Unlock()

	c.sessions[session.SessionID] = session
}

func (c *Client) setSessionRequest(sessionID string, req executor.ExecuteRequest) {
	c.sessionsMu.Lock()
	defer c.sessionsMu.Unlock()
	c.requests[sessionID] = req
}

func (c *Client) getSessionRuntime(sessionID string) (executor.ExecuteRequest, sessionResumeInfo, bool) {
	c.sessionsMu.RLock()
	defer c.sessionsMu.RUnlock()
	req, ok := c.requests[sessionID]
	if !ok {
		return executor.ExecuteRequest{}, sessionResumeInfo{}, false
	}
	return req, c.resumeInfo[sessionID], true
}

func (c *Client) captureResumeState(sessionID, executorName string, logEntry executor.Log) {
	c.sessionsMu.Lock()
	defer c.sessionsMu.Unlock()

	resume := c.resumeInfo[sessionID]
	switch executorName {
	case string(executor.ExecutorClaudeCode):
		if obj, ok := decodeJSONObject(logEntry.Content); ok {
			if sid, ok := obj["session_id"].(string); ok && sid != "" {
				resume.ClaudeSessionID = sid
			}
		} else if text := executor.StringifyContent(logEntry.Content); text != "" {
			if obj, ok := decodeJSONObjectFromLine(text); ok {
				if sid, ok := obj["session_id"].(string); ok && sid != "" {
					resume.ClaudeSessionID = sid
				}
			}
		}
	case string(executor.ExecutorCodex):
		if obj, ok := decodeJSONObject(logEntry.Content); ok {
			if result, ok := obj["result"].(map[string]any); ok {
				if conv, ok := result["conversationId"].(string); ok && conv != "" {
					resume.CodexConversation = conv
				}
				if rollout, ok := result["rolloutPath"].(string); ok && rollout != "" {
					resume.CodexRolloutPath = rollout
				}
			}
			if conv, ok := obj["conversationId"].(string); ok && conv != "" {
				resume.CodexConversation = conv
			}
		}
	}

	c.resumeInfo[sessionID] = resume
}

func truncateTitle(text string, limit int) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || limit <= 0 {
		return ""
	}

	runes := []rune(trimmed)
	if len(runes) <= limit {
		return trimmed
	}

	return string(runes[:limit])
}

func decodeJSONObject(v any) (map[string]any, bool) {
	switch val := v.(type) {
	case map[string]any:
		return val, true
	case json.RawMessage:
		var out map[string]any
		if err := json.Unmarshal(val, &out); err == nil {
			return out, true
		}
	case []byte:
		var out map[string]any
		if err := json.Unmarshal(val, &out); err == nil {
			return out, true
		}
	case string:
		var out map[string]any
		if err := json.Unmarshal([]byte(val), &out); err == nil {
			return out, true
		}
	}
	return nil, false
}

func decodeJSONObjectFromLine(line string) (map[string]any, bool) {
	start := strings.Index(line, "{")
	if start < 0 {
		return nil, false
	}
	return decodeJSONObject(line[start:])
}

// Subscribe streams session events via channel.
func (c *Client) Subscribe(sessionID string, opts executor.SubscribeOptions) (<-chan executor.Event, func()) {
	out := make(chan executor.Event, 100)
	newLogs, unsubscribeStream := c.stream.Subscribe(sessionID)
	stop := make(chan struct{})
	stopOnce := sync.Once{}

	go func() {
		defer close(out)
		defer unsubscribeStream()

		barrierSeq, _ := c.store.LatestSeq(context.Background(), sessionID)
		lastEmittedSeq := opts.AfterSeq

		emit := func(evt executor.Event) bool {
			if evt.Type == "debug" && !opts.IncludeDebug {
				return true
			}
			select {
			case out <- evt:
				if evt.Seq > lastEmittedSeq {
					lastEmittedSeq = evt.Seq
				}
				return true
			case <-stop:
				return false
			}
		}

		if opts.ReturnAll {
			history, err := c.store.List(context.Background(), sessionID, store.ListOptions{
				AfterSeq: opts.AfterSeq,
				UntilSeq: barrierSeq,
				Limit:    opts.Limit,
			})
			if err != nil {
				if c.hooks.OnStoreError != nil {
					c.hooks.OnStoreError(context.Background(), sessionID, executor.Event{SessionID: sessionID, Type: "history"}, err)
				}
				return
			}
			for _, evt := range history {
				if !emit(evt) {
					return
				}
			}
		}

		if !c.SessionRunning(sessionID) {
			if lastEmittedSeq == 0 {
				_ = emit(executor.Event{SessionID: sessionID, Type: "done", Content: map[string]any{}})
			}
			return
		}

		for {
			select {
			case entry, ok := <-newLogs:
				if !ok {
					_ = emit(executor.Event{SessionID: sessionID, Type: "done", Content: map[string]any{}})
					return
				}

				evt, ok := entry.Content.(executor.Event)
				if !ok {
					evt = executor.Event{SessionID: sessionID, Type: entry.Type, Content: entry.Content}
				}
				if evt.Seq > 0 && evt.Seq <= lastEmittedSeq {
					continue
				}
				if !emit(evt) {
					return
				}
				if evt.Type == "done" {
					return
				}
			case <-stop:
				return
			}
		}
	}()

	cancel := func() {
		stopOnce.Do(func() {
			close(stop)
		})
	}

	return out, cancel
}

// Shutdown closes all active sessions.
func (c *Client) Shutdown() {
	c.registry.ShutdownAll()
	if closer, ok := c.store.(storeCloser); ok {
		closer.Close()
	}
}
