package codex

import (
	"encoding/json"
	"testing"

	"github.com/supremeagent/executor/pkg/executor"
)

func TestEventTransformer_ProgressAndApproval(t *testing.T) {
	evt := EventTransformer(executor.TransformInput{
		SessionID: "s1",
		Executor:  "codex",
		Log: executor.Log{
			Type: "control_request",
			Content: map[string]any{
				"request_id": "5",
				"method":     "codex/apply_patch_approval",
			},
		},
	})
	if evt.Type != "approval" {
		t.Fatalf("expected approval, got %s", evt.Type)
	}
	content := evt.Content.(executor.UnifiedContent)
	if content.ToolName != "edit" || content.RequestID != "5" {
		t.Fatalf("unexpected content: %+v", content)
	}

	progress := EventTransformer(executor.TransformInput{
		SessionID: "s1",
		Executor:  "codex",
		Log: executor.Log{
			Type:    "codex/event/task_started",
			Content: `{"msg":{"type":"task_started"}}`,
		},
	})
	if progress.Type != "progress" {
		t.Fatalf("expected progress, got %s", progress.Type)
	}

	done := EventTransformer(executor.TransformInput{
		SessionID: "s1",
		Executor:  "codex",
		Log: executor.Log{
			Type: "done",
		},
	})
	if done.Type != "done" {
		t.Fatalf("expected done, got %s", done.Type)
	}

	errorEvt := EventTransformer(executor.TransformInput{
		SessionID: "s1",
		Executor:  "codex",
		Log: executor.Log{
			Type:    "error",
			Content: "boom",
		},
	})
	if errorEvt.Type != "error" {
		t.Fatalf("expected error, got %s", errorEvt.Type)
	}

	toolEvt := EventTransformer(executor.TransformInput{
		SessionID: "s1",
		Executor:  "codex",
		Log: executor.Log{
			Type:    "codex/event/exec_command_output_delta",
			Content: `{"msg":{"type":"exec_command_output_delta"}}`,
		},
	})
	if toolEvt.Type != "tool" {
		t.Fatalf("expected tool event, got %s", toolEvt.Type)
	}

	patchEvt := EventTransformer(executor.TransformInput{
		SessionID: "s1",
		Executor:  "codex",
		Log: executor.Log{
			Type: "codex/event/patch_apply_begin",
		},
	})
	if patchEvt.Type != "tool" {
		t.Fatalf("expected patch tool event, got %s", patchEvt.Type)
	}

	if _, ok := parseJSONObject(`{"k":"v"}`); !ok {
		t.Fatalf("expected parse string json")
	}
	if _, ok := parseJSONObject(json.RawMessage(`{"k":"v"}`)); !ok {
		t.Fatalf("expected parse raw json")
	}
	if _, ok := parseJSONObject(123); ok {
		t.Fatalf("expected parse failure for unsupported type")
	}

	if got := detectToolFromControl(map[string]any{"method": "x", "params": map[string]any{"tool": "custom"}}); got != "custom" {
		t.Fatalf("unexpected tool detection: %s", got)
	}
}
