// Package droid provides type definitions for the Droid executor stream-json protocol.
package droid

// Autonomy represents the permission level for Droid's file and system operations.
type Autonomy string

const (
	// AutonomyNormal uses the default permission level with prompts.
	AutonomyNormal Autonomy = "normal"
	// AutonomyLow uses a low-autonomy mode (--auto low).
	AutonomyLow Autonomy = "low"
	// AutonomyMedium uses a medium-autonomy mode (--auto medium).
	AutonomyMedium Autonomy = "medium"
	// AutonomyHigh uses a high-autonomy mode (--auto high).
	AutonomyHigh Autonomy = "high"
	// AutonomySkipPermissionsUnsafe bypasses all permission checks (--skip-permissions-unsafe).
	AutonomySkipPermissionsUnsafe Autonomy = "skip-permissions-unsafe"
)

// ReasoningEffort controls how much computation Droid spends on reasoning.
type ReasoningEffort string

const (
	ReasoningEffortNone    ReasoningEffort = "none"
	ReasoningEffortDynamic ReasoningEffort = "dynamic"
	ReasoningEffortOff     ReasoningEffort = "off"
	ReasoningEffortLow     ReasoningEffort = "low"
	ReasoningEffortMedium  ReasoningEffort = "medium"
	ReasoningEffortHigh    ReasoningEffort = "high"
)

// EventType is the value of the "type" field in Droid stream-json output.
type EventType string

const (
	EventTypeSystem     EventType = "system"
	EventTypeMessage    EventType = "message"
	EventTypeToolCall   EventType = "tool_call"
	EventTypeToolResult EventType = "tool_result"
	EventTypeCompletion EventType = "completion"
	EventTypeError      EventType = "error"
)

// DroidEvent is the common envelope for all Droid stream-json lines.
// Fields not present in a given event type are omitted (zero-valued).
type DroidEvent struct {
	Type EventType `json:"type"`

	// System fields
	SessionID string   `json:"session_id,omitempty"`
	Model     string   `json:"model,omitempty"`
	Tools     []string `json:"tools,omitempty"`

	// Message fields
	Role      string `json:"role,omitempty"`
	ID        string `json:"id,omitempty"`
	Text      string `json:"text,omitempty"`
	Timestamp uint64 `json:"timestamp,omitempty"`

	// ToolCall fields
	MessageID string `json:"messageId,omitempty"`
	ToolID    string `json:"toolId,omitempty"`
	ToolName  string `json:"toolName,omitempty"`

	// ToolResult fields
	IsError bool `json:"isError,omitempty"`

	// Completion fields
	FinalText  string `json:"finalText,omitempty"`
	NumTurns   int    `json:"numTurns,omitempty"`
	DurationMs int64  `json:"durationMs,omitempty"`

	// Error fields
	Message string `json:"message,omitempty"`
	Source  string `json:"source,omitempty"`
}
