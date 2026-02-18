package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/anthropics/vibe-kanban/go-api/pkg/executor"
	"github.com/anthropics/vibe-kanban/go-api/pkg/streaming"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// Handler handles API requests
type Handler struct {
	registry *executor.Registry
	sseMgr   *streaming.Manager
}

// NewHandler creates a new API handler
func NewHandler(registry *executor.Registry, sseMgr *streaming.Manager) *Handler {
	return &Handler{
		registry: registry,
		sseMgr:   sseMgr,
	}
}

// HandleExecute handles the execute endpoint
func (h *Handler) HandleExecute(w http.ResponseWriter, r *http.Request) {
	var req ExecuteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	// Validate request
	if req.Prompt == "" {
		http.Error(w, "prompt is required", http.StatusBadRequest)
		return
	}

	if req.Executor == "" {
		req.Executor = "claude_code"
	}

	// Generate session ID
	sessionID := uuid.New().String()

	// Create executor options
	opts := executor.Options{
		WorkingDir:                 req.WorkingDir,
		Model:                      req.Model,
		Plan:                       req.Plan,
		DangerouslySkipPermissions: !req.Plan,
		Sandbox:                    req.Sandbox,
		AskForApproval:             req.AskForApproval,
	}

	// Create executor session
	exec, err := h.registry.CreateSession(sessionID, req.Executor, opts)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to create executor: %v", err), http.StatusInternalServerError)
		return
	}

	// Start executor
	ctx := context.Background()
	if err := exec.Start(ctx, req.Prompt, opts); err != nil {
		exec.Close()
		h.registry.RemoveSession(sessionID)
		http.Error(w, fmt.Sprintf("failed to start executor: %v", err), http.StatusInternalServerError)
		return
	}

	// Background goroutine to pipe logs to Manager
	go func() {
		defer func() {
			exec.Close()
			h.registry.RemoveSession(sessionID)
		}()

		logsChan := exec.Logs()
		for logEntry := range logsChan {
			h.sseMgr.AppendLog(sessionID, streaming.LogEntry{
				Type:    logEntry.Type,
				Content: logEntry.Content,
			})
			if logEntry.Type == "done" {
				break
			}
		}
	}()

	// Return response
	resp := ExecuteResponse{
		SessionID: sessionID,
		Status:    "running",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// HandleContinue handles the continue endpoint
func (h *Handler) HandleContinue(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	sessionID := vars["session_id"]

	var req ContinueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	// Get executor
	exec, ok := h.registry.GetSession(sessionID)
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	// Send message
	if err := exec.SendMessage(r.Context(), req.Message); err != nil {
		http.Error(w, fmt.Sprintf("failed to send message: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// HandleInterrupt handles the interrupt endpoint
func (h *Handler) HandleInterrupt(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	sessionID := vars["session_id"]

	// Get executor
	exec, ok := h.registry.GetSession(sessionID)
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	// Interrupt execution
	if err := exec.Interrupt(); err != nil {
		http.Error(w, fmt.Sprintf("failed to interrupt: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "interrupted"})
}

// HandleStream handles the SSE stream endpoint
func (h *Handler) HandleStream(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	sessionID := vars["session_id"]

	// Set headers for SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	var flusher http.Flusher
	if f, ok := w.(http.Flusher); ok {
		flusher = f
	}

	// 1. Send historical logs
	logs, _ := h.sseMgr.GetSession(sessionID)
	for _, logEntry := range logs {
		data, _ := json.Marshal(logEntry)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", logEntry.Type, data)
	}
	if flusher != nil {
		flusher.Flush()
	}

	// Check if session still running
	_, running := h.registry.GetSession(sessionID)
	if !running {
		// Session finished, just send "done" if not already in historical logs
		hasDone := false
		for _, l := range logs {
			if l.Type == "done" {
				hasDone = true
				break
			}
		}
		if !hasDone {
			fmt.Fprintf(w, "event: done\ndata: {}\n\n")
			if flusher != nil {
				flusher.Flush()
			}
		}
		return
	}

	// 2. Subscribe to new logs
	newLogs, unsubscribe := h.sseMgr.Subscribe(sessionID)
	defer unsubscribe()

	// Keep connection open and pipe new logs
	for {
		select {
		case logEntry, ok := <-newLogs:
			if !ok {
				// Subscription channel closed
				fmt.Fprintf(w, "event: done\ndata: {}\n\n")
				if flusher != nil {
					flusher.Flush()
				}
				return
			}

			data, _ := json.Marshal(logEntry)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", logEntry.Type, data)
			if flusher != nil {
				flusher.Flush()
			}

			if logEntry.Type == "done" {
				return
			}
		case <-r.Context().Done():
			// Client disconnected
			return
		}
	}
}
