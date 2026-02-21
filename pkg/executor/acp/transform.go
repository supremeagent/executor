package acp

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/supremeagent/executor/pkg/executor"
)

// EventTransformer converts ACP executor logs into the unified event format.
// It is shared by Gemini, Qwen, and Copilot executors.
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

	case "acp_done":
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

	case "command":
		content.Category = "lifecycle"
		content.Action = "starting"
		content.Phase = "started"
		content.Summary = "正在启动执行器"
		eventType = "progress"

	case "session_start":
		content.Category = "lifecycle"
		content.Action = "starting"
		content.Phase = "started"
		content.Summary = "会话已启动"
		eventType = "progress"

	case "control_request":
		content.Category = "approval"
		content.Action = "approval_required"
		content.Phase = "requested"
		content.Summary = "等待用户审批"
		eventType = "approval"
		if obj, ok := parseJSONObject(input.Log.Content); ok {
			if reqID, ok := obj["request_id"].(string); ok {
				content.RequestID = reqID
			}
			if tc, ok := obj["tool_call"].(map[string]any); ok {
				if title, ok := tc["title"].(string); ok && title != "" {
					content.ToolName = title
				}
				if kind, ok := tc["kind"].(string); ok && content.ToolName == "" {
					content.ToolName = strings.ToLower(kind)
				}
			}
			if content.ToolName != "" {
				content.Summary = fmt.Sprintf("等待审批：%s", content.ToolName)
			}
		}

	// ACP event types
	case string(EventTypeMessage):
		content.Category = "message"
		content.Action = "responding"
		content.Summary = "正在生成回复"
		if obj, ok := parseJSONObject(input.Log.Content); ok {
			if txt := extractTextFromACPContent(obj); txt != "" {
				content.Text = txt
			}
		}

	case string(EventTypeThought):
		content.Category = "progress"
		content.Action = "thinking"
		content.Summary = "正在深度思考"
		eventType = "progress"

	case string(EventTypeToolCall), string(EventTypeToolUpdate):
		applyACPToolMapping(&content, input.Log.Content)
		eventType = "tool"

	case string(EventTypePlan):
		content.Category = "progress"
		content.Action = "thinking"
		content.Summary = "正在制定计划"
		eventType = "progress"

	default:
		content.Category = "progress"
		content.Action = "thinking"
		content.Summary = "正在处理中"
		eventType = "progress"
	}

	if content.Summary == "" {
		content.Summary = defaultSummary(content)
	}

	return executor.Event{
		Type:    eventType,
		Content: content,
	}
}

// applyACPToolMapping reads a ToolCall or ToolUpdate payload and fills content fields.
func applyACPToolMapping(content *executor.UnifiedContent, raw any) {
	content.Category = "tool"
	content.Phase = "started"

	obj, ok := parseJSONObject(raw)
	if !ok {
		content.Action = "tool_running"
		content.Summary = "正在调用工具"
		return
	}

	// ToolCall/ToolUpdate payloads may be wrapped in an outer object
	// with a single key equal to the event type.
	tc := unwrapToolPayload(obj)

	if title, ok := tc["title"].(string); ok && title != "" {
		content.ToolName = title
	}
	if status, ok := tc["status"].(string); ok {
		content.Status = status
		if status == string(ToolStatusCompleted) {
			content.Phase = "completed"
		} else if status == string(ToolStatusFailed) {
			content.Phase = "failed"
		}
	}

	kind, _ := tc["kind"].(string)
	mapACPToolKind(content, ToolKind(kind))
}

// unwrapToolPayload returns the inner map if the object is wrapped in a
// single-key envelope (e.g. {"ToolCall": {...}}).
func unwrapToolPayload(obj map[string]any) map[string]any {
	if len(obj) == 1 {
		for _, v := range obj {
			if inner, ok := v.(map[string]any); ok {
				return inner
			}
		}
	}
	return obj
}

// mapACPToolKind maps an ACP ToolKind to human-readable content fields.
func mapACPToolKind(content *executor.UnifiedContent, kind ToolKind) {
	switch kind {
	case ToolKindRead:
		content.Action = "reading"
		content.Summary = "正在读取文件"
		if content.ToolName != "" {
			content.Summary = fmt.Sprintf("正在读取 %s", content.ToolName)
		}
	case ToolKindEdit:
		content.Action = "editing"
		content.Summary = "正在修改代码"
		if content.ToolName != "" {
			content.Summary = fmt.Sprintf("正在编辑 %s", content.ToolName)
		}
	case ToolKindExecute:
		content.Action = "tool_running"
		content.Summary = "正在执行命令"
		if content.ToolName != "" {
			content.Summary = fmt.Sprintf("正在执行：%s", content.ToolName)
		}
	case ToolKindSearch:
		content.Action = "searching"
		content.Summary = "正在进行搜索"
		if content.ToolName != "" {
			content.Summary = fmt.Sprintf("正在搜索：%s", content.ToolName)
		}
	case ToolKindFetch:
		content.Action = "searching"
		content.Summary = "正在获取网页"
	case ToolKindDelete:
		content.Action = "editing"
		content.Summary = "正在删除文件"
	case ToolKindThink:
		content.Category = "progress"
		content.Action = "thinking"
		content.Summary = "正在深度思考"
	default:
		content.Action = "tool_running"
		content.Summary = "正在调用工具"
		if content.ToolName != "" {
			content.Summary = fmt.Sprintf("正在调用工具：%s", content.ToolName)
		}
	}
}

// extractTextFromACPContent extracts text from an ACP Message content block.
func extractTextFromACPContent(obj map[string]any) string {
	// Message event wraps a ContentBlock, which is typically {"Text": {"text": "..."}}
	for _, v := range obj {
		if inner, ok := v.(map[string]any); ok {
			if text, ok := inner["text"].(string); ok {
				return text
			}
		}
	}
	if text, ok := obj["text"].(string); ok {
		return text
	}
	return ""
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

func defaultSummary(content executor.UnifiedContent) string {
	switch content.Action {
	case "thinking":
		return "正在深度思考"
	case "reading":
		return "正在读取文件"
	case "searching":
		return "正在进行搜索"
	case "editing":
		return "正在修改代码"
	case "tool_running":
		return "正在调用工具"
	case "responding":
		return "正在生成回复"
	case "completed":
		return "执行完成"
	case "failed":
		return "执行失败"
	case "starting":
		return "正在启动"
	default:
		return "处理中"
	}
}
