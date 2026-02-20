package sdk

import (
	"context"
	"errors"
	"sync"

	"github.com/google/uuid"
	"github.com/supremeagent/executor/pkg/executor"
	"github.com/supremeagent/executor/pkg/executor/claude"
	"github.com/supremeagent/executor/pkg/executor/codex"
	"github.com/supremeagent/executor/pkg/streaming"
)

var ErrPromptRequired = errors.New("prompt is required")

// Client is the SDK entry point for executing and managing tasks.
type Client struct {
	registry *executor.Registry
	stream   *streaming.Manager
}

// New creates an SDK client with built-in executors registered.
func New() *Client {
	registry := executor.NewRegistry()
	registry.Register(string(ExecutorClaudeCode), claude.NewFactory())
	registry.Register(string(ExecutorCodex), codex.NewFactory())

	return NewWithRegistry(registry, streaming.NewManager())
}

// NewWithRegistry creates an SDK client using custom dependencies.
func NewWithRegistry(registry *executor.Registry, streamMgr *streaming.Manager) *Client {
	if registry == nil {
		registry = executor.NewRegistry()
	}
	if streamMgr == nil {
		streamMgr = streaming.NewManager()
	}

	return &Client{registry: registry, stream: streamMgr}
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
		exec.Close()
		c.registry.RemoveSession(sessionID)
		return ExecuteResponse{}, err
	}

	go c.pipeSessionLogs(sessionID, exec)

	return ExecuteResponse{SessionID: sessionID, Status: "running"}, nil
}

func (c *Client) pipeSessionLogs(sessionID string, exec executor.Executor) {
	defer func() {
		exec.Close()
		c.registry.RemoveSession(sessionID)
	}()

	for logEntry := range exec.Logs() {
		c.stream.AppendLog(sessionID, streaming.LogEntry{Type: logEntry.Type, Content: logEntry.Content})
		if logEntry.Type == "done" {
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

// GetSessionEvents returns stored events for a session.
func (c *Client) GetSessionEvents(sessionID string) ([]Event, bool) {
	logs, ok := c.stream.GetSession(sessionID)
	if !ok {
		return nil, false
	}

	events := make([]Event, len(logs))
	for i := range logs {
		events[i] = Event{Type: logs[i].Type, Content: logs[i].Content}
	}
	return events, true
}

// Subscribe streams session events via channel.
func (c *Client) Subscribe(sessionID string, opts SubscribeOptions) (<-chan Event, func()) {
	out := make(chan Event, 100)
	newLogs, unsubscribeStream := c.stream.Subscribe(sessionID)
	logs, _ := c.stream.GetSession(sessionID)
	running := c.SessionRunning(sessionID)

	stop := make(chan struct{})
	stopOnce := sync.Once{}

	emit := func(entry streaming.LogEntry) bool {
		if entry.Type == "debug" && !opts.IncludeDebug {
			return true
		}
		select {
		case out <- Event{Type: entry.Type, Content: entry.Content}:
			return true
		case <-stop:
			return false
		}
	}

	go func() {
		defer close(out)
		defer unsubscribeStream()

		hasDone := false
		if opts.ReturnAll {
			for _, entry := range logs {
				if entry.Type == "done" {
					hasDone = true
				}
				if !emit(entry) {
					return
				}
			}
		}

		if !running {
			if !hasDone {
				_ = emit(streaming.LogEntry{Type: "done", Content: map[string]any{}})
			}
			return
		}

		for {
			select {
			case entry, ok := <-newLogs:
				if !ok {
					_ = emit(streaming.LogEntry{Type: "done", Content: map[string]any{}})
					return
				}
				if !emit(entry) {
					return
				}
				if entry.Type == "done" {
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
}
