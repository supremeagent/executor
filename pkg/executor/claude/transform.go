package claude

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
			if request, ok := obj["request"].(map[string]any); ok {
				content.ToolName, _ = request["tool_name"].(string)
			}
			content.RequestID, _ = obj["request_id"].(string)
		}
	case "result":
		content.Phase = "completed"
	case "command":
		content.Category = "lifecycle"
		content.Phase = "started"
		eventType = "progress"
	default:
		if obj, ok := parseJSONObject(input.Log.Content); ok {
			switch obj["type"] {
			case "tool_use":
				content.Category = "tool"
				content.Phase = "started"
				content.ToolName = extractClaudeToolName(obj)
				eventType = "tool"
			case "tool_result":
				content.Category = "tool"
				content.Phase = "completed"
				content.ToolName = extractClaudeToolName(obj)
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
		text := strings.TrimSpace(val)
		if text == "" {
			return nil, false
		}
		start := strings.Index(text, "{")
		if start >= 0 {
			text = text[start:]
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(text), &out); err == nil {
			return out, true
		}
	}
	return nil, false
}

func extractClaudeToolName(obj map[string]any) string {
	if name, ok := obj["tool_name"].(string); ok && name != "" {
		return name
	}
	if name, ok := obj["name"].(string); ok && name != "" {
		return name
	}
	return ""
}
