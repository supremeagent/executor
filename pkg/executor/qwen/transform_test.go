package qwen

import (
	"encoding/json"
	"testing"

	"github.com/supremeagent/executor/pkg/executor"
)

func TestEventTransformer_ControlToolAndDone(t *testing.T) {
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
	if content.RequestID != "req-1" || content.ToolName != "Bash" || content.Action != "approval_required" {
		t.Fatalf("unexpected approval content: %+v", content)
	}

	toolEvt := EventTransformer(executor.TransformInput{
		SessionID: "s1",
		Executor:  "claude_code",
		Log: executor.Log{
			Type:    "stdout",
			Content: `{"type":"tool_use","tool_name":"Read","input":{"file_path":"/tmp/a.go"}}`,
		},
	})
	toolContent := toolEvt.Content.(executor.UnifiedContent)
	if toolEvt.Type != "tool" || toolContent.Action != "reading" || toolContent.Target != "/tmp/a.go" {
		t.Fatalf("unexpected tool event, got type=%s content=%+v", toolEvt.Type, toolContent)
	}

	searchEvt := EventTransformer(executor.TransformInput{
		SessionID: "s1",
		Executor:  "claude_code",
		Log: executor.Log{
			Type:    "stdout",
			Content: `{"type":"tool_use","tool_name":"Grep","input":{"pattern":"transform"}}`,
		},
	})
	searchContent := searchEvt.Content.(executor.UnifiedContent)
	if searchContent.Action != "searching" {
		t.Fatalf("expected searching action, got %+v", searchContent)
	}

	doneEvt := EventTransformer(executor.TransformInput{
		SessionID: "s1",
		Executor:  "claude_code",
		Log:       executor.Log{Type: "done"},
	})
	d := doneEvt.Content.(executor.UnifiedContent)
	if doneEvt.Type != "done" || d.Action != "completed" {
		t.Fatalf("unexpected done mapping: type=%s content=%+v", doneEvt.Type, d)
	}

	errEvt := EventTransformer(executor.TransformInput{
		SessionID: "s1",
		Executor:  "claude_code",
		Log:       executor.Log{Type: "error", Content: "bad"},
	})
	errContent := errEvt.Content.(executor.UnifiedContent)
	if errEvt.Type != "error" || errContent.Action != "failed" {
		t.Fatalf("unexpected error mapping: type=%s content=%+v", errEvt.Type, errContent)
	}
}

func TestEventTransformer_CommandResultAndStdout(t *testing.T) {
	cmdEvt := EventTransformer(executor.TransformInput{
		SessionID: "s1",
		Executor:  "claude_code",
		Log: executor.Log{
			Type:    "command",
			Content: "npx ...",
		},
	})
	cmdContent := cmdEvt.Content.(executor.UnifiedContent)
	if cmdEvt.Type != "progress" || cmdContent.Action != "starting" {
		t.Fatalf("unexpected command mapping: type=%s content=%+v", cmdEvt.Type, cmdContent)
	}

	resultEvt := EventTransformer(executor.TransformInput{
		SessionID: "s1",
		Executor:  "claude_code",
		Log: executor.Log{
			Type:    "result",
			Content: "Hello",
		},
	})
	resultContent := resultEvt.Content.(executor.UnifiedContent)
	if resultEvt.Type != "message" || resultContent.Summary == "" {
		t.Fatalf("unexpected result mapping: type=%s content=%+v", resultEvt.Type, resultContent)
	}

	assistantEvt := EventTransformer(executor.TransformInput{
		SessionID: "s1",
		Executor:  "claude_code",
		Log: executor.Log{
			Type: "stdout",
			Content: map[string]any{
				"type": "assistant",
				"message": map[string]any{
					"content": []any{map[string]any{"type": "text", "text": "hi"}},
				},
			},
		},
	})
	assistant := assistantEvt.Content.(executor.UnifiedContent)
	if assistantEvt.Type != "message" || assistant.Text != "hi" {
		t.Fatalf("unexpected assistant mapping: type=%s content=%+v", assistantEvt.Type, assistant)
	}
}

