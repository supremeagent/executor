package executor

import (
	"context"
	"sync"
)

// Executor defines the interface for AI executor implementations
type Executor interface {
	// Start starts the executor with the given prompt
	Start(ctx context.Context, prompt string, opts Options) error

	// Interrupt interrupts the current execution
	Interrupt() error

	// SendMessage sends a message to continue the conversation
	SendMessage(ctx context.Context, message string) error

	// RespondControl sends a response to a pending control/approval request.
	RespondControl(ctx context.Context, response ControlResponse) error

	// Wait waits for the execution to complete
	Wait() error

	// Logs returns a channel for streaming logs
	Logs() <-chan Log

	// Done returns a channel that signals when execution is complete
	Done() <-chan struct{}

	// Close closes the executor and releases resources
	Close() error
}

// Factory creates executor instances
type Factory interface {
	Create() (Executor, error)
}

// FactoryFunc is a function that creates executor instances
type FactoryFunc func() (Executor, error)

func (f FactoryFunc) Create() (Executor, error) {
	return f()
}

// Options contains executor-specific options
type Options struct {
	WorkingDir string
	Model      string
	Plan       bool
	Env        map[string]string
	// ResumeSessionID identifies the upstream executor session/conversation to resume.
	ResumeSessionID string
	// ResumePath is optional executor specific resume source path (for example Codex rollout file).
	ResumePath string

	// Claude Code specific
	Approvals                  bool
	DangerouslySkipPermissions bool

	// Codex specific
	Sandbox              string
	AskForApproval       string
	ModelReasoningEffort string

	// Shared: skip all permission/approval prompts and run autonomously.
	// Used by Gemini (--yolo), Qwen (--yolo), Droid (--skip-permissions-unsafe).
	Yolo bool

	// Droid specific: autonomy level (normal, low, medium, high, skip-permissions-unsafe).
	// When empty, Droid defaults to skip-permissions-unsafe if Yolo is true.
	DroidAutonomy string

	// Droid specific: reasoning effort (none, dynamic, off, low, medium, high).
	DroidReasoningEffort string

	// Copilot specific: allow all tools without prompting.
	CopilotAllowAllTools bool

	// Gemini / Qwen / Copilot: extra CLI args forwarded verbatim to the subprocess.
	ExtraArgs []string
}

// Log represents a log entry from the executor
type Log struct {
	Type    string // "stdout", "stderr", "tool_use", "error", "done"
	Content any
}

// Registry manages executor instances
type Registry struct {
	factories map[string]Factory
	sessions  map[string]Executor
	mu        sync.RWMutex
}

// NewRegistry creates a new executor registry
func NewRegistry() *Registry {
	return &Registry{
		factories: make(map[string]Factory),
		sessions:  make(map[string]Executor),
	}
}

// Register registers an executor factory
func (r *Registry) Register(name string, factory Factory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[name] = factory
}

// CreateSession creates a new executor session
func (r *Registry) CreateSession(id, executorType string, opts Options) (Executor, error) {
	r.mu.RLock()
	factory, ok := r.factories[executorType]
	r.mu.RUnlock()
	if !ok {
		return nil, ErrUnknownExecutorType
	}

	exec, err := factory.Create()
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	r.sessions[id] = exec
	r.mu.Unlock()

	return exec, nil
}

// GetSession gets an executor session by ID
func (r *Registry) GetSession(id string) (Executor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	exec, ok := r.sessions[id]
	return exec, ok
}

// RemoveSession removes a session from the registry
func (r *Registry) RemoveSession(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessions, id)
}

// ShutdownAll shuts down all active sessions
func (r *Registry) ShutdownAll() {
	r.mu.Lock()
	sessions := make([]Executor, 0, len(r.sessions))
	for id, ex := range r.sessions {
		sessions = append(sessions, ex)
		delete(r.sessions, id)
	}
	r.mu.Unlock()

	for _, ex := range sessions {
		_ = ex.Close()
	}
}
