package droid

import (
	"testing"

	"github.com/supremeagent/executor/pkg/executor"
)

func makeInput(logType string, content any) executor.TransformInput {
	return executor.TransformInput{
		SessionID: "test-session",
		Executor:  "droid",
		Log:       executor.Log{Type: logType, Content: content},
	}
}

func TestEventTransformer_Done(t *testing.T) {
	evt := EventTransformer(makeInput("done", "finished"))
	if evt.Type != "done" {
		t.Errorf("expected type 'done', got %q", evt.Type)
	}
	uc, _ := evt.Content.(executor.UnifiedContent)
	if uc.Category != "done" {
		t.Errorf("expected category 'done', got %q", uc.Category)
	}
}

func TestEventTransformer_Error(t *testing.T) {
	evt := EventTransformer(makeInput("error", "something failed"))
	if evt.Type != "error" {
		t.Errorf("expected type 'error', got %q", evt.Type)
	}
	uc, _ := evt.Content.(executor.UnifiedContent)
	if uc.Category != "error" {
		t.Errorf("expected category 'error', got %q", uc.Category)
	}
}

func TestEventTransformer_Stderr(t *testing.T) {
	evt := EventTransformer(makeInput("stderr", "some stderr"))
	if evt.Type != "error" {
		t.Errorf("expected type 'error', got %q", evt.Type)
	}
}

func TestEventTransformer_Command(t *testing.T) {
	evt := EventTransformer(makeInput("command", "droid exec --output-format stream-json"))
	if evt.Type != "progress" {
		t.Errorf("expected type 'progress', got %q", evt.Type)
	}
	uc, _ := evt.Content.(executor.UnifiedContent)
	if uc.Category != "lifecycle" {
		t.Errorf("expected category 'lifecycle', got %q", uc.Category)
	}
}

func TestEventTransformer_DroidSystem(t *testing.T) {
	dEvt := DroidEvent{Type: EventTypeSystem, SessionID: "s1", Model: "gpt-4"}
	evt := EventTransformer(makeInput("droid_system", dEvt))
	if evt.Type != "progress" {
		t.Errorf("expected type 'progress', got %q", evt.Type)
	}
	uc, _ := evt.Content.(executor.UnifiedContent)
	if uc.Category != "lifecycle" {
		t.Errorf("expected category 'lifecycle', got %q", uc.Category)
	}
}

func TestEventTransformer_DroidMessage_Assistant(t *testing.T) {
	dEvt := DroidEvent{Type: EventTypeMessage, Role: "assistant", Text: "Hello!"}
	evt := EventTransformer(makeInput("droid_message", dEvt))
	if evt.Type != "message" {
		t.Errorf("expected type 'message', got %q", evt.Type)
	}
	uc, _ := evt.Content.(executor.UnifiedContent)
	if uc.Text != "Hello!" {
		t.Errorf("expected text 'Hello!', got %q", uc.Text)
	}
}

func TestEventTransformer_DroidMessage_User(t *testing.T) {
	dEvt := DroidEvent{Type: EventTypeMessage, Role: "user", Text: "do something"}
	evt := EventTransformer(makeInput("droid_message", dEvt))
	if evt.Type != "progress" {
		t.Errorf("expected type 'progress' for user messages, got %q", evt.Type)
	}
}

func TestEventTransformer_DroidToolCall_Read(t *testing.T) {
	dEvt := DroidEvent{Type: EventTypeToolCall, ToolName: "Read"}
	evt := EventTransformer(makeInput("droid_tool_call", dEvt))
	if evt.Type != "tool" {
		t.Errorf("expected type 'tool', got %q", evt.Type)
	}
	uc, _ := evt.Content.(executor.UnifiedContent)
	if uc.Action != "reading" {
		t.Errorf("expected action 'reading', got %q", uc.Action)
	}
}

func TestEventTransformer_DroidToolCall_Execute(t *testing.T) {
	dEvt := DroidEvent{Type: EventTypeToolCall, ToolName: "Execute"}
	evt := EventTransformer(makeInput("droid_tool_call", dEvt))
	uc, _ := evt.Content.(executor.UnifiedContent)
	if uc.Action != "tool_running" {
		t.Errorf("expected action 'tool_running' for Execute, got %q", uc.Action)
	}
}

func TestEventTransformer_DroidToolCall_Edit(t *testing.T) {
	dEvt := DroidEvent{Type: EventTypeToolCall, ToolName: "Edit"}
	evt := EventTransformer(makeInput("droid_tool_call", dEvt))
	uc, _ := evt.Content.(executor.UnifiedContent)
	if uc.Action != "editing" {
		t.Errorf("expected action 'editing', got %q", uc.Action)
	}
}

func TestEventTransformer_DroidToolCall_Grep(t *testing.T) {
	dEvt := DroidEvent{Type: EventTypeToolCall, ToolName: "Grep"}
	evt := EventTransformer(makeInput("droid_tool_call", dEvt))
	uc, _ := evt.Content.(executor.UnifiedContent)
	if uc.Action != "searching" {
		t.Errorf("expected action 'searching' for Grep, got %q", uc.Action)
	}
}