func TestHelpers(t *testing.T) {
	if _, ok := parseJSONObject(json.RawMessage(`{"x":1}`)); !ok {
		t.Fatalf("expected raw json parsing")
	}
	if _, ok := parseJSONObject("prefix {\"x\":1}"); !ok {
		t.Fatalf("expected string parsing")
	}
	if _, ok := parseJSONObject(42); ok {
		t.Fatalf("expected parse failure for unsupported type")
	}

	if got := extractClaudeToolName(map[string]any{"name": "Edit"}); got != "Edit" {
		t.Fatalf("unexpected tool name: %s", got)
	}
	if got := extractClaudeTarget(map[string]any{"input": map[string]any{"query": "rg"}}); got != "rg" {
		t.Fatalf("unexpected target: %s", got)
	}
	if got := extractClaudeText(map[string]any{"message": map[string]any{"content": []any{map[string]any{"text": "hello"}}}}); got != "hello" {
		t.Fatalf("unexpected extracted text: %s", got)
	}
	if got := eventTypeForCategory("tool"); got != "tool" {
		t.Fatalf("unexpected category mapping: %s", got)
	}
	if got := eventTypeForCategory("progress"); got != "progress" {
		t.Fatalf("unexpected progress category mapping: %s", got)
	}
	if got := eventTypeForCategory("done"); got != "done" {
		t.Fatalf("unexpected done category mapping: %s", got)
	}
	if got := eventTypeForCategory("approval"); got != "approval" {
		t.Fatalf("unexpected approval category mapping: %s", got)
	}
	if got := eventTypeForCategory("error"); got != "error" {
		t.Fatalf("unexpected error category mapping: %s", got)
	}
	if got := eventTypeForCategory("unknown"); got != "message" {
		t.Fatalf("unexpected fallback category mapping: %s", got)
	}

	if s := defaultSummary(executor.UnifiedContent{Action: "thinking"}); s == "" {
		t.Fatal("defaultSummary should not be empty")
	}
	if s := defaultSummary(executor.UnifiedContent{Action: "reading"}); s == "" {
		t.Fatal("reading summary should not be empty")
	}
	if s := defaultSummary(executor.UnifiedContent{Action: "searching"}); s == "" {
		t.Fatal("searching summary should not be empty")
	}
	if s := defaultSummary(executor.UnifiedContent{Action: "tool_running"}); s == "" {
		t.Fatal("tool summary should not be empty")
	}
	if s := defaultSummary(executor.UnifiedContent{Action: "responding"}); s == "" {
		t.Fatal("responding summary should not be empty")
	}
	if s := defaultSummary(executor.UnifiedContent{Action: "completed"}); s == "" {
		t.Fatal("completed summary should not be empty")
	}
	if s := defaultSummary(executor.UnifiedContent{Action: "failed"}); s == "" {
		t.Fatal("failed summary should not be empty")
	}
	if s := defaultSummary(executor.UnifiedContent{Action: "other"}); s == "" {
		t.Fatal("fallback summary should not be empty")
	}
}

func TestApplyClaudeObjectMapping_Branches(t *testing.T) {
	cases := []map[string]any{
		{
			"type":      "tool_result",
			"tool_name": "Edit",
		},
		{
			"type":    "system",
			"subtype": "init",
		},
		{
			"type": "result",
		},
		{
			"type": "unknown_event",
			"text": "search keyword",
		},
	}

	for _, c := range cases {
		content := executor.UnifiedContent{}
		applyClaudeObjectMapping(&content, c)
		if content.Action == "" {
			t.Fatalf("expected action for %#v, got %+v", c, content)
		}
	}
}

func TestMapToolAction_Branches(t *testing.T) {
	cases := []struct {
		name   string
		tool   string
		target string
		want   string
	}{
		{name: "Read", tool: "Read", target: "/tmp/a.go", want: "reading"},
		{name: "Search", tool: "WebSearch", target: "transform", want: "searching"},
		{name: "Edit", tool: "Edit", want: "editing"},
		{name: "Think", tool: "Task", want: "thinking"},
		{name: "Unknown", tool: "CustomTool", want: "tool_running"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			content := executor.UnifiedContent{ToolName: tc.tool, Target: tc.target}
			mapToolAction(&content)
			if content.Action != tc.want {
				t.Fatalf("unexpected action for %s: %+v", tc.name, content)
			}
		})
	}
}
