package codex

import (
	"encoding/json"
	"fmt"
)

// RequestID represents a JSON-RPC request ID
type RequestID struct {
	Number *int64  `json:"number,omitempty"`
	String *string `json:"string,omitempty"`
}

func (r RequestID) MarshalJSON() ([]byte, error) {
	if r.Number != nil {
		return json.Marshal(*r.Number)
	}
	if r.String != nil {
		return json.Marshal(*r.String)
	}
	return []byte("null"), nil
}

func (r *RequestID) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		r.Number = nil
		r.String = nil
		return nil
	}

	var n int64
	if err := json.Unmarshal(data, &n); err == nil {
		r.Number = &n
		r.String = nil
		return nil
	}

	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		r.String = &s
		r.Number = nil
		return nil
	}

	return fmt.Errorf("invalid request id format: %s", string(data))
}

// JSONRPCMessage represents a JSON-RPC message
type JSONRPCMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *RequestID      `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC error
type JSONRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
	ID      *RequestID      `json:"id,omitempty"`
}

// ClientInfo represents client information
type ClientInfo struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version"`
}

// InitializeParams represents initialize parameters
type InitializeParams struct {
	ClientInfo      ClientInfo `json:"clientInfo"`
	Capabilities    any        `json:"capabilities,omitempty"`
	ProtocolVersion string     `json:"protocolVersion,omitempty"`
}

// InitializeResult represents initialize result
type InitializeResult struct {
	ProtocolVersion string `json:"protocolVersion"`
	Capabilities    struct {
		Tools *struct{} `json:"tools"`
	} `json:"capabilities"`
	ServerInfo struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"serverInfo"`
}

// NewConversationParams represents new conversation parameters
type NewConversationParams struct {
	Model                string `json:"model,omitempty"`
	Sandbox              string `json:"sandbox,omitempty"`
	AskForApproval       string `json:"askForApproval,omitempty"`
	ModelReasoningEffort string `json:"modelReasoningEffort,omitempty"`
	WorkingDirectory     string `json:"workingDirectory,omitempty"`
}

// NewConversationResult represents new conversation result
type NewConversationResult struct {
	ConversationID string `json:"conversationId"`
	RolloutPath    string `json:"rolloutPath,omitempty"`
}

// ResumeConversationParams represents resume conversation parameters
type ResumeConversationParams struct {
	Path           string                 `json:"path,omitempty"`
	ConversationID string                 `json:"conversationId,omitempty"`
	History        string                 `json:"history,omitempty"`
	Overrides      *NewConversationParams `json:"overrides,omitempty"`
}

// ResumeConversationResult represents resume conversation result
type ResumeConversationResult struct {
	ConversationID string `json:"conversationId"`
	RolloutPath    string `json:"rolloutPath,omitempty"`
}

// SendUserMessageParams represents send user message parameters
type SendUserMessageParams struct {
	ConversationID string      `json:"conversationId"`
	Items          []InputItem `json:"items"`
}

// InputItem represents an input item
type InputItem struct {
	Type string        `json:"type"`
	Data InputItemData `json:"data"`
}

type InputItemData struct {
	Text string `json:"text,omitempty"`
}

// SendUserMessageResult represents send user message result
type SendUserMessageResult struct {
	ResultItem *struct{} `json:"resultItem,omitempty"`
}

// AddConversationListenerParams represents add conversation listener parameters
type AddConversationListenerParams struct {
	ConversationID        string `json:"conversationId"`
	ExperimentalRawEvents bool   `json:"experimentalRawEvents"`
}

// AddConversationListenerResult represents add conversation listener result
type AddConversationListenerResult struct {
	SubscriptionID string `json:"subscriptionId"`
}

// ServerNotification represents a server notification
type ServerNotification struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// EventNotification represents an event notification
type EventNotification struct {
	Event string `json:"event"`
	Type  string `json:"type,omitempty"`
}

// IDCounter is used to generate unique request IDs
var idCounter int64 = 1

func nextID() RequestID {
	idCounter++
	return RequestID{Number: &idCounter}
}
