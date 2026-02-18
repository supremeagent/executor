package codex

import "encoding/json"

// RequestID represents a JSON-RPC request ID
type RequestID struct {
	Number *int64  `json:"number,omitempty"`
	String *string `json:"string,omitempty"`
}

// JSONRPCMessage represents a JSON-RPC message
type JSONRPCMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *RequestID     `json:"id,omitempty"`
	Method  string         `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC error
type JSONRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
	ID      *RequestID     `json:"id,omitempty"`
}

// ClientInfo represents client information
type ClientInfo struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version"`
}

// InitializeParams represents initialize parameters
type InitializeParams struct {
	ClientInfo   ClientInfo `json:"client_info"`
	Capabilities interface{} `json:"capabilities,omitempty"`
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
	Model               string                 `json:"model,omitempty"`
	Sandbox             string                 `json:"sandbox,omitempty"`
	AskForApproval     string                 `json:"ask_for_approval,omitempty"`
	ModelReasoningEffort string                `json:"model_reasoning_effort,omitempty"`
	WorkingDirectory   string                 `json:"working_directory,omitempty"`
}

// NewConversationResult represents new conversation result
type NewConversationResult struct {
	ConversationID string `json:"conversation_id"`
}

// ResumeConversationParams represents resume conversation parameters
type ResumeConversationParams struct {
	Path          string `json:"path,omitempty"`
	ConversationID string `json:"conversation_id,omitempty"`
	History       string `json:"history,omitempty"`
	Overrides     *NewConversationParams `json:"overrides,omitempty"`
}

// ResumeConversationResult represents resume conversation result
type ResumeConversationResult struct {
	ConversationID string `json:"conversation_id"`
}

// SendUserMessageParams represents send user message parameters
type SendUserMessageParams struct {
	ConversationID string     `json:"conversation_id"`
	Items          []InputItem `json:"items"`
}

// InputItem represents an input item
type InputItem struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// SendUserMessageResult represents send user message result
type SendUserMessageResult struct {
	ResultItem *struct{} `json:"resultItem,omitempty"`
}

// AddConversationListenerParams represents add conversation listener parameters
type AddConversationListenerParams struct {
	ConversationID       string `json:"conversation_id"`
	ExperimentalRawEvents bool  `json:"experimental_raw_events"`
}

// AddConversationListenerResult represents add conversation listener result
type AddConversationListenerResult struct {
	SubscriptionID string `json:"subscription_id"`
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
