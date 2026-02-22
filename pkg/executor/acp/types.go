// Package acp provides a shared client for AI executors that speak the
// Agent Client Protocol (ACP) over line-delimited JSON on stdin/stdout.
//
// Gemini CLI, Qwen Code, and GitHub Copilot all use the ACP protocol.
// The harness in this package handles process lifecycle, event parsing,
// and the session/approval handshake that is common across all three.
package acp

import "encoding/json"

// EventType identifies the kind of an ACP event line.
type EventType string

const (
	// EventTypeSessionStart is emitted once with the session ID.
	EventTypeSessionStart EventType = "SessionStart"
	// EventTypeMessage carries assistant text content (may be incremental).
	EventTypeMessage EventType = "Message"
	// EventTypeThought carries thinking/reasoning text.
	EventTypeThought EventType = "Thought"
	// EventTypeToolCall is sent when a tool is invoked.
	EventTypeToolCall EventType = "ToolCall"
	// EventTypeToolUpdate updates an in-progress tool call.
	EventTypeToolUpdate EventType = "ToolUpdate"
	// EventTypePlan carries the agent's task plan.
	EventTypePlan EventType = "Plan"
	// EventTypeApprovalRequest is sent when the agent needs permission.
	EventTypeApprovalRequest EventType = "RequestPermission"
	// EventTypeDone signals that the agent finished responding.
	EventTypeDone EventType = "Done"
	// EventTypeError carries an error message.
	EventTypeError EventType = "Error"
	// EventTypeUser echoes back user messages.
	EventTypeUser EventType = "User"
)

// Event is the top-level envelope for a single ACP output line.
// Each line is a JSON object with exactly one of the typed fields set
// (using the serde-style enum-as-object representation used by the Rust SDK).
type Event struct {
	// One of the EventType* constants.
	Type EventType `json:"-"`
	// Raw unparsed JSON payload for fields we do not inspect structurally.
	Raw json.RawMessage `json:"-"`
}

// ToolStatus represents the lifecycle state of a tool call.
type ToolStatus string

const (
	ToolStatusPending    ToolStatus = "pending"
	ToolStatusInProgress ToolStatus = "in_progress"
	ToolStatusCompleted  ToolStatus = "completed"
	ToolStatusFailed     ToolStatus = "failed"
)

// ToolKind classifies the kind of operation a tool performs.
type ToolKind string

const (
	ToolKindRead    ToolKind = "Read"
	ToolKindEdit    ToolKind = "Edit"
	ToolKindExecute ToolKind = "Execute"
	ToolKindSearch  ToolKind = "Search"
	ToolKindFetch   ToolKind = "Fetch"
	ToolKindDelete  ToolKind = "Delete"
	ToolKindThink   ToolKind = "Think"
	ToolKindOther   ToolKind = "Other"
)

// ToolCall carries the structured data for a single tool invocation.
type ToolCall struct {
	ID     string     `json:"tool_call_id"`
	Kind   ToolKind   `json:"kind"`
	Title  string     `json:"title"`
	Status ToolStatus `json:"status"`
	// Raw input arguments (tool-specific schema).
	RawInput json.RawMessage `json:"raw_input,omitempty"`
	// Raw output (tool-specific result).
	RawOutput json.RawMessage `json:"raw_output,omitempty"`
}

// PlanEntry is one step in the agent's plan.
type PlanEntry struct {
	Content  string `json:"content"`
	Status   string `json:"status"`
	Priority string `json:"priority,omitempty"`
}

// Plan carries the agent's task plan emitted by plan-mode agents.
type Plan struct {
	Entries []PlanEntry `json:"entries"`
}

// PermissionRequest is emitted when the agent needs user approval.
type PermissionRequest struct {
	ToolCallID string   `json:"tool_call_id"`
	ToolCall   ToolCall `json:"tool_call"`
}

// parseEvent converts a raw JSON line to an Event, identifying its type
// via the single top-level key that matches one of the EventType constants.
func parseEvent(line []byte) (Event, bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(line, &raw); err != nil {
		return Event{}, false
	}

	types := []EventType{
		EventTypeSessionStart,
		EventTypeMessage,
		EventTypeThought,
		EventTypeToolCall,
		EventTypeToolUpdate,
		EventTypePlan,
		EventTypeApprovalRequest,
		EventTypeDone,
		EventTypeError,
		EventTypeUser,
	}

	for _, t := range types {
		if payload, ok := raw[string(t)]; ok {
			return Event{Type: t, Raw: payload}, true
		}
	}

	return Event{}, false
}
