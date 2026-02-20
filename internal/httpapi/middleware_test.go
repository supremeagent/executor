package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/supremeagent/executor/pkg/sdk"
)

func TestMiddleware(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("LoggingMiddleware", func(t *testing.T) {
		mw := LoggingMiddleware(handler)
		req, _ := http.NewRequest(http.MethodGet, "/", nil)
		rr := httptest.NewRecorder()
		mw.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rr.Code)
		}
	})

	t.Run("RecoveryMiddleware", func(t *testing.T) {
		panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			panic("test panic")
		})
		mw := RecoveryMiddleware(panicHandler)
		req, _ := http.NewRequest(http.MethodGet, "/", nil)
		rr := httptest.NewRecorder()
		mw.ServeHTTP(rr, req)
		if rr.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d", rr.Code)
		}
	})
}

func TestRouter(t *testing.T) {
	handler := NewHandler(sdk.New())
	router := NewRouter(handler)
	if router == nil {
		t.Fatal("router should not be nil")
	}
}
