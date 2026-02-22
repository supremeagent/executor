package qwen

import (
	"encoding/json"

	"github.com/google/uuid"
)

// PermissionMode represents the permission mode for Claude Code
type PermissionMode string

const (
	PermissionModeDefault        PermissionMode = "default"
	PermissionModeAcceptEdits    PermissionMode = "acceptEdits"
	PermissionModePlan           PermissionMode = "plan"
	PermissionModeBypassPermissions PermissionMode = "bypassPermissions"
)

// CLIMessage represents the top-level message from Claude Code CLI stdout
type CLIMessage struct {
	Type string `json:"type"`
}

// ControlRequest represents a control request from Claude Code
type ControlRequest struct {
	RequestID  string           `json:"request_id"`
	Request    ControlRequestType `json:"request"`
}

// ControlRequestType represents the type of control request
type ControlRequestType struct {
	Subtype              string          `json:"subtype"`
	ToolName             string          `json:"tool_name,omitempty"`
	Input                json.RawMessage `json:"input,omitempty"`
	PermissionSuggestions []PermissionUpdate `json:"permission_suggestions,omitempty"`
	BlockedPaths         string          `json:"blocked_paths,omitempty"`
	ToolUseID            string          `json:"tool_use_id,omitempty"`
	CallbackID           string          `json:"callback_id,omitempty"`
}

// PermissionUpdate represents a permission update operation
type PermissionUpdate struct {
	UpdateType   string `json:"type"`
	Mode        string `json:"mode,omitempty"`
	Destination string `json:"destination,omitempty"`
	Rules       []PermissionRuleValue `json:"rules,omitempty"`
	Behavior    string `json:"behavior,omitempty"`
	Directories []string `json:"directories,omitempty"`
}

// PermissionRuleValue represents a permission rule
type PermissionRuleValue struct {
	ToolName    string `json:"tool_name"`
	RuleContent string `json:"rule_content,omitempty"`
}

// ControlResponse represents a control response from the SDK
type ControlResponse struct {
	Type      string            `json:"type"`
	Response  ControlResponseType `json:"response"`
}

// ControlResponseType represents the type of control response
type ControlResponseType struct {
	Subtype   string           `json:"subtype"`
	RequestID string           `json:"request_id"`
	Response  *json.RawMessage `json:"response,omitempty"`
	Error     *string          `json:"error,omitempty"`
}

// SDKControlRequest represents an outgoing control request to Claude Code
type SDKControlRequest struct {
	Type       string                  `json:"type"`
	RequestID  string                  `json:"request_id"`
	Request    SDKControlRequestType  `json:"request"`
}

// SDKControlRequestType represents the SDK control request type
type SDKControlRequestType struct {
	Subtype string          `json:"subtype"`
	Mode    PermissionMode `json:"mode,omitempty"`
	Hooks   json.RawMessage `json:"hooks,omitempty"`
}

// Message represents a user message to Claude Code
type Message struct {
	Type   string          `json:"type"`
	User   ClaudeUserMessage `json:"user"`
}

// ClaudeUserMessage represents a user message
type ClaudeUserMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// NewUserMessage creates a new user message
func NewUserMessage(content string) Message {
	return Message{
		Type: "message",
		User: ClaudeUserMessage{
			Role:    "user",
			Content: content,
		},
	}
}

// NewInitializeRequest creates a new initialize request
func NewInitializeRequest() SDKControlRequest {
	return SDKControlRequest{
		Type:      "control_request",
		RequestID: newRequestID(),
		Request: SDKControlRequestType{
			Subtype: "initialize",
		},
	}
}

// NewSetPermissionModeRequest creates a new set permission mode request
func NewSetPermissionModeRequest(mode PermissionMode) SDKControlRequest {
	return SDKControlRequest{
		Type:      "control_request",
		RequestID: newRequestID(),
		Request: SDKControlRequestType{
			Subtype: "setPermissionMode",
			Mode:    mode,
		},
	}
}

// NewInterruptRequest creates a new interrupt request
func NewInterruptRequest() SDKControlRequest {
	return SDKControlRequest{
		Type:      "control_request",
		RequestID: newRequestID(),
		Request: SDKControlRequestType{
			Subtype: "interrupt",
		},
	}
}

// ControlResponseMessage creates a control response message
func ControlResponseMessage(requestID string, response json.RawMessage) ControlResponse {
	return ControlResponse{
		Type: "control_response",
		Response: ControlResponseType{
			Subtype:   "success",
			RequestID: requestID,
			Response:  &response,
		},
	}
}

// ControlErrorResponse creates an error response message
func ControlErrorResponse(requestID string, err string) ControlResponse {
	return ControlResponse{
		Type: "control_response",
		Response: ControlResponseType{
			Subtype:   "error",
			RequestID: requestID,
			Error:     &err,
		},
	}
}

func newRequestID() string {
	return uuid.New().String()
}
