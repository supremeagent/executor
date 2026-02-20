package httpapi

import (
	"net/http"

	"github.com/gorilla/mux"
)

// NewRouter creates a new HTTP router.
func NewRouter(handler *Handler) *mux.Router {
	router := mux.NewRouter()
	router.Use(LoggingMiddleware)
	router.Use(RecoveryMiddleware)

	router.HandleFunc("/api/execute", handler.HandleExecute).Methods(http.MethodPost)
	router.HandleFunc("/api/execute/{session_id}/continue", handler.HandleContinue).Methods(http.MethodPost)
	router.HandleFunc("/api/execute/{session_id}/interrupt", handler.HandleInterrupt).Methods(http.MethodPost)
	router.HandleFunc("/api/execute/{session_id}/stream", handler.HandleStream).Methods(http.MethodGet)
	router.HandleFunc("/api/execute/{session_id}/events", handler.HandleEvents).Methods(http.MethodGet)

	router.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}).Methods(http.MethodGet)

	return router
}
