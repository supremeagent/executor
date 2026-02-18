package api

import (
	"net/http"

	"github.com/gorilla/mux"
)

// NewRouter creates a new HTTP router
func NewRouter(handler *Handler) *mux.Router {
	router := mux.NewRouter()

	// Add middleware
	router.Use(LoggingMiddleware)
	router.Use(RecoveryMiddleware)

	// API routes
	router.HandleFunc("/api/execute", handler.HandleExecute).Methods(http.MethodPost)
	router.HandleFunc("/api/execute/{session_id}/continue", handler.HandleContinue).Methods(http.MethodPost)
	router.HandleFunc("/api/execute/{session_id}/interrupt", handler.HandleInterrupt).Methods(http.MethodPost)
	router.HandleFunc("/api/execute/{session_id}/stream", handler.HandleStream).Methods(http.MethodGet)

	// Health check
	router.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}).Methods(http.MethodGet)

	return router
}
