package api

// ExecuteRequest represents an execute API request
type ExecuteRequest struct {
	Prompt      string `json:"prompt"`
	Executor    string `json:"executor"`
	WorkingDir  string `json:"working_dir"`
	Model       string `json:"model,omitempty"`
	Plan        bool   `json:"plan,omitempty"`
	Sandbox     string `json:"sandbox,omitempty"`
	AskForApproval string `json:"ask_for_approval,omitempty"`
}

// ExecuteResponse represents an execute API response
type ExecuteResponse struct {
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
}

// ContinueRequest represents a continue API request
type ContinueRequest struct {
	Message string `json:"message"`
}

// LogEvent represents an SSE log event
type LogEvent struct {
	Type    string      `json:"type"`
	Content interface{} `json:"content"`
}
