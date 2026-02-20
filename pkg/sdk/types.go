package sdk

type ExecutorType string

// Built-in executor identifiers.
const (
	ExecutorClaudeCode ExecutorType = "claude_code"
	ExecutorCodex      ExecutorType = "codex"
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

// Event represents one streamed task event.
type Event struct {
	Type    string `json:"type"`
	Content any    `json:"content"`
}

// SubscribeOptions configures event subscription behavior.
type SubscribeOptions struct {
	ReturnAll    bool
	IncludeDebug bool
}
