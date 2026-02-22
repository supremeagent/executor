package codex

import (
	"encoding/json"
	"testing"

	"github.com/supremeagent/executor/pkg/executor"
)

func TestEventTransformer_ControlAndLifecycle(t *testing.T) {
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
	if content.Action != "approval_required" || content.ToolName != "edit" || content.RequestID != "5" {
		t.Fatalf("unexpected content: %+v", content)
	}

	progress := EventTransformer(executor.TransformInput{
		SessionID: "s1",
		Executor:  "codex",
		Log: executor.Log{
			Type: "codex/event/task_started",
			Content: map[string]any{
				"msg": map[string]any{"type": "task_started"},
			},
		},
	})
	if progress.Type != "progress" {
		t.Fatalf("expected progress, got %s", progress.Type)
	}
	p := progress.Content.(executor.UnifiedContent)
	if p.Action != "thinking" || p.Summary == "" {
		t.Fatalf("unexpected progress mapping: %+v", p)
	}

	initEvt := EventTransformer(executor.TransformInput{
		SessionID: "s1",
		Executor:  "codex",
		Log:       executor.Log{Type: "init", Content: "npx ..."},
	})
	initContent := initEvt.Content.(executor.UnifiedContent)
	if initEvt.Type != "progress" || initContent.Action != "starting" {
		t.Fatalf("unexpected init mapping: type=%s content=%+v", initEvt.Type, initContent)
	}
}

func TestEventTransformer_ToolSearchReadAndDone(t *testing.T) {
	readEvt := EventTransformer(executor.TransformInput{
		SessionID: "s1",
		Executor:  "codex",
		Log: executor.Log{
			Type: "codex/event/read_file",
			Content: map[string]any{
				"msg": map[string]any{"type": "read_file"},
			},
		},
	})
	read := readEvt.Content.(executor.UnifiedContent)
	if read.Action != "reading" {
		t.Fatalf("expected reading action, got %+v", read)
	}

	searchEvt := EventTransformer(executor.TransformInput{
		SessionID: "s1",
		Executor:  "codex",
		Log: executor.Log{
			Type: "codex/event/search_start",
			Content: map[string]any{
				"msg": map[string]any{"type": "search_start"},
			},
		},
	})
	search := searchEvt.Content.(executor.UnifiedContent)
	if search.Action != "searching" {
		t.Fatalf("expected searching action, got %+v", search)
	}

	toolEvt := EventTransformer(executor.TransformInput{
		SessionID: "s1",
		Executor:  "codex",
		Log: executor.Log{
			Type: "codex/event/exec_command_output_delta",
			Content: map[string]any{
				"msg": map[string]any{
					"type": "exec_command_output_delta",
					"call": map[string]any{
						"name":      "exec_command",
						"arguments": map[string]any{"cmd": "rg -n transform"},
					},
				},
			},
		},
	})
	tool := toolEvt.Content.(executor.UnifiedContent)
	if toolEvt.Type != "progress" || tool.Category != "tool" || tool.Action != "tool_running" || tool.ToolName == "" {
		t.Fatalf("unexpected tool mapping: type=%s content=%+v", toolEvt.Type, tool)
	}

	done := EventTransformer(executor.TransformInput{
		SessionID: "s1",
		Executor:  "codex",
		Log:       executor.Log{Type: "done"},
	})
	d := done.Content.(executor.UnifiedContent)
	if done.Type != "done" || d.Action != "completed" {
		t.Fatalf("unexpected done mapping: type=%s content=%+v", done.Type, d)
	}

	errorEvt := EventTransformer(executor.TransformInput{
		SessionID: "s1",
		Executor:  "codex",
		Log:       executor.Log{Type: "error", Content: "boom"},
	})
	errContent := errorEvt.Content.(executor.UnifiedContent)
	if errorEvt.Type != "error" || errContent.Action != "failed" {
		t.Fatalf("unexpected error mapping: type=%s content=%+v", errorEvt.Type, errContent)
	}
}

func TestHelpers(t *testing.T) {
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
	if got := nestedString(map[string]any{"a": map[string]any{"b": "c"}}, "a", "b"); got != "c" {
		t.Fatalf("unexpected nested string: %s", got)
	}
	if got := fallback("", "d"); got != "d" {
		t.Fatalf("unexpected fallback value: %s", got)
	}
	if s := defaultSummary(executor.UnifiedContent{Action: "searching"}); s == "" {
		t.Fatal("defaultSummary should not be empty")
	}
}
