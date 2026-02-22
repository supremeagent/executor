package qwen

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
		content.Summary = "执行完成"
		eventType = "done"
	case "stderr", "error":
		content.Category = "error"
		content.Action = "failed"
		content.Phase = "failed"
		content.Summary = "执行失败"
		eventType = "error"
	case "control_request":
		content.Category = "approval"
		content.Action = "approval_required"
		content.Phase = "requested"
		content.Summary = "等待用户审批"
		eventType = "approval"
		if obj, ok := parseJSONObject(input.Log.Content); ok {
			if request, ok := obj["request"].(map[string]any); ok {
				content.ToolName, _ = request["tool_name"].(string)
			}
			content.RequestID, _ = obj["request_id"].(string)
			if content.ToolName != "" {
				content.Summary = fmt.Sprintf("等待审批：%s", content.ToolName)
			}
		}
	case "result":
		content.Category = "message"
		content.Action = "responding"
		content.Phase = "completed"
		content.Summary = "正在返回结果"
	case "command":
		content.Category = "lifecycle"
		content.Action = "starting"
		content.Phase = "started"
		content.Summary = "正在启动 Claude Code"
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
			content.Summary = strings.Replace(content.Summary, "正在", "已完成", 1)
		}
	case "assistant", "message":
		content.Category = "message"
		content.Action = "responding"
		content.Summary = "正在生成回复"
	case "system":
		content.Category = "progress"
		if subtype == "init" {
			content.Action = "thinking"
			content.Summary = "正在初始化会话"
		} else {
			content.Action = "thinking"
			content.Summary = "正在处理系统事件"
		}
	case "result":
		content.Category = "done"
		content.Action = "completed"
		content.Phase = "completed"
		content.Summary = "任务执行完成"
	default:
		if strings.Contains(strings.ToLower(content.Text), "search") {
			content.Action = "searching"
			content.Category = "progress"
			content.Summary = "正在进行搜索"
		} else {
			content.Action = "thinking"
			content.Category = "progress"
			content.Summary = "正在深度思考"
		}
	}
}

func mapToolAction(content *executor.UnifiedContent) {
	name := strings.ToLower(content.ToolName)
	target := content.Target

	switch {
	case strings.Contains(name, "read"):
		content.Action = "reading"
		content.Summary = "正在读取文件"
		if target != "" {
			content.Summary = fmt.Sprintf("正在读取 %s", target)
		}
	case strings.Contains(name, "grep"), strings.Contains(name, "glob"), strings.Contains(name, "search"), strings.Contains(name, "webfetch"), strings.Contains(name, "websearch"):
		content.Action = "searching"
		content.Summary = "正在进行搜索"
		if target != "" {
			content.Summary = fmt.Sprintf("正在搜索：%s", target)
		}
	case strings.Contains(name, "edit"), strings.Contains(name, "write"), strings.Contains(name, "notebook"):
		content.Action = "editing"
		content.Summary = "正在修改代码"
	case strings.Contains(name, "task") || strings.Contains(name, "plan"):
		content.Action = "thinking"
		content.Category = "progress"
		content.Summary = "正在深度思考"
	default:
		content.Action = "tool_running"
		content.Summary = "正在调用工具"
		if content.ToolName != "" {
			content.Summary = fmt.Sprintf("正在调用工具：%s", content.ToolName)
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
		return "正在深度思考"
	case "reading":
		return "正在读取文件"
	case "searching":
		return "正在进行搜索"
	case "tool_running":
		return "正在调用工具"
	case "responding":
		return "正在生成回复"
	case "completed":
		return "执行完成"
	case "failed":
		return "执行失败"
	default:
		return "处理中"
	}
}
