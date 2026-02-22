package acp

import (
	"encoding/json"
	"testing"

	"github.com/supremeagent/executor/pkg/executor"
)

func makeInput(logType string, content any) executor.TransformInput {
	return executor.TransformInput{
		SessionID: "test-session",
		Executor:  "gemini",
		Log:       executor.Log{Type: logType, Content: content},
	}
}

func TestEventTransformer_Done(t *testing.T) {
	evt := EventTransformer(makeInput("done", "finished"))
	if evt.Type != "done" {
		t.Errorf("expected type 'done', got %q", evt.Type)
	}
	uc, ok := evt.Content.(executor.UnifiedContent)
	if !ok {
		t.Fatalf("expected UnifiedContent, got %T", evt.Content)
	}
	if uc.Category != "done" {
		t.Errorf("expected category 'done', got %q", uc.Category)
	}
}

func TestEventTransformer_AcpDone(t *testing.T) {
	evt := EventTransformer(makeInput("acp_done", json.RawMessage(`{"Done":"end_turn"}`)))
	if evt.Type != "done" {
		t.Errorf("expected type 'done', got %q", evt.Type)
	}
}

func TestEventTransformer_Error(t *testing.T) {
	evt := EventTransformer(makeInput("error", "something broke"))
	if evt.Type != "error" {
		t.Errorf("expected type 'error', got %q", evt.Type)
	}
	uc, ok := evt.Content.(executor.UnifiedContent)
	if !ok {
		t.Fatalf("expected UnifiedContent, got %T", evt.Content)
	}
	if uc.Category != "error" {
		t.Errorf("expected category 'error', got %q", uc.Category)
	}
}

func TestEventTransformer_Stderr(t *testing.T) {
	evt := EventTransformer(makeInput("stderr", "some stderr line"))
	if evt.Type != "error" {
		t.Errorf("expected type 'error', got %q", evt.Type)
	}
}

func TestEventTransformer_Command(t *testing.T) {
	evt := EventTransformer(makeInput("command", "npx -y @google/gemini-cli"))
	if evt.Type != "progress" {
		t.Errorf("expected type 'progress', got %q", evt.Type)
	}
	uc, _ := evt.Content.(executor.UnifiedContent)
	if uc.Category != "lifecycle" {
		t.Errorf("expected category 'lifecycle', got %q", uc.Category)
	}
}

func TestEventTransformer_SessionStart(t *testing.T) {
	evt := EventTransformer(makeInput("session_start", "sess-123"))
	if evt.Type != "progress" {
		t.Errorf("expected type 'progress', got %q", evt.Type)
	}
	uc, _ := evt.Content.(executor.UnifiedContent)
	if uc.Category != "lifecycle" {
		t.Errorf("expected category 'lifecycle', got %q", uc.Category)
	}
}

func TestEventTransformer_ControlRequest(t *testing.T) {
	content := map[string]any{
		"request_id": "req-1",
		"tool_call": map[string]any{
			"title": "bash",
			"kind":  "Execute",
		},
	}
	evt := EventTransformer(makeInput("control_request", content))
	if evt.Type != "approval" {
		t.Errorf("expected type 'approval', got %q", evt.Type)
	}
	uc, _ := evt.Content.(executor.UnifiedContent)
	if uc.Category != "approval" {
		t.Errorf("expected category 'approval', got %q", uc.Category)
	}
	if uc.RequestID != "req-1" {
		t.Errorf("expected RequestID 'req-1', got %q", uc.RequestID)
	}
	if uc.ToolName != "bash" {
		t.Errorf("expected ToolName 'bash', got %q", uc.ToolName)
	}
}

func TestEventTransformer_ToolCall_Read(t *testing.T) {
	payload := json.RawMessage(`{"ToolCall":{"tool_call_id":"read-1","kind":"Read","title":"main.go","status":"pending"}}`)
	evt := EventTransformer(makeInput(string(EventTypeToolCall), payload))
	if evt.Type != "tool" {
		t.Errorf("expected type 'tool', got %q", evt.Type)
	}
	uc, _ := evt.Content.(executor.UnifiedContent)
	if uc.Action != "reading" {
		t.Errorf("expected action 'reading', got %q", uc.Action)
	}
}

func TestEventTransformer_ToolCall_Edit(t *testing.T) {
	payload := json.RawMessage(`{"ToolCall":{"tool_call_id":"edit-1","kind":"Edit","title":"main.go","status":"in_progress"}}`)
	evt := EventTransformer(makeInput(string(EventTypeToolCall), payload))
	uc, _ := evt.Content.(executor.UnifiedContent)
	if uc.Action != "editing" {
		t.Errorf("expected action 'editing', got %q", uc.Action)
	}
}

func TestEventTransformer_ToolCall_Execute(t *testing.T) {
	payload := json.RawMessage(`{"ToolCall":{"tool_call_id":"exec-1","kind":"Execute","title":"ls -la","status":"in_progress"}}`)
	evt := EventTransformer(makeInput(string(EventTypeToolCall), payload))
	uc, _ := evt.Content.(executor.UnifiedContent)
	if uc.Action != "tool_running" {
		t.Errorf("expected action 'tool_running', got %q", uc.Action)
	}
}

