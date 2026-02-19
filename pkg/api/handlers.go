package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/mylxsw/asteria/log"
	"github.com/supremeagent/executor/pkg/executor"
	"github.com/supremeagent/executor/pkg/streaming"
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
		Env:                        req.Env,
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
	defer func() {
		if err := recover(); err != nil {
			log.Errorf("HandleStream: panic recovered: %v", err)
		}
	}()

	vars := mux.Vars(r)
	sessionID := vars["session_id"]

	log.Debugf("HandleStream: started for session=%s", sessionID)
	debugEnabled, _ := strconv.ParseBool(r.URL.Query().Get("debug"))

	// Set headers for SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering if behind proxy

	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Error("HandleStream: flusher not supported")
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// 1. Subscribe FIRST to avoid race condition (missing logs between GetSession and Subscribe)
	newLogs, unsubscribe := h.sseMgr.Subscribe(sessionID)
	defer unsubscribe()

	// 2. Send historical logs (now safe because we're already subscribed)
	logs, _ := h.sseMgr.GetSession(sessionID)
	for _, logEntry := range logs {
		if logEntry.Type == "debug" && !debugEnabled {
			continue
		}
		data, _ := json.Marshal(logEntry)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", logEntry.Type, data)
	}
	flusher.Flush()

	// Check if session still running
	_, running := h.registry.GetSession(sessionID)
	log.Debugf("HandleStream: session=%s, running=%v, historical_logs=%d", sessionID, running, len(logs))
	if !running {
		// Session finished, just send "done" if not already in historical logs
		hasDone := false
		for _, l := range logs {
			if l.Type == "done" {
				hasDone = true
				break
			}
		}
		log.Debugf("HandleStream: session not running, hasDone=%v", hasDone)
		if !hasDone {
			fmt.Fprintf(w, "event: done\ndata: {}\n\n")
			flusher.Flush()
		}
		return
	}

	// 3. Keep connection open and pipe new logs
	// Note: Since we Subscribe BEFORE GetSession, historical logs were appended
	// when there were no subscribers, so they won't appear in the channel.
	// Only new logs after Subscribe will be sent to the channel.
	log.Debug("HandleStream: waiting for new logs")
	for {
		select {
		case logEntry, ok := <-newLogs:
			if !ok {
				log.Debug("HandleStream: channel closed")
				// Subscription channel closed
				fmt.Fprintf(w, "event: done\ndata: {}\n\n")
				flusher.Flush()
				return
			}

			log.Debugf("HandleStream: received log type=%s", logEntry.Type)
			if logEntry.Type == "debug" && !debugEnabled {
				continue
			}

			data, _ := json.Marshal(logEntry)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", logEntry.Type, data)
			flusher.Flush()
			log.Debugf("HandleStream: flushed log type=%s", logEntry.Type)

			if logEntry.Type == "done" {
				return
			}
		case <-r.Context().Done():
			log.Debug("HandleStream: client disconnected")
			// Client disconnected
			return
		}
	}
}
