package sdk

import (
	"context"
	"errors"
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
	exec, ok := c.registry.GetSession(sessionID)
	if !ok {
		return executor.ErrSessionNotFound
	}
	if message == "" {
		message = "continue"
	}
	if err := exec.SendMessage(ctx, message); err != nil {
		return err
	}

	c.updateSessionStatus(sessionID, executor.SessionStatusRunning)
	return nil
}

// ResumeTask is an alias for ContinueTask.
func (c *Client) ResumeTask(ctx context.Context, sessionID string, message string) error {
	return c.ContinueTask(ctx, sessionID, message)
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