func TestEventTransformer_ToolCall_Search(t *testing.T) {
	payload := json.RawMessage(`{"ToolCall":{"tool_call_id":"search-1","kind":"Search","title":"foo bar","status":"in_progress"}}`)
	evt := EventTransformer(makeInput(string(EventTypeToolCall), payload))
	uc, _ := evt.Content.(executor.UnifiedContent)
	if uc.Action != "searching" {
		t.Errorf("expected action 'searching', got %q", uc.Action)
	}
}

func TestEventTransformer_ToolUpdate_Completed(t *testing.T) {
	payload := json.RawMessage(`{"ToolUpdate":{"tool_call_id":"read-1","kind":"Read","title":"main.go","status":"completed"}}`)
	evt := EventTransformer(makeInput(string(EventTypeToolUpdate), payload))
	uc, _ := evt.Content.(executor.UnifiedContent)
	if uc.Phase != "completed" {
		t.Errorf("expected phase 'completed', got %q", uc.Phase)
	}
}

func TestEventTransformer_Message(t *testing.T) {
	payload := json.RawMessage(`{"Message":{"Text":{"text":"Hello world"}}}`)
	evt := EventTransformer(makeInput(string(EventTypeMessage), payload))
	if evt.Type != "message" {
		t.Errorf("expected type 'message', got %q", evt.Type)
	}
	uc, _ := evt.Content.(executor.UnifiedContent)
	if uc.Action != "responding" {
		t.Errorf("expected action 'responding', got %q", uc.Action)
	}
}

func TestEventTransformer_Thought(t *testing.T) {
	evt := EventTransformer(makeInput(string(EventTypeThought), json.RawMessage(`{"Thought":{"Text":{"text":"thinking..."}}}`)))
	if evt.Type != "progress" {
		t.Errorf("expected type 'progress', got %q", evt.Type)
	}
	uc, _ := evt.Content.(executor.UnifiedContent)
	if uc.Action != "thinking" {
		t.Errorf("expected action 'thinking', got %q", uc.Action)
	}
}

func TestEventTransformer_Plan(t *testing.T) {
	evt := EventTransformer(makeInput(string(EventTypePlan), json.RawMessage(`{"Plan":{"entries":[]}}`)))
	if evt.Type != "progress" {
		t.Errorf("expected type 'progress', got %q", evt.Type)
	}
}

func TestEventTransformer_Unknown(t *testing.T) {
	evt := EventTransformer(makeInput("some_unknown_type", "data"))
	if evt.Type != "progress" {
		t.Errorf("expected type 'progress' for unknown, got %q", evt.Type)
	}
}

func TestMapACPToolKind_Delete(t *testing.T) {
	content := &executor.UnifiedContent{}
	mapACPToolKind(content, ToolKindDelete)
	if content.Action != "editing" {
		t.Errorf("expected action 'editing' for Delete kind, got %q", content.Action)
	}
}

func TestMapACPToolKind_Think(t *testing.T) {
	content := &executor.UnifiedContent{}
	mapACPToolKind(content, ToolKindThink)
	if content.Action != "thinking" {
		t.Errorf("expected action 'thinking' for Think kind, got %q", content.Action)
	}
	if content.Category != "progress" {
		t.Errorf("expected category 'progress' for Think kind, got %q", content.Category)
	}
}

func TestMapACPToolKind_Fetch(t *testing.T) {
	content := &executor.UnifiedContent{}
	mapACPToolKind(content, ToolKindFetch)
	if content.Action != "searching" {
		t.Errorf("expected action 'searching' for Fetch kind, got %q", content.Action)
	}
}

func TestMapACPToolKind_Other(t *testing.T) {
	content := &executor.UnifiedContent{ToolName: "my_tool"}
	mapACPToolKind(content, ToolKindOther)
	if content.Action != "tool_running" {
		t.Errorf("expected action 'tool_running' for Other kind, got %q", content.Action)
	}
}

func TestExtractTextFromACPContent_Nested(t *testing.T) {
	obj := map[string]any{
		"Text": map[string]any{"text": "hello world"},
	}
	text := extractTextFromACPContent(obj)
	if text != "hello world" {
		t.Errorf("expected 'hello world', got %q", text)
	}
}

func TestExtractTextFromACPContent_Direct(t *testing.T) {
	obj := map[string]any{"text": "direct text"}
	text := extractTextFromACPContent(obj)
	if text != "direct text" {
		t.Errorf("expected 'direct text', got %q", text)
	}
}

func TestExtractTextFromACPContent_Empty(t *testing.T) {
	obj := map[string]any{"other": "data"}
	text := extractTextFromACPContent(obj)
	if text != "" {
		t.Errorf("expected empty string, got %q", text)
	}
}

func TestDefaultSummary(t *testing.T) {
	cases := []struct {
		action   string
		expected string
	}{
		{"thinking", "Thinking deeply"},
		{"reading", "Reading file"},
		{"searching", "Searching"},
		{"editing", "Modifying code"},
		{"tool_running", "Calling tool"},
		{"responding", "Generating reply"},
		{"completed", "Execution completed"},
		{"failed", "Execution failed"},
		{"starting", "Starting"},
		{"unknown_action", "Processing"},
	}
	for _, tc := range cases {
		t.Run(tc.action, func(t *testing.T) {
			c := executor.UnifiedContent{Action: tc.action}
			got := defaultSummary(c)
			if got != tc.expected {
				t.Errorf("defaultSummary(%q) = %q, want %q", tc.action, got, tc.expected)
			}
		})
	}
}
