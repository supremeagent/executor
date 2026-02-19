package executor

import (
	"context"
)

// Executor defines the interface for AI executor implementations
type Executor interface {
	// Start starts the executor with the given prompt
	Start(ctx context.Context, prompt string, opts Options) error

	// Interrupt interrupts the current execution
	Interrupt() error

	// SendMessage sends a message to continue the conversation
	SendMessage(ctx context.Context, message string) error

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

	// Claude Code specific
	Approvals                  bool
	DangerouslySkipPermissions bool

	// Codex specific
	Sandbox              string
	AskForApproval       string
	ModelReasoningEffort string
}

// Log represents a log entry from the executor
type Log struct {
	Type    string // "stdout", "stderr", "tool_use", "error", "done"
	Content interface{}
}

// Registry manages executor instances
type Registry struct {
	factories map[string]Factory
	sessions  map[string]Executor
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
	r.factories[name] = factory
}

// CreateSession creates a new executor session
func (r *Registry) CreateSession(id, executorType string, opts Options) (Executor, error) {
	factory, ok := r.factories[executorType]
	if !ok {
		return nil, ErrUnknownExecutorType
	}

	executor, err := factory.Create()
	if err != nil {
		return nil, err
	}

	r.sessions[id] = executor
	return executor, nil
}

// GetSession gets an executor session by ID
func (r *Registry) GetSession(id string) (Executor, bool) {
	executor, ok := r.sessions[id]
	return executor, ok
}

// RemoveSession removes a session from the registry
func (r *Registry) RemoveSession(id string) {
	delete(r.sessions, id)
}

// ShutdownAll shuts down all active sessions
func (r *Registry) ShutdownAll() {
	for _, executor := range r.sessions {
		executor.Close()
	}
}
