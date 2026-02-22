package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/mylxsw/asteria/log"
	"github.com/supremeagent/executor/pkg/executor"
	"github.com/supremeagent/executor/pkg/sdk"
)

// Handler handles HTTP API requests.
type Handler struct {
	client *sdk.Client
}

func NewHandler(client *sdk.Client) *Handler {
	return &Handler{client: client}
}

func (h *Handler) HandleExecute(w http.ResponseWriter, r *http.Request) {
	var req ExecuteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	resp, err := h.client.Execute(r.Context(), req)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sdk.ErrPromptRequired) || errors.Is(err, executor.ErrUnknownExecutorType) {
			status = http.StatusBadRequest
		}
		http.Error(w, err.Error(), status)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *Handler) HandleContinue(w http.ResponseWriter, r *http.Request) {
	sessionID := mux.Vars(r)["session_id"]

	var req ContinueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if err := h.client.ContinueTask(r.Context(), sessionID, req.Message); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, executor.ErrSessionNotFound) {
			status = http.StatusNotFound
		} else if errors.Is(err, sdk.ErrResumeUnavailable) {
			status = http.StatusConflict
		}
		http.Error(w, fmt.Sprintf("failed to continue: %v", err), status)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (h *Handler) HandleInterrupt(w http.ResponseWriter, r *http.Request) {
	sessionID := mux.Vars(r)["session_id"]

	if err := h.client.PauseTask(sessionID); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, executor.ErrSessionNotFound) {
			status = http.StatusNotFound
		}
		http.Error(w, fmt.Sprintf("failed to interrupt: %v", err), status)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "interrupted"})
}

func (h *Handler) HandleControl(w http.ResponseWriter, r *http.Request) {
	sessionID := mux.Vars(r)["session_id"]
	var req ControlResponse
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}
	if req.RequestID == "" {
		http.Error(w, "request_id is required", http.StatusBadRequest)
		return
	}
	if req.Decision != executor.ControlDecisionApprove && req.Decision != executor.ControlDecisionDeny {
		http.Error(w, "decision must be approve or deny", http.StatusBadRequest)
		return
	}

	if err := h.client.RespondControl(r.Context(), sessionID, req); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, executor.ErrSessionNotFound) {
			status = http.StatusNotFound
		}
		http.Error(w, fmt.Sprintf("failed to respond control request: %v", err), status)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (h *Handler) HandleStream(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if err := recover(); err != nil {
			log.Errorf("HandleStream: panic recovered: %v", err)
		}
	}()

	sessionID := mux.Vars(r)["session_id"]
	debugEnabled, _ := strconv.ParseBool(r.URL.Query().Get("debug"))
	returnAll, _ := strconv.ParseBool(r.URL.Query().Get("return_all"))

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	events, unsubscribe := h.client.Subscribe(sessionID, executor.SubscribeOptions{
		ReturnAll:    returnAll,
		IncludeDebug: debugEnabled,
	})
	defer unsubscribe()

	for {
		select {
		case evt, ok := <-events:
			if !ok {
				return
			}

			data, _ := json.Marshal(evt)
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.Type, data)
			flusher.Flush()

			if evt.Type == "done" {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}

func (h *Handler) HandleEvents(w http.ResponseWriter, r *http.Request) {
	sessionID := mux.Vars(r)["session_id"]

	afterSeq, err := strconv.ParseUint(r.URL.Query().Get("after_seq"), 10, 64)
	if err != nil {
		afterSeq = 0
	}
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil {
		limit = 0
	}

	events, err := h.client.ListEvents(r.Context(), sessionID, afterSeq, limit)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to list events: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"session_id": sessionID,
		"events":     events,
	})
}

func (h *Handler) HandleSessions(w http.ResponseWriter, r *http.Request) {
	sessions := h.client.ListSessions(r.Context())

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"sessions": sessions,
	})
}

// HandleExecutors returns the list of available executors
func (h *Handler) HandleExecutors(w http.ResponseWriter, r *http.Request) {
	executorsList := h.client.Executors()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"executors": executorsList,
	})
}
