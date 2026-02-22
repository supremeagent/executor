package claude

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/supremeagent/executor/pkg/executor"
)

func EventTransformer(input executor.TransformInput) executor.Event {
	content := executor.UnifiedContent{
		Source:     input.Executor,
		SourceType: input.Log.Type,
		Category:   "message",
		Action:     "responding",
		Text:       executor.StringifyContent(input.Log.Content),
		Raw:        input.Log.Content,
	}
	eventType := "message"

	switch input.Log.Type {
	case "done":
		content.Category = "done"
		content.Action = "completed"
		content.Phase = "completed"
		content.Summary = "Execution completed"
		eventType = "done"
	case "stderr", "error":
		content.Category = "error"
		content.Action = "failed"
		content.Phase = "failed"
		content.Summary = "Execution failed"
		eventType = "error"
	case "control_request":
		content.Category = "approval"
		content.Action = "approval_required"
		content.Phase = "requested"
		content.Summary = "Waiting for user approval"
		eventType = "approval"
		if obj, ok := parseJSONObject(input.Log.Content); ok {
			if request, ok := obj["request"].(map[string]any); ok {
				content.ToolName, _ = request["tool_name"].(string)
			}
			content.RequestID, _ = obj["request_id"].(string)
			if content.ToolName != "" {
				content.Summary = fmt.Sprintf("Waiting for approval: %s", content.ToolName)
			}
		}
	case "result":
		content.Category = "message"
		content.Action = "responding"
		content.Phase = "completed"
		content.Summary = "Returning results"
	case "command":
		content.Category = "lifecycle"
		content.Action = "starting"
		content.Phase = "started"
		content.Summary = "Starting Claude Code"
		eventType = "progress"
	default:
		if obj, ok := parseJSONObject(input.Log.Content); ok {
			applyClaudeObjectMapping(&content, obj)
			eventType = eventTypeForCategory(content.Category)
		}
	}

	if content.Summary == "" {
		content.Summary = defaultSummary(content)
	}

	return executor.Event{
		Type:    eventType,
		Content: content,
	}
}

func applyClaudeObjectMapping(content *executor.UnifiedContent, obj map[string]any) {
	typeName, _ := obj["type"].(string)
	subtype, _ := obj["subtype"].(string)

	if msgText := extractClaudeText(obj); msgText != "" {
		content.Text = msgText
	}

	switch typeName {
	case "tool_use":
		content.Category = "tool"
		content.Phase = "started"
		content.ToolName = extractClaudeToolName(obj)
		content.Target = extractClaudeTarget(obj)
		mapToolAction(content)
	case "tool_result":
		content.Category = "tool"
		content.Phase = "completed"
		content.ToolName = extractClaudeToolName(obj)
		content.Target = extractClaudeTarget(obj)
		mapToolAction(content)
		if content.Summary != "" {
			content.Summary = strings.Replace(content.Summary, "Starting", "Completed", 1)
		}
	case "assistant", "message":
		content.Category = "message"
		content.Action = "responding"
		content.Summary = "Generating reply"
	case "system":
		content.Category = "progress"
		if subtype == "init" {
			content.Action = "thinking"
			content.Summary = "Initializing session"
		} else {
			content.Action = "thinking"
			content.Summary = "Processing system events"
		}
	case "result":
		content.Category = "done"
		content.Action = "completed"
		content.Phase = "completed"
		content.Summary = "Task execution completed"
	default:
		if strings.Contains(strings.ToLower(content.Text), "search") {
			content.Action = "searching"
			content.Category = "progress"
			content.Summary = "Searching"
		} else {
			content.Action = "thinking"
			content.Category = "progress"
			content.Summary = "Thinking deeply"
		}
	}
}

func mapToolAction(content *executor.UnifiedContent) {
	name := strings.ToLower(content.ToolName)
	target := content.Target

	switch {
	case strings.Contains(name, "read"):
		content.Action = "reading"
		content.Summary = "Reading file"
		if target != "" {
			content.Summary = fmt.Sprintf("Reading %s", target)
		}
	case strings.Contains(name, "grep"), strings.Contains(name, "glob"), strings.Contains(name, "search"), strings.Contains(name, "webfetch"), strings.Contains(name, "websearch"):
		content.Action = "searching"
		content.Summary = "Searching"
		if target != "" {
			content.Summary = fmt.Sprintf("Searching: %s", target)
		}
	case strings.Contains(name, "edit"), strings.Contains(name, "write"), strings.Contains(name, "notebook"):
		content.Action = "editing"
		content.Summary = "Modifying code"
	case strings.Contains(name, "task") || strings.Contains(name, "plan"):
		content.Action = "thinking"
		content.Category = "progress"
		content.Summary = "Thinking deeply"
	default:
		content.Action = "tool_running"
		content.Summary = "Calling tool"
		if content.ToolName != "" {
			content.Summary = fmt.Sprintf("Calling tool: %s", content.ToolName)
		}
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

func extractClaudeTarget(obj map[string]any) string {
	if input, ok := obj["input"].(map[string]any); ok {
		if path, ok := input["file_path"].(string); ok && path != "" {
			return path
		}
		if path, ok := input["path"].(string); ok && path != "" {
			return path
		}
		if pattern, ok := input["pattern"].(string); ok && pattern != "" {
			return pattern
		}
		if query, ok := input["query"].(string); ok && query != "" {
			return query
		}
	}
	return ""
}

func extractClaudeText(obj map[string]any) string {
	if result, ok := obj["result"].(string); ok && result != "" {
		return result
	}
	if msg, ok := obj["message"].(map[string]any); ok {
		if content, ok := msg["content"].([]any); ok {
			parts := make([]string, 0, len(content))
			for _, item := range content {
				block, ok := item.(map[string]any)
				if !ok {
					continue
				}
				if text, ok := block["text"].(string); ok && text != "" {
					parts = append(parts, text)
				}
			}
			return strings.Join(parts, "\n")
		}
	}
	return ""
}

func eventTypeForCategory(category string) string {
	switch category {
	case "tool":
		return "tool"
	case "progress", "lifecycle":
		return "progress"
	case "done":
		return "done"
	case "approval":
		return "approval"
	case "error":
		return "error"
	default:
		return "message"
	}
}

func defaultSummary(content executor.UnifiedContent) string {
	switch content.Action {
	case "thinking":
		return "Thinking deeply"
	case "reading":
		return "Reading file"
	case "searching":
		return "Searching"
	case "tool_running":
		return "Calling tool"
	case "responding":
		return "Generating reply"
	case "completed":
		return "Execution completed"
	case "failed":
		return "Execution failed"
	default:
		return "Processing"
	}
}
