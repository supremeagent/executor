package sdk

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/supremeagent/executor/pkg/executor"
	"github.com/supremeagent/executor/pkg/streaming"
)

func TestClientExecutePauseContinue(t *testing.T) {
	registry := executor.NewRegistry()
	streamMgr := streaming.NewManager()
	client := NewWithOptions(ClientOptions{Registry: registry, StreamManager: streamMgr, EventStore: NewMemoryEventStore()})

	mock := &testExecutor{logs: make(chan executor.Log, 10), done: make(chan struct{})}
	registry.Register("test", executor.FactoryFunc(func() (executor.Executor, error) { return mock, nil }))

	resp, err := client.Execute(context.Background(), ExecuteRequest{Prompt: "hello", Executor: "test"})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if resp.SessionID == "" {
		t.Fatal("expected non-empty session ID")
	}
	if err := client.PauseTask(resp.SessionID); err != nil {
		t.Fatalf("pause failed: %v", err)
	}
	if !mock.interrupted {
		t.Fatal("expected executor to be interrupted")
	}
	if err := client.ContinueTask(context.Background(), resp.SessionID, ""); err != nil {
		t.Fatalf("continue failed: %v", err)
	}
	if mock.lastMessage != "continue" {
		t.Fatalf("expected default continue message, got %q", mock.lastMessage)
	}
}

func TestClientSubscribeAndListFromStore(t *testing.T) {
	registry := executor.NewRegistry()
	client := NewWithOptions(ClientOptions{Registry: registry, StreamManager: streaming.NewManager(), EventStore: NewMemoryEventStore()})

	sessionID := "s1"
	_, err := client.store.Append(context.Background(), Event{SessionID: sessionID, Type: "stdout", Content: "history"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.store.Append(context.Background(), Event{SessionID: sessionID, Type: "debug", Content: "hidden"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.store.Append(context.Background(), Event{SessionID: sessionID, Type: "done", Content: "done"})
	if err != nil {
		t.Fatal(err)
	}

	listed, err := client.ListEvents(context.Background(), sessionID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 3 {
		t.Fatalf("expected 3 listed events, got %d", len(listed))
	}

	ch, cancel := client.Subscribe(sessionID, SubscribeOptions{ReturnAll: true, IncludeDebug: false})
	defer cancel()

	var events []Event
	for evt := range ch {
		events = append(events, evt)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events (stdout, done), got %d", len(events))
	}
	if events[0].Type != "stdout" || events[1].Type != "done" {
		t.Fatalf("unexpected event sequence: %#v", events)
	}
	if events[0].Seq == 0 {
		t.Fatalf("expected persisted sequence number, got %#v", events[0])
	}
}

func TestHooks(t *testing.T) {
	registry := executor.NewRegistry()
	mock := &testExecutor{logs: make(chan executor.Log, 10), done: make(chan struct{})}
	registry.Register("test", executor.FactoryFunc(func() (executor.Executor, error) { return mock, nil }))

	var startCount int32
	var eventCount int32
	var endCount int32

	client := NewWithOptions(ClientOptions{
		Registry:      registry,
		StreamManager: streaming.NewManager(),
		EventStore:    NewMemoryEventStore(),
		Hooks: Hooks{
			OnSessionStart: func(ctx context.Context, sessionID string, req ExecuteRequest) {
				atomic.AddInt32(&startCount, 1)
			},
			OnEventStored: func(ctx context.Context, evt Event) {
				atomic.AddInt32(&eventCount, 1)
			},
			OnSessionEnd: func(ctx context.Context, sessionID string) {
				atomic.AddInt32(&endCount, 1)
			},
		},
	})

	_, err := client.Execute(context.Background(), ExecuteRequest{Prompt: "hello", Executor: "test"})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	if atomic.LoadInt32(&startCount) != 1 {
		t.Fatalf("expected start hook to run once")
	}
	if atomic.LoadInt32(&eventCount) == 0 {
		t.Fatalf("expected event hook to run")
	}
	if atomic.LoadInt32(&endCount) != 1 {
		t.Fatalf("expected end hook to run once")
	}
}

func TestExecuteValidation(t *testing.T) {
	client := NewWithRegistry(executor.NewRegistry(), streaming.NewManager())
	_, err := client.Execute(context.Background(), ExecuteRequest{})
	if err != ErrPromptRequired {
		t.Fatalf("expected ErrPromptRequired, got %v", err)
	}
}

type testExecutor struct {
	logs        chan executor.Log
	done        chan struct{}
	interrupted bool
	lastMessage string
}

func (m *testExecutor) Start(ctx context.Context, prompt string, opts executor.Options) error {
	go func() {
		m.logs <- executor.Log{Type: "stdout", Content: "running"}
		time.Sleep(10 * time.Millisecond)
		m.logs <- executor.Log{Type: "done", Content: "done"}
	}()
	return nil
}

func (m *testExecutor) Interrupt() error {
	m.interrupted = true
	return nil
}

func (m *testExecutor) SendMessage(ctx context.Context, message string) error {
	m.lastMessage = message
	return nil
}

func (m *testExecutor) Wait() error               { return nil }
func (m *testExecutor) Logs() <-chan executor.Log { return m.logs }
func (m *testExecutor) Done() <-chan struct{}     { return m.done }
func (m *testExecutor) Close() error              { return nil }
