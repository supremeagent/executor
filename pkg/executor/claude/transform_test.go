package claude

import (
	"encoding/json"
	"testing"

	"github.com/supremeagent/executor/pkg/executor"
)

func TestEventTransformer_ControlAndToolEvents(t *testing.T) {
	evt := EventTransformer(executor.TransformInput{
		SessionID: "s1",
		Executor:  "claude_code",
		Log: executor.Log{
			Type: "control_request",
			Content: map[string]any{
				"request_id": "req-1",
				"request": map[string]any{
					"tool_name": "Bash",
				},
			},
		},
	})

	if evt.Type != "approval" {
		t.Fatalf("expected approval event, got %s", evt.Type)
	}
	content := evt.Content.(executor.UnifiedContent)
	if content.RequestID != "req-1" || content.ToolName != "Bash" {
		t.Fatalf("unexpected approval content: %+v", content)
	}

	toolEvt := EventTransformer(executor.TransformInput{
		SessionID: "s1",
		Executor:  "claude_code",
		Log: executor.Log{
			Type:    "stdout",
			Content: `{"type":"tool_use","tool_name":"Read"}`,
		},
	})
	if toolEvt.Type != "tool" {
		t.Fatalf("expected tool event, got %s", toolEvt.Type)
	}

	doneEvt := EventTransformer(executor.TransformInput{
		SessionID: "s1",
		Executor:  "claude_code",
		Log: executor.Log{
			Type: "done",
		},
	})
	if doneEvt.Type != "done" {
		t.Fatalf("expected done event, got %s", doneEvt.Type)
	}

	errEvt := EventTransformer(executor.TransformInput{
		SessionID: "s1",
		Executor:  "claude_code",
		Log: executor.Log{
			Type:    "error",
			Content: "bad",
		},
	})
	if errEvt.Type != "error" {
		t.Fatalf("expected error event, got %s", errEvt.Type)
	}

	cmdEvt := EventTransformer(executor.TransformInput{
		SessionID: "s1",
		Executor:  "claude_code",
		Log: executor.Log{
			Type:    "command",
			Content: "npx ...",
		},
	})
	if cmdEvt.Type != "progress" {
		t.Fatalf("expected progress for command, got %s", cmdEvt.Type)
	}

	resultEvt := EventTransformer(executor.TransformInput{
		SessionID: "s1",
		Executor:  "claude_code",
		Log: executor.Log{
			Type: "result",
		},
	})
	if resultEvt.Type != "message" {
		t.Fatalf("expected message for result, got %s", resultEvt.Type)
	}

	fallbackNameEvt := EventTransformer(executor.TransformInput{
		SessionID: "s1",
		Executor:  "claude_code",
		Log: executor.Log{
			Type:    "stdout",
			Content: `{"type":"tool_result","name":"Edit"}`,
		},
	})
	content, ok := fallbackNameEvt.Content.(executor.UnifiedContent)
	if !ok || content.ToolName != "Edit" {
		t.Fatalf("expected fallback tool name from name field, got %#v", fallbackNameEvt.Content)
	}

	if _, ok := parseJSONObject(json.RawMessage(`{"x":1}`)); !ok {
		t.Fatalf("expected raw json parsing")
	}
	if _, ok := parseJSONObject("prefix {\"x\":1}"); !ok {
		t.Fatalf("expected string parsing")
	}
	if _, ok := parseJSONObject(42); ok {
		t.Fatalf("expected parse failure for unsupported type")
	}
}
