// Package droid – event transformer for the Droid executor.
package droid

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/supremeagent/executor/pkg/executor"
)

// EventTransformer converts Droid stream-json executor logs into the unified event format.
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

	case "command":
		content.Category = "lifecycle"
		content.Action = "starting"
		content.Phase = "started"
		content.Summary = "正在启动 Droid"
		eventType = "progress"

	case "droid_system":
		content.Category = "lifecycle"
		content.Action = "starting"
		content.Phase = "started"
		content.Summary = "正在初始化会话"
		eventType = "progress"
		if evt, ok := parseDroidEvent(input.Log.Content); ok && evt.Model != "" {
			content.Summary = fmt.Sprintf("正在初始化会话，模型：%s", evt.Model)
		}

	case "droid_message":
		if evt, ok := parseDroidEvent(input.Log.Content); ok {
			switch evt.Role {
			case "assistant":
				content.Category = "message"
				content.Action = "responding"
				content.Summary = "正在生成回复"
				content.Text = evt.Text
			case "user":
				content.Category = "message"
				content.Action = "responding"
				content.Summary = "用户消息"
				content.Text = evt.Text
				eventType = "progress"
			default:
				content.Category = "message"
				content.Action = "responding"
				content.Summary = "消息"
				content.Text = evt.Text
			}
		}

	case "droid_tool_call":
		eventType = "tool"
		content.Category = "tool"
		content.Phase = "started"
		if evt, ok := parseDroidEvent(input.Log.Content); ok {
			content.ToolName = evt.ToolName
			applyDroidToolMapping(&content, evt.ToolName)
		}

	case "droid_tool_result":
		eventType = "tool"
		content.Category = "tool"
		content.Phase = "completed"
		if evt, ok := parseDroidEvent(input.Log.Content); ok {
			content.ToolName = evt.ToolName
			applyDroidToolMapping(&content, evt.ToolName)
			if evt.IsError {
				content.Phase = "failed"
				content.Status = "failed"
			} else {
				content.Status = "success"
			}
		}

	case "droid_completion":
		content.Category = "done"
		content.Action = "completed"
		content.Phase = "completed"
		content.Summary = "任务执行完成"
		eventType = "done"
		if evt, ok := parseDroidEvent(input.Log.Content); ok && evt.FinalText != "" {
			content.Text = evt.FinalText
		}

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

// applyDroidToolMapping maps a Droid tool name to human-readable action fields.
func applyDroidToolMapping(content *executor.UnifiedContent, toolName string) {
	name := strings.ToLower(toolName)

	switch {
	case name == "read" || name == "ls":
		content.Action = "reading"
		content.Summary = "正在读取文件"
		if content.ToolName != "" {
			content.Summary = fmt.Sprintf("正在读取 %s", content.ToolName)
		}
	case name == "grep" || name == "glob" || strings.Contains(name, "search") || strings.Contains(name, "websearch"):
		content.Action = "searching"
		content.Summary = "正在进行搜索"
	case name == "edit" || name == "multiedit" || name == "create" || name == "applypatch":
		content.Action = "editing"
		content.Summary = "正在修改代码"
		if content.ToolName != "" {
			content.Summary = fmt.Sprintf("正在编辑文件")
		}
	case name == "execute":
		content.Action = "tool_running"
		content.Summary = "正在执行命令"
	case name == "todowrite":
		content.Category = "progress"
		content.Action = "thinking"
		content.Summary = "正在更新任务列表"
	case strings.Contains(name, "fetch") || strings.Contains(name, "url"):
		content.Action = "searching"
		content.Summary = "正在获取网页"
	default:
		content.Action = "tool_running"
		content.Summary = "正在调用工具"
		if content.ToolName != "" {
			content.Summary = fmt.Sprintf("正在调用工具：%s", content.ToolName)
		}
	}
}

// parseDroidEvent extracts a DroidEvent from a Log content value.
func parseDroidEvent(raw any) (DroidEvent, bool) {
	switch v := raw.(type) {
	case DroidEvent:
		return v, true
	case map[string]any:
		data, err := json.Marshal(v)
		if err != nil {
			return DroidEvent{}, false
		}
		var evt DroidEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			return DroidEvent{}, false
		}
		return evt, true
	case json.RawMessage:
		var evt DroidEvent
		if err := json.Unmarshal(v, &evt); err != nil {
			return DroidEvent{}, false
		}
		return evt, true
	}
	return DroidEvent{}, false
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
