package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type ExecutorType string

// Built-in executor identifiers.
const (
	ExecutorClaudeCode ExecutorType = "claude_code"
	ExecutorCodex      ExecutorType = "codex"
	ExecutorDroid      ExecutorType = "droid"
	ExecutorGemini     ExecutorType = "gemini"
	ExecutorQwen       ExecutorType = "qwen"
	ExecutorCopilot    ExecutorType = "copilot"
)

// ExecuteRequest defines task startup options.
type ExecuteRequest struct {
	Prompt         string            `json:"prompt"`
	Executor       ExecutorType      `json:"executor"`
	WorkingDir     string            `json:"working_dir"`
	Model          string            `json:"model,omitempty"`
	Plan           bool              `json:"plan,omitempty"`
	Sandbox        string            `json:"sandbox,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	AskForApproval string            `json:"ask_for_approval,omitempty"`
}

// ExecuteResponse is returned after a task starts.
type ExecuteResponse struct {
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
}

// ContinueRequest defines a resume/continue payload.
type ContinueRequest struct {
	Message string `json:"message"`
}

type ControlDecision string

const (
	ControlDecisionApprove ControlDecision = "approve"
	ControlDecisionDeny    ControlDecision = "deny"
)

// ControlRequest describes a pending executor control/approval action.
type ControlRequest struct {
	RequestID string    `json:"request_id"`
	Executor  string    `json:"executor"`
	Type      string    `json:"type"`
	ToolName  string    `json:"tool_name,omitempty"`
	Message   string    `json:"message,omitempty"`
	Payload   any       `json:"payload,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// ControlResponse is used to answer a pending ControlRequest.
type ControlResponse struct {
	RequestID string          `json:"request_id"`
	Decision  ControlDecision `json:"decision"`
	Reason    string          `json:"reason,omitempty"`
}

type SessionStatus string

const (
	SessionStatusRunning     SessionStatus = "running"
	SessionStatusDone        SessionStatus = "done"
	SessionStatusInterrupted SessionStatus = "interrupted"
)

// Session represents one task session summary.
type Session struct {
	SessionID string        `json:"session_id"`
	Title     string        `json:"title"`
	Status    SessionStatus `json:"status"`
	Executor  ExecutorType  `json:"executor"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

// Event represents one streamed task event.
type Event struct {
	SessionID string    `json:"session_id,omitempty"`
	Executor  string    `json:"executor,omitempty"`
	Seq       uint64    `json:"seq,omitempty"`
	Timestamp time.Time `json:"timestamp,omitempty"`
	Type      string    `json:"type"`
	Content   any       `json:"content"`
}

// SubscribeOptions configures event subscription behavior.
type SubscribeOptions struct {
	ReturnAll    bool
	IncludeDebug bool
	AfterSeq     uint64
	Limit        int
}

// Hooks allows callers to observe session lifecycle and persistence behavior.
type Hooks struct {
	OnSessionStart func(ctx context.Context, sessionID string, req ExecuteRequest)
	OnEventStored  func(ctx context.Context, evt Event)
	OnSessionEnd   func(ctx context.Context, sessionID string)
	OnStoreError   func(ctx context.Context, sessionID string, evt Event, err error)
}

// EventTransformer transforms executor logs to a unified stream event.
// Returning an Event with empty Type/SessionID/Executor falls back to defaults.
type EventTransformer func(input TransformInput) Event

// TransformInput contains original executor output metadata.
type TransformInput struct {
	SessionID string
	Executor  string
	Log       Log
}

// UnifiedContent is the default normalized event payload.
type UnifiedContent struct {
	Source     string `json:"source"`
	SourceType string `json:"source_type"`
	Category   string `json:"category"`
	Action     string `json:"action,omitempty"`
	Phase      string `json:"phase,omitempty"`
	Summary    string `json:"summary,omitempty"`
	Target     string `json:"target,omitempty"`
	Text       string `json:"text,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	RequestID  string `json:"request_id,omitempty"`
	Status     string `json:"status,omitempty"`
	Raw        any    `json:"raw,omitempty"`
}

func StringifyContent(v any) string {
	switch val := v.(type) {
	case nil:
		return ""
	case string:
		return val
	case json.RawMessage:
		return string(val)
	default:
		return fmt.Sprintf("%v", val)
	}
}
