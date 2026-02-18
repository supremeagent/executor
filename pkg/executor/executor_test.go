package executor

import (
	"context"
	"testing"
)

type MockExecutor struct {
	logs chan Log
	done chan struct{}
}

func (m *MockExecutor) Start(ctx context.Context, prompt string, opts Options) error { return nil }
func (m *MockExecutor) Interrupt() error                                         { return nil }
func (m *MockExecutor) SendMessage(ctx context.Context, message string) error     { return nil }
func (m *MockExecutor) Wait() error                                              { return nil }
func (m *MockExecutor) Logs() <-chan Log                                         { return m.logs }
func (m *MockExecutor) Done() <-chan struct{}                                    { return m.done }
func (m *MockExecutor) Close() error                                             { return nil }

func TestRegistry(t *testing.T) {
	r := NewRegistry()
	
	factory := FactoryFunc(func() (Executor, error) {
		return &MockExecutor{
			logs: make(chan Log),
			done: make(chan struct{}),
		}, nil
	})

	r.Register("mock", factory)

	opts := Options{WorkingDir: "/tmp"}
	sessionID := "sess-1"
	
	exec, err := r.CreateSession(sessionID, "mock", opts)
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	retrieved, ok := r.GetSession(sessionID)
	if !ok || retrieved != exec {
		t.Error("failed to retrieve session")
	}

	r.RemoveSession(sessionID)
	_, ok = r.GetSession(sessionID)
	if ok {
		t.Error("session should be removed")
	}

	// Test ShutdownAll
	r.CreateSession("sess-3", "mock", opts)
	r.ShutdownAll()

	// Test unknown executor type
	_, err = r.CreateSession("sess-2", "unknown", opts)
	if err != ErrUnknownExecutorType {
		t.Errorf("expected ErrUnknownExecutorType, got %v", err)
	}
}
