package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/supremeagent/executor/pkg/executor"
	"github.com/supremeagent/executor/pkg/sdk"
	"github.com/supremeagent/executor/pkg/store"
	"github.com/supremeagent/executor/pkg/streaming"
)

func TestHandlers(t *testing.T) {
	registry := executor.NewRegistry()
	sseMgr := streaming.NewManager()
	store := store.NewMemoryEventStore()
	client := sdk.NewWithOptions(sdk.ClientOptions{
		Registry:      registry,
		StreamManager: sseMgr,
		EventStore:    store,
	})
	handler := NewHandler(client)

	registry.Register(string(executor.ExecutorClaudeCode), executor.FactoryFunc(func() (executor.Executor, error) {
		return &mockExecutor{logs: make(chan executor.Log, 10), done: make(chan struct{})}, nil
	}))

	t.Run("HandleExecute", func(t *testing.T) {
		reqBody, _ := json.Marshal(ExecuteRequest{Prompt: "hello", Executor: executor.ExecutorClaudeCode})
		req, _ := http.NewRequest(http.MethodPost, "/execute", bytes.NewBuffer(reqBody))
		rr := httptest.NewRecorder()

		handler.HandleExecute(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rr.Code)
		}

		var resp ExecuteResponse
		_ = json.Unmarshal(rr.Body.Bytes(), &resp)
		if resp.SessionID == "" {
			t.Fatal("expected session ID")
		}
	})

	t.Run("HandleExecute_WithEnv", func(t *testing.T) {
		capture := &mockExecutor{logs: make(chan executor.Log, 10), done: make(chan struct{})}
		registry.Register("capture_env", executor.FactoryFunc(func() (executor.Executor, error) { return capture, nil }))

		reqBody, _ := json.Marshal(ExecuteRequest{
			Prompt:   "hello",
			Executor: "capture_env",
			Env:      map[string]string{"OPENAI_API_KEY": "test-key"},
		})
		req, _ := http.NewRequest(http.MethodPost, "/execute", bytes.NewBuffer(reqBody))
		rr := httptest.NewRecorder()

		handler.HandleExecute(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rr.Code)
		}
		if capture.lastOpts.Env["OPENAI_API_KEY"] != "test-key" {
			t.Fatal("expected env to be passed into executor options")
		}
	})

	t.Run("HandleContinue", func(t *testing.T) {
		sessionID := "test-session-continue"
		_, _ = registry.CreateSession(sessionID, string(executor.ExecutorClaudeCode), executor.Options{})

		reqBody, _ := json.Marshal(ContinueRequest{Message: "keep going"})
		req, _ := http.NewRequest(http.MethodPost, "/continue/"+sessionID, bytes.NewBuffer(reqBody))
		req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})
		rr := httptest.NewRecorder()

		handler.HandleContinue(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rr.Code)
		}
	})

	t.Run("HandleStream", func(t *testing.T) {
		sessionID := "test-session-stream"
		sseMgr.AppendLog(sessionID, streaming.LogEntry{Type: "stdout", Content: "historical"})

		req, _ := http.NewRequest(http.MethodGet, "/stream/"+sessionID, nil)

		ctx, cancel := context.WithCancel(context.Background())
		req = req.WithContext(ctx)
		req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})
		rr := httptest.NewRecorder()

		go func() {
			time.Sleep(100 * time.Millisecond)
			cancel()
		}()

		handler.HandleStream(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rr.Code)
		}
	})

	t.Run("HandleStream_Unregister", func(t *testing.T) {
		sessionID := "test-session-stream-unregister"
		_, _ = registry.CreateSession(sessionID, string(executor.ExecutorClaudeCode), executor.Options{})

		req, _ := http.NewRequest(http.MethodGet, "/stream/"+sessionID, nil)
		req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})
		rr := httptest.NewRecorder()

		go func() {
			time.Sleep(100 * time.Millisecond)
			sseMgr.UnregisterSession(sessionID)
		}()

		handler.HandleStream(rr, req)
	})

	t.Run("HandleStream_NewLog", func(t *testing.T) {
		sessionID := "test-session-stream-newlog"
		_, _ = registry.CreateSession(sessionID, string(executor.ExecutorClaudeCode), executor.Options{})

		req, _ := http.NewRequest(http.MethodGet, "/stream/"+sessionID, nil)
		req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})
		rr := httptest.NewRecorder()

		go func() {
			time.Sleep(100 * time.Millisecond)
			sseMgr.AppendLog(sessionID, streaming.LogEntry{Type: "done", Content: "done"})
		}()

		handler.HandleStream(rr, req)
	})

	t.Run("HandleStream_FilterDebugByDefault", func(t *testing.T) {
		sessionID := "test-session-stream-debug-filtered"
		_, _ = store.Append(context.Background(), executor.Event{SessionID: sessionID, Type: "debug", Content: "hidden-debug"})
		_, _ = store.Append(context.Background(), executor.Event{SessionID: sessionID, Type: "done", Content: "done"})

		req, _ := http.NewRequest(http.MethodGet, "/stream/"+sessionID+"?return_all=true", nil)
		req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})
		rr := httptest.NewRecorder()

		handler.HandleStream(rr, req)

		body := rr.Body.String()
		if strings.Contains(body, "event: debug") {
			t.Fatalf("expected debug event to be filtered by default, got body: %s", body)
		}
		if !strings.Contains(body, "event: done") {
			t.Fatalf("expected done event in response, got body: %s", body)
		}
	})

	t.Run("HandleStream_IncludeDebugWhenEnabled", func(t *testing.T) {
		sessionID := "test-session-stream-debug-enabled"
		_, _ = store.Append(context.Background(), executor.Event{SessionID: sessionID, Type: "debug", Content: "visible-debug"})
		_, _ = store.Append(context.Background(), executor.Event{SessionID: sessionID, Type: "done", Content: "done"})

		req, _ := http.NewRequest(http.MethodGet, "/stream/"+sessionID+"?debug=true&return_all=true", nil)
		req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})
		rr := httptest.NewRecorder()

		handler.HandleStream(rr, req)

		if !strings.Contains(rr.Body.String(), "event: debug") {
			t.Fatalf("expected debug event when debug=true, got body: %s", rr.Body.String())
		}
	})

	t.Run("HandleStream_NotReturnHistoryByDefault", func(t *testing.T) {
		sessionID := "test-session-stream-no-history-default"
		_, _ = store.Append(context.Background(), executor.Event{SessionID: sessionID, Type: "stdout", Content: "historical-stdout"})
		_, _ = store.Append(context.Background(), executor.Event{SessionID: sessionID, Type: "done", Content: "done"})

		req, _ := http.NewRequest(http.MethodGet, "/stream/"+sessionID, nil)
		req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})
		rr := httptest.NewRecorder()

		handler.HandleStream(rr, req)

		body := rr.Body.String()
		if strings.Contains(body, "historical-stdout") {
			t.Fatalf("expected historical logs to be excluded by default, got body: %s", body)
		}
		if !strings.Contains(body, "event: done") {
			t.Fatalf("expected done event in response, got body: %s", body)
		}
	})

	t.Run("HandleStream_ReturnHistoryWhenReturnAllEnabled", func(t *testing.T) {
		sessionID := "test-session-stream-return-all"
		_, _ = store.Append(context.Background(), executor.Event{SessionID: sessionID, Type: "stdout", Content: "historical-stdout"})
		_, _ = store.Append(context.Background(), executor.Event{SessionID: sessionID, Type: "done", Content: "done"})

		req, _ := http.NewRequest(http.MethodGet, "/stream/"+sessionID+"?return_all=true", nil)
		req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})
		rr := httptest.NewRecorder()

		handler.HandleStream(rr, req)

		body := rr.Body.String()
		if !strings.Contains(body, "historical-stdout") {
			t.Fatalf("expected historical logs when return_all=true, got body: %s", body)
		}
		if strings.Count(body, "event: done") != 1 {
			t.Fatalf("expected done event exactly once, got body: %s", body)
		}
	})

	t.Run("HandleExecute_InvalidBody", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, "/execute", bytes.NewBufferString("invalid json"))
		rr := httptest.NewRecorder()
		handler.HandleExecute(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("HandleExecute_EmptyPrompt", func(t *testing.T) {
		reqBody, _ := json.Marshal(ExecuteRequest{Prompt: ""})
		req, _ := http.NewRequest(http.MethodPost, "/execute", bytes.NewBuffer(reqBody))
		rr := httptest.NewRecorder()
		handler.HandleExecute(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("HandleContinue_NotFound", func(t *testing.T) {
		reqBody, _ := json.Marshal(ContinueRequest{Message: "hello"})
		req, _ := http.NewRequest(http.MethodPost, "/continue/not-found", bytes.NewBuffer(reqBody))
		req = mux.SetURLVars(req, map[string]string{"session_id": "not-found"})
		rr := httptest.NewRecorder()
		handler.HandleContinue(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", rr.Code)
		}
	})

	t.Run("HandleContinue_ResumeUnavailable", func(t *testing.T) {
		registry.Register(string(executor.ExecutorCodex), executor.FactoryFunc(func() (executor.Executor, error) {
			return &mockExecutor{logs: make(chan executor.Log, 10), done: make(chan struct{})}, nil
		}))

		reqBody, _ := json.Marshal(ExecuteRequest{Prompt: "resume me", Executor: executor.ExecutorCodex})
		reqExec, _ := http.NewRequest(http.MethodPost, "/execute", bytes.NewBuffer(reqBody))
		rrExec := httptest.NewRecorder()
		handler.HandleExecute(rrExec, reqExec)
		if rrExec.Code != http.StatusOK {
			t.Fatalf("expected execute 200, got %d", rrExec.Code)
		}
		var executeResp ExecuteResponse
		_ = json.Unmarshal(rrExec.Body.Bytes(), &executeResp)
		time.Sleep(50 * time.Millisecond)

		continueBody, _ := json.Marshal(ContinueRequest{Message: "continue"})
		req, _ := http.NewRequest(http.MethodPost, "/continue/"+executeResp.SessionID, bytes.NewBuffer(continueBody))
		req = mux.SetURLVars(req, map[string]string{"session_id": executeResp.SessionID})
		rr := httptest.NewRecorder()
		handler.HandleContinue(rr, req)
		if rr.Code != http.StatusConflict {
			t.Fatalf("expected 409 for resume unavailable, got %d", rr.Code)
		}
	})

	t.Run("HandleInterrupt_NotFound", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, "/interrupt/not-found", nil)
		req = mux.SetURLVars(req, map[string]string{"session_id": "not-found"})
		rr := httptest.NewRecorder()
		handler.HandleInterrupt(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", rr.Code)
		}
	})

	t.Run("HandleInterrupt_Error", func(t *testing.T) {
		sessionID := "test-session-interrupt-error"
		registry.Register("error_executor", executor.FactoryFunc(func() (executor.Executor, error) {
			return &mockErrorExecutor{mockExecutor: mockExecutor{logs: make(chan executor.Log, 10), done: make(chan struct{})}}, nil
		}))
		_, _ = registry.CreateSession(sessionID, "error_executor", executor.Options{})

		req, _ := http.NewRequest(http.MethodPost, "/interrupt/"+sessionID, nil)
		req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})
		rr := httptest.NewRecorder()

		handler.HandleInterrupt(rr, req)
		if rr.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d", rr.Code)
		}
	})

	t.Run("HandleEvents", func(t *testing.T) {
		reqBody, _ := json.Marshal(ExecuteRequest{
			Prompt:   "hello",
			Executor: executor.ExecutorClaudeCode,
		})
		reqExec, _ := http.NewRequest(http.MethodPost, "/execute", bytes.NewBuffer(reqBody))
		rrExec := httptest.NewRecorder()
		handler.HandleExecute(rrExec, reqExec)
		if rrExec.Code != http.StatusOK {
			t.Fatalf("expected execute 200, got %d", rrExec.Code)
		}
		var executeResp ExecuteResponse
		_ = json.Unmarshal(rrExec.Body.Bytes(), &executeResp)
		if executeResp.SessionID == "" {
			t.Fatalf("expected session id")
		}
		time.Sleep(50 * time.Millisecond)

		req, _ := http.NewRequest(http.MethodGet, "/events/"+executeResp.SessionID, nil)
		req = mux.SetURLVars(req, map[string]string{"session_id": executeResp.SessionID})
		rr := httptest.NewRecorder()
		handler.HandleEvents(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rr.Code)
		}
		if !strings.Contains(rr.Body.String(), "\"events\"") {
			t.Fatalf("expected events payload, got: %s", rr.Body.String())
		}
	})

	t.Run("HandleSessions", func(t *testing.T) {
		reqBody, _ := json.Marshal(ExecuteRequest{
			Prompt:   "session list test",
			Executor: executor.ExecutorClaudeCode,
		})
		reqExec, _ := http.NewRequest(http.MethodPost, "/execute", bytes.NewBuffer(reqBody))
		rrExec := httptest.NewRecorder()
		handler.HandleExecute(rrExec, reqExec)
		if rrExec.Code != http.StatusOK {
			t.Fatalf("expected execute 200, got %d", rrExec.Code)
		}

		time.Sleep(50 * time.Millisecond)
		req, _ := http.NewRequest(http.MethodGet, "/api/sessions", nil)
		rr := httptest.NewRecorder()
		handler.HandleSessions(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rr.Code)
		}
		body := rr.Body.String()
		if !strings.Contains(body, "\"sessions\"") || !strings.Contains(body, "session list test") {
			t.Fatalf("expected sessions payload with title, got: %s", body)
		}
	})

	t.Run("HandleControl", func(t *testing.T) {
		sessionID := "test-session-control"
		capture := &mockExecutor{logs: make(chan executor.Log, 10), done: make(chan struct{})}
		registry.Register("control_executor", executor.FactoryFunc(func() (executor.Executor, error) {
			return capture, nil
		}))
		_, _ = registry.CreateSession(sessionID, "control_executor", executor.Options{})

		reqBody, _ := json.Marshal(ControlResponse{
			RequestID: "req-1",
			Decision:  executor.ControlDecisionApprove,
		})
		req, _ := http.NewRequest(http.MethodPost, "/control/"+sessionID, bytes.NewBuffer(reqBody))
		req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})
		rr := httptest.NewRecorder()
		handler.HandleControl(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rr.Code)
		}
		if capture.lastControl.RequestID != "req-1" {
			t.Fatalf("expected control response delivered, got %+v", capture.lastControl)
		}
	})

	t.Run("HandleControl_InvalidDecision", func(t *testing.T) {
		sessionID := "test-session-control-invalid"
		_, _ = registry.CreateSession(sessionID, string(executor.ExecutorClaudeCode), executor.Options{})
		reqBody := []byte(`{"request_id":"req-2","decision":"maybe"}`)
		req, _ := http.NewRequest(http.MethodPost, "/control/"+sessionID, bytes.NewBuffer(reqBody))
		req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})
		rr := httptest.NewRecorder()
		handler.HandleControl(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("HandleControl_MissingRequestID", func(t *testing.T) {
		sessionID := "test-session-control-missing"
		_, _ = registry.CreateSession(sessionID, string(executor.ExecutorClaudeCode), executor.Options{})
		reqBody := []byte(`{"decision":"approve"}`)
		req, _ := http.NewRequest(http.MethodPost, "/control/"+sessionID, bytes.NewBuffer(reqBody))
		req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})
		rr := httptest.NewRecorder()
		handler.HandleControl(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rr.Code)
		}
	})
}

type mockExecutor struct {
	logs        chan executor.Log
	done        chan struct{}
	lastOpts    executor.Options
	lastControl executor.ControlResponse
}

func (m *mockExecutor) Start(ctx context.Context, prompt string, opts executor.Options) error {
	m.lastOpts = opts
	select {
	case m.logs <- executor.Log{Type: "done", Content: "done"}:
	default:
	}
	return nil
}

func (m *mockExecutor) Interrupt() error                                      { return nil }
func (m *mockExecutor) SendMessage(ctx context.Context, message string) error { return nil }
func (m *mockExecutor) RespondControl(ctx context.Context, response executor.ControlResponse) error {
	m.lastControl = response
	return nil
}
func (m *mockExecutor) Wait() error               { return nil }
func (m *mockExecutor) Logs() <-chan executor.Log { return m.logs }
func (m *mockExecutor) Done() <-chan struct{}     { return m.done }
func (m *mockExecutor) Close() error              { return nil }

type mockErrorExecutor struct {
	mockExecutor
}

func (m *mockErrorExecutor) Interrupt() error {
	return fmt.Errorf("interrupt error")
}
