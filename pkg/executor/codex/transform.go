package codex

import (
	"encoding/json"
	"strings"

	"github.com/supremeagent/executor/pkg/executor"
)

func EventTransformer(input executor.TransformInput) executor.Event {
	content := executor.UnifiedContent{
		Source:     input.Executor,
		SourceType: input.Log.Type,
		Category:   "message",
		Text:       executor.StringifyContent(input.Log.Content),
		Raw:        input.Log.Content,
	}
	eventType := "message"

	switch input.Log.Type {
	case "done":
		content.Category = "done"
		content.Phase = "completed"
		eventType = "done"
	case "stderr", "error":
		content.Category = "error"
		content.Phase = "failed"
		eventType = "error"
	case "control_request":
		content.Category = "approval"
		content.Phase = "requested"
		eventType = "approval"
		if obj, ok := parseJSONObject(input.Log.Content); ok {
			content.RequestID, _ = obj["request_id"].(string)
			content.ToolName = detectToolFromControl(obj)
		}
	case "init":
		content.Category = "lifecycle"
		content.Phase = "started"
		eventType = "progress"
	default:
		if strings.HasPrefix(input.Log.Type, "codex/event/") {
			content.Category = "progress"
			content.Phase = "delta"
			eventType = "progress"
			if strings.Contains(input.Log.Type, "task_complete") {
				content.Category = "done"
				content.Phase = "completed"
				eventType = "done"
			}
			if strings.Contains(input.Log.Type, "exec_command") {
				content.Category = "tool"
				content.ToolName = "bash"
				eventType = "tool"
			}
			if strings.Contains(input.Log.Type, "patch") {
				content.Category = "tool"
				content.ToolName = "edit"
				eventType = "tool"
			}
		}
	}

	return executor.Event{
		Type:    eventType,
		Content: content,
	}
}

func parseJSONObject(v any) (map[string]any, bool) {
	switch val := v.(type) {
	case map[string]any:
		return val, true
	case json.RawMessage:
		var out map[string]any
		if err := json.Unmarshal(val, &out); err == nil {
			return out, true
		}
	case string:
		var out map[string]any
		if err := json.Unmarshal([]byte(val), &out); err == nil {
			return out, true
		}
	}
	return nil, false
}

func detectToolFromControl(obj map[string]any) string {
	params, _ := obj["params"].(map[string]any)
	method, _ := obj["method"].(string)
	if strings.Contains(strings.ToLower(method), "patch") {
		return "edit"
	}
	if strings.Contains(strings.ToLower(method), "exec") {
		return "bash"
	}
	if params != nil {
		if tool, ok := params["tool"].(string); ok {
			return tool
		}
	}
	return ""
}
