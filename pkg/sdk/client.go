package sdk

import (
	"context"
	"errors"
	"sync"

	"github.com/google/uuid"
	"github.com/mylxsw/asteria/log"
	"github.com/supremeagent/executor/pkg/executor"
	"github.com/supremeagent/executor/pkg/executor/claude"
	"github.com/supremeagent/executor/pkg/executor/codex"
	"github.com/supremeagent/executor/pkg/streaming"
)

var ErrPromptRequired = errors.New("prompt is required")

// ClientOptions configures SDK client behavior.
type ClientOptions struct {
	Registry      *executor.Registry
	StreamManager *streaming.Manager
	EventStore    EventStore
	Hooks         Hooks
}

// Client is the SDK entry point for executing and managing tasks.
type Client struct {
	registry *executor.Registry
	stream   *streaming.Manager
	store    EventStore
	hooks    Hooks
}

type storeCloser interface {
	Close()
}

// New creates an SDK client with built-in executors registered.
func New() *Client {
	registry := executor.NewRegistry()
	registry.Register(string(ExecutorClaudeCode), claude.NewFactory())
	registry.Register(string(ExecutorCodex), codex.NewFactory())

	return NewWithOptions(ClientOptions{
		Registry:      registry,
		StreamManager: streaming.NewManager(),
		EventStore:    NewMemoryEventStore(),
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
		opts.EventStore = NewMemoryEventStore()
	}

	return &Client{registry: opts.Registry, stream: opts.StreamManager, store: opts.EventStore, hooks: opts.Hooks}
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
func (c *Client) Execute(ctx context.Context, req ExecuteRequest) (ExecuteResponse, error) {
	if req.Prompt == "" {
		return ExecuteResponse{}, ErrPromptRequired
	}
	if req.Executor == "" {
		req.Executor = ExecutorClaudeCode
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
		return ExecuteResponse{}, err
	}

	if err := exec.Start(ctx, req.Prompt, opts); err != nil {
		_ = exec.Close()
		c.registry.RemoveSession(sessionID)
		return ExecuteResponse{}, err
	}

	if c.hooks.OnSessionStart != nil {
		c.hooks.OnSessionStart(ctx, sessionID, req)
	}

	go c.pipeSessionLogs(sessionID, string(req.Executor), exec)

	return ExecuteResponse{SessionID: sessionID, Status: "running"}, nil
}

func (c *Client) pipeSessionLogs(sessionID, executorName string, exec executor.Executor) {
	defer func() {
		_ = exec.Close()
		c.registry.RemoveSession(sessionID)
		if c.hooks.OnSessionEnd != nil {
			c.hooks.OnSessionEnd(context.Background(), sessionID)
		}
	}()

	for logEntry := range exec.Logs() {
		evt := Event{SessionID: sessionID, Executor: executorName, Type: logEntry.Type, Content: logEntry.Content}
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

		c.stream.AppendLog(sessionID, streaming.LogEntry{Type: storedEvt.Type, Content: storedEvt})
		if storedEvt.Type == "done" {
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
	return exec.Interrupt()
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
	return exec.SendMessage(ctx, message)
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
func (c *Client) ListEvents(ctx context.Context, sessionID string, afterSeq uint64, limit int) ([]Event, error) {
	return c.store.List(ctx, sessionID, ListOptions{AfterSeq: afterSeq, Limit: limit})
}

// GetSessionEvents returns stored events for a session.
func (c *Client) GetSessionEvents(sessionID string) ([]Event, bool) {
	events, err := c.ListEvents(context.Background(), sessionID, 0, 0)
	if err != nil {
		return nil, false
	}
	if len(events) == 0 && !c.SessionRunning(sessionID) {
		return nil, false
	}
	return events, true
}

// Subscribe streams session events via channel.
func (c *Client) Subscribe(sessionID string, opts SubscribeOptions) (<-chan Event, func()) {
	out := make(chan Event, 100)
	newLogs, unsubscribeStream := c.stream.Subscribe(sessionID)
	stop := make(chan struct{})
	stopOnce := sync.Once{}

	go func() {
		defer close(out)
		defer unsubscribeStream()

		barrierSeq, _ := c.store.LatestSeq(context.Background(), sessionID)
		lastEmittedSeq := opts.AfterSeq

		emit := func(evt Event) bool {
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
			history, err := c.store.List(context.Background(), sessionID, ListOptions{
				AfterSeq: opts.AfterSeq,
				UntilSeq: barrierSeq,
				Limit:    opts.Limit,
			})
			if err != nil {
				if c.hooks.OnStoreError != nil {
					c.hooks.OnStoreError(context.Background(), sessionID, Event{SessionID: sessionID, Type: "history"}, err)
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
				_ = emit(Event{SessionID: sessionID, Type: "done", Content: map[string]any{}})
			}
			return
		}

		for {
			select {
			case entry, ok := <-newLogs:
				if !ok {
					_ = emit(Event{SessionID: sessionID, Type: "done", Content: map[string]any{}})
					return
				}

				evt, ok := entry.Content.(Event)
				if !ok {
					evt = Event{SessionID: sessionID, Type: entry.Type, Content: entry.Content}
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