func TestEventTransformer_DroidToolCall_TodoWrite(t *testing.T) {
	dEvt := DroidEvent{Type: EventTypeToolCall, ToolName: "TodoWrite"}
	evt := EventTransformer(makeInput("droid_tool_call", dEvt))
	uc, _ := evt.Content.(executor.UnifiedContent)
	if uc.Category != "progress" {
		t.Errorf("expected category 'progress' for TodoWrite, got %q", uc.Category)
	}
}

func TestEventTransformer_DroidToolResult_Success(t *testing.T) {
	dEvt := DroidEvent{Type: EventTypeToolResult, ToolName: "Read", IsError: false}
	evt := EventTransformer(makeInput("droid_tool_result", dEvt))
	if evt.Type != "tool" {
		t.Errorf("expected type 'tool', got %q", evt.Type)
	}
	uc, _ := evt.Content.(executor.UnifiedContent)
	if uc.Phase != "completed" {
		t.Errorf("expected phase 'completed' on success, got %q", uc.Phase)
	}
	if uc.Status != "success" {
		t.Errorf("expected status 'success', got %q", uc.Status)
	}
}

func TestEventTransformer_DroidToolResult_Error(t *testing.T) {
	dEvt := DroidEvent{Type: EventTypeToolResult, ToolName: "Execute", IsError: true}
	evt := EventTransformer(makeInput("droid_tool_result", dEvt))
	uc, _ := evt.Content.(executor.UnifiedContent)
	if uc.Phase != "failed" {
		t.Errorf("expected phase 'failed' on error, got %q", uc.Phase)
	}
	if uc.Status != "failed" {
		t.Errorf("expected status 'failed', got %q", uc.Status)
	}
}

func TestEventTransformer_DroidCompletion(t *testing.T) {
	dEvt := DroidEvent{Type: EventTypeCompletion, FinalText: "all done"}
	evt := EventTransformer(makeInput("droid_completion", dEvt))
	if evt.Type != "done" {
		t.Errorf("expected type 'done', got %q", evt.Type)
	}
	uc, _ := evt.Content.(executor.UnifiedContent)
	if uc.Text != "all done" {
		t.Errorf("expected text 'all done', got %q", uc.Text)
	}
}

func TestEventTransformer_Unknown(t *testing.T) {
	evt := EventTransformer(makeInput("some_unknown_type", "data"))
	if evt.Type != "progress" {
		t.Errorf("expected type 'progress' for unknown, got %q", evt.Type)
	}
}

func TestApplyDroidToolMapping_LS(t *testing.T) {
	c := &executor.UnifiedContent{}
	applyDroidToolMapping(c, "LS")
	if c.Action != "reading" {
		t.Errorf("expected action 'reading' for LS, got %q", c.Action)
	}
}

func TestApplyDroidToolMapping_MultiEdit(t *testing.T) {
	c := &executor.UnifiedContent{}
	applyDroidToolMapping(c, "MultiEdit")
	if c.Action != "editing" {
		t.Errorf("expected action 'editing' for MultiEdit, got %q", c.Action)
	}
}

func TestApplyDroidToolMapping_FetchUrl(t *testing.T) {
	c := &executor.UnifiedContent{}
	applyDroidToolMapping(c, "FetchUrl")
	if c.Action != "searching" {
		t.Errorf("expected action 'searching' for FetchUrl, got %q", c.Action)
	}
}

func TestApplyDroidToolMapping_ApplyPatch(t *testing.T) {
	c := &executor.UnifiedContent{}
	applyDroidToolMapping(c, "ApplyPatch")
	if c.Action != "editing" {
		t.Errorf("expected action 'editing' for ApplyPatch, got %q", c.Action)
	}
}

func TestApplyDroidToolMapping_WebSearch(t *testing.T) {
	c := &executor.UnifiedContent{}
	applyDroidToolMapping(c, "WebSearch")
	if c.Action != "searching" {
		t.Errorf("expected action 'searching' for WebSearch, got %q", c.Action)
	}
}

func TestApplyDroidToolMapping_UnknownTool(t *testing.T) {
	c := &executor.UnifiedContent{ToolName: "slack_post_message"}
	applyDroidToolMapping(c, "slack_post_message")
	if c.Action != "tool_running" {
		t.Errorf("expected action 'tool_running' for unknown, got %q", c.Action)
	}
}

func TestParseDroidEvent_FromStruct(t *testing.T) {
	dEvt := DroidEvent{Type: EventTypeMessage, Role: "assistant", Text: "hi"}
	parsed, ok := parseDroidEvent(dEvt)
	if !ok {
		t.Fatal("expected parseDroidEvent to succeed for DroidEvent struct")
	}
	if parsed.Text != "hi" {
		t.Errorf("expected text 'hi', got %q", parsed.Text)
	}
}

func TestParseDroidEvent_FromMapStringAny(t *testing.T) {
	m := map[string]any{
		"type": "message",
		"role": "assistant",
		"text": "hello",
	}
	parsed, ok := parseDroidEvent(m)
	if !ok {
		t.Fatal("expected parseDroidEvent to succeed for map")
	}
	if parsed.Text != "hello" {
		t.Errorf("expected text 'hello', got %q", parsed.Text)
	}
}

func TestParseDroidEvent_Unsupported(t *testing.T) {
	_, ok := parseDroidEvent(42)
	if ok {
		t.Error("expected parseDroidEvent to fail for unsupported type")
	}
}
