package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anthropics/vibe-kanban/go-api/pkg/executor"
	"github.com/anthropics/vibe-kanban/go-api/pkg/streaming"
)

func TestMiddleware(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("LoggingMiddleware", func(t *testing.T) {
		mw := LoggingMiddleware(handler)
		req, _ := http.NewRequest("GET", "/", nil)
		rr := httptest.NewRecorder()
		mw.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rr.Code)
		}
	})

	t.Run("RecoveryMiddleware", func(t *testing.T) {
		panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			panic("test panic")
		})
		mw := RecoveryMiddleware(panicHandler)
		req, _ := http.NewRequest("GET", "/", nil)
		rr := httptest.NewRecorder()
		mw.ServeHTTP(rr, req)
		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", rr.Code)
		}
	})
}

func TestRouter(t *testing.T) {
	registry := executor.NewRegistry()
	sseMgr := streaming.NewManager()
	handler := NewHandler(registry, sseMgr)
	router := NewRouter(handler)

	if router == nil {
		t.Fatal("router should not be nil")
	}
}
