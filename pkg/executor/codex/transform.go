package codex

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
			content.RequestID, _ = obj["request_id"].(string)
			content.ToolName = detectToolFromControl(obj)
			if content.ToolName != "" {
				content.Summary = fmt.Sprintf("Waiting for approval: %s", content.ToolName)
			}
		}
	case "init":
		content.Category = "lifecycle"
		content.Action = "starting"
		content.Phase = "started"
		content.Summary = "Starting Codex"
		eventType = "progress"
	default:
		if strings.HasPrefix(input.Log.Type, "codex/event/") {
			content.Category = "progress"
			content.Action = "thinking"
			content.Phase = "delta"
			eventType = "progress"
			applyCodexEventMapping(&content, input.Log.Type, input.Log.Content)
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

func applyCodexEventMapping(content *executor.UnifiedContent, logType string, raw any) {
	msgType := strings.TrimPrefix(logType, "codex/event/")
	if obj, ok := parseJSONObject(raw); ok {
		if msg, ok := obj["msg"].(map[string]any); ok {
			if t, ok := msg["type"].(string); ok && t != "" {
				msgType = t
			}
			if server, ok := msg["server"].(string); ok {
				content.Target = server
			}
			if state := nestedString(msg, "status", "state"); state != "" {
				content.Status = state
			}
			if toolName := nestedString(msg, "call", "name"); toolName != "" {
				content.ToolName = toolName
			}
			if cmd := nestedString(msg, "call", "arguments", "cmd"); cmd != "" {
				content.Target = cmd
			}
			if path := nestedString(msg, "call", "arguments", "path"); path != "" {
				content.Target = path
			}
		}
	}

	switch {
	case strings.Contains(msgType, "task_complete"):
		content.Category = "done"
		content.Action = "completed"
		content.Phase = "completed"
		content.Summary = "Task execution completed"
	case strings.Contains(msgType, "task_started"):
		content.Action = "thinking"
		content.Summary = "Analyzing task"
	case strings.Contains(msgType, "mcp_startup"):
		content.Action = "starting"
		content.Summary = "Initializing tool"
		if content.Target != "" {
			content.Summary = fmt.Sprintf("Initializing tool: %s", content.Target)
		}
		if content.Status != "" {
			content.Summary = fmt.Sprintf("Tool %s status: %s", content.Target, content.Status)
		}
	case strings.Contains(msgType, "search"):
		content.Action = "searching"
		content.Summary = "Searching"
	case strings.Contains(msgType, "read"):
		content.Action = "reading"
		content.Summary = "Reading file"
	case strings.Contains(msgType, "exec_command"):
		content.Category = "tool"
		content.Action = "tool_running"
		content.ToolName = fallback(content.ToolName, "bash")
		content.Summary = "Executing command"
		if content.Target != "" {
			content.Summary = fmt.Sprintf("Executing command: %s", content.Target)
		}
	case strings.Contains(msgType, "patch") || strings.Contains(msgType, "edit") || strings.Contains(msgType, "apply"):
		content.Category = "tool"
		content.Action = "editing"
		content.ToolName = fallback(content.ToolName, "edit")
		content.Summary = "Modifying code"
	case strings.Contains(msgType, "agent_message"):
		content.Action = "responding"
		content.Summary = "Organizing reply"
	default:
		content.Summary = fmt.Sprintf("Processing: %s", msgType)
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

func nestedString(src map[string]any, path ...string) string {
	var cur any = src
	for _, p := range path {
		obj, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = obj[p]
	}
	out, _ := cur.(string)
	return out
}

func fallback(v, d string) string {
	if v != "" {
		return v
	}
	return d
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
