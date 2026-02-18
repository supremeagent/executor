package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/anthropics/vibe-kanban/go-api/pkg/executor"
	"github.com/anthropics/vibe-kanban/go-api/pkg/streaming"
	"github.com/gorilla/mux"
)

func TestHandlers(t *testing.T) {
	registry := executor.NewRegistry()
	sseMgr := streaming.NewManager()
	handler := NewHandler(registry, sseMgr)

	// Register a mock executor
	registry.Register("claude_code", executor.FactoryFunc(func() (executor.Executor, error) {
		return &mockExecutor{
			logs: make(chan executor.Log, 10),
			done: make(chan struct{}),
		}, nil
	}))

	t.Run("HandleExecute", func(t *testing.T) {
		reqBody, _ := json.Marshal(ExecuteRequest{
			Prompt:   "hello",
			Executor: "claude_code",
		})
		req, _ := http.NewRequest("POST", "/execute", bytes.NewBuffer(reqBody))
		rr := httptest.NewRecorder()

		handler.HandleExecute(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rr.Code)
		}

		var resp ExecuteResponse
		json.Unmarshal(rr.Body.Bytes(), &resp)
		if resp.SessionID == "" {
			t.Error("expected session ID")
		}
	})

	t.Run("HandleContinue", func(t *testing.T) {
		sessionID := "test-session-continue"
		registry.CreateSession(sessionID, "claude_code", executor.Options{})

		reqBody, _ := json.Marshal(ContinueRequest{Message: "keep going"})
		req, _ := http.NewRequest("POST", "/continue/"+sessionID, bytes.NewBuffer(reqBody))
		req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})
		rr := httptest.NewRecorder()

		handler.HandleContinue(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rr.Code)
		}
	})

	t.Run("HandleStream", func(t *testing.T) {
		sessionID := "test-session-stream"
		// Append some historical logs
		sseMgr.AppendLog(sessionID, streaming.LogEntry{Type: "stdout", Content: "historical"})
		
		req, _ := http.NewRequest("GET", "/stream/"+sessionID, nil)
		req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})
		
		// Use a context with timeout to stop the streaming handler
		ctx, cancel := context.WithCancel(context.Background())
		req = req.WithContext(ctx)
		
		rr := httptest.NewRecorder()
		
		go func() {
			// Wait a bit then cancel
			time.Sleep(100 * time.Millisecond)
			cancel()
		}()

		handler.HandleStream(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rr.Code)
		}
	})

	t.Run("HandleStream_Unregister", func(t *testing.T) {
		sessionID := "test-session-stream-unregister"
		registry.CreateSession(sessionID, "claude_code", executor.Options{})
		
		req, _ := http.NewRequest("GET", "/stream/"+sessionID, nil)
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
		registry.CreateSession(sessionID, "claude_code", executor.Options{})
		
		req, _ := http.NewRequest("GET", "/stream/"+sessionID, nil)
		req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})
		rr := httptest.NewRecorder()
		
		go func() {
			time.Sleep(100 * time.Millisecond)
			sseMgr.AppendLog(sessionID, streaming.LogEntry{Type: "done", Content: "done"})
		}()

		handler.HandleStream(rr, req)
	})

	t.Run("HandleExecute_InvalidBody", func(t *testing.T) {
		req, _ := http.NewRequest("POST", "/execute", bytes.NewBufferString("invalid json"))
		rr := httptest.NewRecorder()
		handler.HandleExecute(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("HandleExecute_EmptyPrompt", func(t *testing.T) {
		reqBody, _ := json.Marshal(ExecuteRequest{Prompt: ""})
		req, _ := http.NewRequest("POST", "/execute", bytes.NewBuffer(reqBody))
		rr := httptest.NewRecorder()
		handler.HandleExecute(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("HandleContinue_NotFound", func(t *testing.T) {
		reqBody, _ := json.Marshal(ContinueRequest{Message: "hello"})
		req, _ := http.NewRequest("POST", "/continue/not-found", bytes.NewBuffer(reqBody))
		req = mux.SetURLVars(req, map[string]string{"session_id": "not-found"})
		rr := httptest.NewRecorder()
		handler.HandleContinue(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", rr.Code)
		}
	})

	t.Run("HandleInterrupt_NotFound", func(t *testing.T) {
		req, _ := http.NewRequest("POST", "/interrupt/not-found", nil)
		req = mux.SetURLVars(req, map[string]string{"session_id": "not-found"})
		rr := httptest.NewRecorder()
		handler.HandleInterrupt(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", rr.Code)
		}
	})

	t.Run("HandleInterrupt_Error", func(t *testing.T) {
		sessionID := "test-session-interrupt-error"
		registry.Register("error_executor", executor.FactoryFunc(func() (executor.Executor, error) {
			return &mockErrorExecutor{
				mockExecutor: mockExecutor{
					logs: make(chan executor.Log, 10),
					done: make(chan struct{}),
				},
			}, nil
		}))
		registry.CreateSession(sessionID, "error_executor", executor.Options{})

		req, _ := http.NewRequest("POST", "/interrupt/"+sessionID, nil)
		req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})
		rr := httptest.NewRecorder()

		handler.HandleInterrupt(rr, req)
		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", rr.Code)
		}
	})
}

type mockExecutor struct {
	logs chan executor.Log
	done chan struct{}
}

func (m *mockExecutor) Start(ctx context.Context, prompt string, opts executor.Options) error {
	select {
	case m.logs <- executor.Log{Type: "done", Content: "done"}:
	default:
	}
	return nil
}
func (m *mockExecutor) Interrupt() error                                             { return nil }
func (m *mockExecutor) SendMessage(ctx context.Context, message string) error         { return nil }
func (m *mockExecutor) Wait() error                                                  { return nil }
func (m *mockExecutor) Logs() <-chan executor.Log                                     { return m.logs }
func (m *mockExecutor) Done() <-chan struct{}                                        { return m.done }
func (m *mockExecutor) Close() error                                                 { return nil }

type mockErrorExecutor struct {
	mockExecutor
}

func (m *mockErrorExecutor) Interrupt() error {
	return fmt.Errorf("interrupt error")
}
