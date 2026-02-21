package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/supremeagent/executor/pkg/executor"
	"github.com/supremeagent/executor/pkg/store"
	"github.com/supremeagent/executor/pkg/streaming"
)

func TestClientExecutePauseContinue(t *testing.T) {
	registry := executor.NewRegistry()
	streamMgr := streaming.NewManager()
	client := NewWithOptions(ClientOptions{Registry: registry, StreamManager: streamMgr, EventStore: store.NewMemoryEventStore()})

	mock := &testExecutor{logs: make(chan executor.Log, 10), done: make(chan struct{})}
	registry.Register("test", executor.FactoryFunc(func() (executor.Executor, error) { return mock, nil }))

	resp, err := client.Execute(context.Background(), executor.ExecuteRequest{Prompt: "hello", Executor: "test"})
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
	client := NewWithOptions(ClientOptions{Registry: registry, StreamManager: streaming.NewManager(), EventStore: store.NewMemoryEventStore()})

	sessionID := "s1"
	_, err := client.store.Append(context.Background(), executor.Event{SessionID: sessionID, Type: "stdout", Content: "history"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.store.Append(context.Background(), executor.Event{SessionID: sessionID, Type: "debug", Content: "hidden"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.store.Append(context.Background(), executor.Event{SessionID: sessionID, Type: "done", Content: "done"})
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

	ch, cancel := client.Subscribe(sessionID, executor.SubscribeOptions{ReturnAll: true, IncludeDebug: false})
	defer cancel()

	var events []executor.Event
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
		EventStore:    store.NewMemoryEventStore(),
		Hooks: executor.Hooks{
			OnSessionStart: func(ctx context.Context, sessionID string, req executor.ExecuteRequest) {
				atomic.AddInt32(&startCount, 1)
			},
			OnEventStored: func(ctx context.Context, evt executor.Event) {
				atomic.AddInt32(&eventCount, 1)
			},
			OnSessionEnd: func(ctx context.Context, sessionID string) {
				atomic.AddInt32(&endCount, 1)
			},
		},
	})

	_, err := client.Execute(context.Background(), executor.ExecuteRequest{Prompt: "hello", Executor: "test"})
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
	_, err := client.Execute(context.Background(), executor.ExecuteRequest{})
	if err != ErrPromptRequired {
		t.Fatalf("expected ErrPromptRequired, got %v", err)
	}
}

func TestListSessions(t *testing.T) {
	registry := executor.NewRegistry()
	streamMgr := streaming.NewManager()
	client := NewWithOptions(ClientOptions{Registry: registry, StreamManager: streamMgr, EventStore: store.NewMemoryEventStore()})

	mock := &testExecutor{logs: make(chan executor.Log, 10), done: make(chan struct{})}
	registry.Register("test", executor.FactoryFunc(func() (executor.Executor, error) { return mock, nil }))

	resp, err := client.Execute(context.Background(), executor.ExecuteRequest{Prompt: "list sessions", Executor: "test"})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	sessions := client.ListSessions(context.Background())
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}

	session := sessions[0]
	if session.SessionID != resp.SessionID {
		t.Fatalf("unexpected session id: %s", session.SessionID)
	}
	if session.Status != executor.SessionStatusDone {
		t.Fatalf("expected done status, got %s", session.Status)
	}
	if session.Title != "list sessions" {
		t.Fatalf("unexpected session title: %q", session.Title)
	}
	if session.Executor != "test" {
		t.Fatalf("unexpected executor: %s", session.Executor)
	}
}

func TestDefaultTransformer_NormalizesCodexAndClaude(t *testing.T) {
	registry := executor.NewRegistry()
	client := NewWithOptions(ClientOptions{
		Registry:      registry,
		StreamManager: streaming.NewManager(),
		EventStore:    store.NewMemoryEventStore(),
	})

	// Register fake executors under built-in names so default transformers apply.
	registry.Register(string(executor.ExecutorCodex), executor.FactoryFunc(func() (executor.Executor, error) {
		return &testExecutor{logs: make(chan executor.Log, 10), done: make(chan struct{})}, nil
	}))
	registry.Register(string(executor.ExecutorClaudeCode), executor.FactoryFunc(func() (executor.Executor, error) {
		return &testExecutor{logs: make(chan executor.Log, 10), done: make(chan struct{})}, nil
	}))

	for _, execName := range []executor.ExecutorType{executor.ExecutorCodex, executor.ExecutorClaudeCode} {
		resp, err := client.Execute(context.Background(), executor.ExecuteRequest{Prompt: "hello", Executor: execName})
		if err != nil {
			t.Fatalf("execute failed for %s: %v", execName, err)
		}
		time.Sleep(50 * time.Millisecond)

		events, err := client.ListEvents(context.Background(), resp.SessionID, 0, 0)
		if err != nil {
			t.Fatalf("list events failed for %s: %v", execName, err)
		}
		if len(events) < 2 {
			t.Fatalf("expected at least 2 events for %s, got %d", execName, len(events))
		}

		first := events[0]
		content, ok := first.Content.(executor.UnifiedContent)
		if !ok {
			t.Fatalf("expected UnifiedContent for %s, got %T", execName, first.Content)
		}
		if first.Type != "message" {
			t.Fatalf("expected normalized type=message for %s, got %s", execName, first.Type)
		}
		if content.Source != string(execName) || content.SourceType != "stdout" || content.Category != "message" {
			t.Fatalf("unexpected normalized content for %s: %#v", execName, content)
		}
	}
}

func TestCustomTransformer_OverridesDefault(t *testing.T) {
	registry := executor.NewRegistry()
	client := NewWithOptions(ClientOptions{
		Registry:      registry,
		StreamManager: streaming.NewManager(),
		EventStore:    store.NewMemoryEventStore(),
		Transformers: map[string]executor.EventTransformer{
			string(executor.ExecutorCodex): func(input executor.TransformInput) executor.Event {
				return executor.Event{
					Type: "custom",
					Content: map[string]any{
						"text": fmt.Sprintf("%s:%v", input.Log.Type, input.Log.Content),
					},
				}
			},
		},
	})

	registry.Register(string(executor.ExecutorCodex), executor.FactoryFunc(func() (executor.Executor, error) {
		return &testExecutor{logs: make(chan executor.Log, 10), done: make(chan struct{})}, nil
	}))

	resp, err := client.Execute(context.Background(), executor.ExecuteRequest{Prompt: "hello", Executor: executor.ExecutorCodex})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	events, err := client.ListEvents(context.Background(), resp.SessionID, 0, 0)
	if err != nil {
		t.Fatalf("list events failed: %v", err)
	}
	if len(events) == 0 {
		t.Fatalf("expected events")
	}
	if events[0].Type != "custom" {
		t.Fatalf("expected custom transformed type, got %s", events[0].Type)
	}
}

func TestContinueTask_ResumeFromStoredRuntime(t *testing.T) {
	registry := executor.NewRegistry()
	streamMgr := streaming.NewManager()
	client := NewWithOptions(ClientOptions{Registry: registry, StreamManager: streamMgr, EventStore: store.NewMemoryEventStore()})

	re := &resumeExecutor{logs: make(chan executor.Log, 10), done: make(chan struct{})}
	registry.Register(string(executor.ExecutorCodex), executor.FactoryFunc(func() (executor.Executor, error) {
		return re, nil
	}))

	sessionID := "resume-session"
	client.requests[sessionID] = executor.ExecuteRequest{
		Executor:   executor.ExecutorCodex,
		WorkingDir: ".",
	}
	client.resumeInfo[sessionID] = sessionResumeInfo{CodexConversation: "conv-123"}

	if err := client.ContinueTask(context.Background(), sessionID, "resume me"); err != nil {
		t.Fatalf("continue failed: %v", err)
	}
	if re.startPrompt != "resume me" {
		t.Fatalf("expected resume message as prompt, got %q", re.startPrompt)
	}
	if re.startOpts.ResumeSessionID != "conv-123" {
		t.Fatalf("expected resume session id, got %+v", re.startOpts)
	}
}

func TestContinueTask_ResumeUnavailable(t *testing.T) {
	registry := executor.NewRegistry()
	client := NewWithOptions(ClientOptions{Registry: registry, StreamManager: streaming.NewManager(), EventStore: store.NewMemoryEventStore()})
	sessionID := "resume-unavailable"
	client.requests[sessionID] = executor.ExecuteRequest{
		Executor: executor.ExecutorClaudeCode,
	}

	err := client.ContinueTask(context.Background(), sessionID, "resume")
	if err != ErrResumeUnavailable {
		t.Fatalf("expected ErrResumeUnavailable, got %v", err)
	}
}

func TestCaptureResumeState(t *testing.T) {
	client := NewWithOptions(ClientOptions{
		Registry:      executor.NewRegistry(),
		StreamManager: streaming.NewManager(),
		EventStore:    store.NewMemoryEventStore(),
	})

	client.captureResumeState("s1", string(executor.ExecutorClaudeCode), executor.Log{
		Type:    "stdout",
		Content: `{"type":"result","session_id":"claude-sid-1","result":"ok"}`,
	})
	if client.resumeInfo["s1"].ClaudeSessionID != "claude-sid-1" {
		t.Fatalf("expected claude session id captured, got %+v", client.resumeInfo["s1"])
	}

	client.captureResumeState("s2", string(executor.ExecutorCodex), executor.Log{
		Type:    "output",
		Content: `{"id":3,"result":{"conversationId":"conv-1","rolloutPath":"/tmp/rollout.jsonl"}}`,
	})
	if client.resumeInfo["s2"].CodexConversation != "conv-1" {
		t.Fatalf("expected codex conversation captured, got %+v", client.resumeInfo["s2"])
	}
	if client.resumeInfo["s2"].CodexRolloutPath != "/tmp/rollout.jsonl" {
		t.Fatalf("expected codex rollout path captured, got %+v", client.resumeInfo["s2"])
	}
}

func TestDecodeJSONObjectHelpers(t *testing.T) {
	obj, ok := decodeJSONObject(map[string]any{"k": "v"})
	if !ok || obj["k"] != "v" {
		t.Fatalf("decode map failed: %#v", obj)
	}

	raw := json.RawMessage(`{"n":1}`)
	obj, ok = decodeJSONObject(raw)
	if !ok || obj["n"].(float64) != 1 {
		t.Fatalf("decode raw failed: %#v", obj)
	}

	obj, ok = decodeJSONObjectFromLine("prefix {\"a\":\"b\"}")
	if !ok || obj["a"] != "b" {
		t.Fatalf("decode from line failed: %#v", obj)
	}
}

func TestSetAndGetSessionRuntime(t *testing.T) {
	client := NewWithOptions(ClientOptions{
		Registry:      executor.NewRegistry(),
		StreamManager: streaming.NewManager(),
		EventStore:    store.NewMemoryEventStore(),
	})

	req := executor.ExecuteRequest{Prompt: "hello", Executor: executor.ExecutorCodex}
	client.setSessionRequest("sid", req)
	client.resumeInfo["sid"] = sessionResumeInfo{CodexConversation: "conv-x"}

	gotReq, gotResume, ok := client.getSessionRuntime("sid")
	if !ok {
		t.Fatalf("expected session runtime found")
	}
	if gotReq.Executor != executor.ExecutorCodex || gotResume.CodexConversation != "conv-x" {
		t.Fatalf("unexpected runtime: req=%+v resume=%+v", gotReq, gotResume)
	}
}

func TestNewAndRegisterExecutor(t *testing.T) {
	client := New()
	client.RegisterExecutor("x", executor.FactoryFunc(func() (executor.Executor, error) {
		return &testExecutor{logs: make(chan executor.Log, 10), done: make(chan struct{})}, nil
	}))

	resp, err := client.Execute(context.Background(), executor.ExecuteRequest{
		Prompt:   "hello",
		Executor: "x",
	})
	if err != nil {
		t.Fatalf("execute with registered executor failed: %v", err)
	}
	if resp.SessionID == "" {
		t.Fatalf("expected session id")
	}
}

func TestResumeRespondGetEventsAndShutdown(t *testing.T) {
	registry := executor.NewRegistry()
	client := NewWithOptions(ClientOptions{
		Registry:      registry,
		StreamManager: streaming.NewManager(),
		EventStore:    store.NewMemoryEventStore(),
	})

	mock := &testExecutor{logs: make(chan executor.Log, 10), done: make(chan struct{})}
	registry.Register("test", executor.FactoryFunc(func() (executor.Executor, error) { return mock, nil }))

	resp, err := client.Execute(context.Background(), executor.ExecuteRequest{Prompt: "x", Executor: "test"})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	if err := client.ResumeTask(context.Background(), resp.SessionID, "resume"); err != nil {
		t.Fatalf("resume failed: %v", err)
	}
	if err := client.RespondControl(context.Background(), resp.SessionID, executor.ControlResponse{
		RequestID: "req-1",
		Decision:  executor.ControlDecisionApprove,
	}); err != nil {
		t.Fatalf("respond control failed: %v", err)
	}

	events, ok := client.GetSessionEvents(resp.SessionID)
	if !ok || len(events) == 0 {
		time.Sleep(50 * time.Millisecond)
		events, ok = client.GetSessionEvents(resp.SessionID)
		if !ok || len(events) == 0 {
			t.Fatalf("expected session events")
		}
	}

	client.Shutdown()
}

func TestSubscribeBranches(t *testing.T) {
	registry := executor.NewRegistry()
	client := NewWithOptions(ClientOptions{
		Registry:      registry,
		StreamManager: streaming.NewManager(),
		EventStore:    store.NewMemoryEventStore(),
	})

	// non-running session with no history should emit synthetic done
	ch, cancel := client.Subscribe("not-found", executor.SubscribeOptions{})
	defer cancel()
	first := <-ch
	if first.Type != "done" {
		t.Fatalf("expected synthetic done, got %s", first.Type)
	}
}

type testExecutor struct {
	logs        chan executor.Log
	done        chan struct{}
	interrupted bool
	lastMessage string
	lastControl executor.ControlResponse
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

func (m *testExecutor) RespondControl(ctx context.Context, response executor.ControlResponse) error {
	m.lastControl = response
	return nil
}

func (m *testExecutor) Wait() error               { return nil }
func (m *testExecutor) Logs() <-chan executor.Log { return m.logs }
func (m *testExecutor) Done() <-chan struct{}     { return m.done }
func (m *testExecutor) Close() error              { return nil }

type resumeExecutor struct {
	logs        chan executor.Log
	done        chan struct{}
	startPrompt string
	startOpts   executor.Options
}

func (m *resumeExecutor) Start(ctx context.Context, prompt string, opts executor.Options) error {
	m.startPrompt = prompt
	m.startOpts = opts
	go func() {
		m.logs <- executor.Log{Type: "done", Content: "done"}
	}()
	return nil
}

func (m *resumeExecutor) Interrupt() error                                      { return nil }
func (m *resumeExecutor) SendMessage(ctx context.Context, message string) error { return nil }
func (m *resumeExecutor) RespondControl(ctx context.Context, response executor.ControlResponse) error {
	return nil
}
func (m *resumeExecutor) Wait() error               { return nil }
func (m *resumeExecutor) Logs() <-chan executor.Log { return m.logs }
func (m *resumeExecutor) Done() <-chan struct{}     { return m.done }
func (m *resumeExecutor) Close() error              { return nil }
