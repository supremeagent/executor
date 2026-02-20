package sdk

import (
	"context"
	"testing"
	"time"

	"github.com/supremeagent/executor/pkg/executor"
	"github.com/supremeagent/executor/pkg/streaming"
)

func TestClientExecutePauseContinue(t *testing.T) {
	registry := executor.NewRegistry()
	streamMgr := streaming.NewManager()
	client := NewWithRegistry(registry, streamMgr)

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

func TestClientSubscribe(t *testing.T) {
	client := NewWithRegistry(executor.NewRegistry(), streaming.NewManager())
	sessionID := "s1"

	// Historical logs
	client.stream.AppendLog(sessionID, streaming.LogEntry{Type: "stdout", Content: "history"})
	client.stream.AppendLog(sessionID, streaming.LogEntry{Type: "debug", Content: "hidden"})
	client.stream.AppendLog(sessionID, streaming.LogEntry{Type: "done", Content: "done"})

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
