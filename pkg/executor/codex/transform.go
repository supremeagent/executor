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
			content.RequestID, _ = obj["request_id"].(string)
			content.ToolName = detectToolFromControl(obj)
			if content.ToolName != "" {
				content.Summary = fmt.Sprintf("等待审批：%s", content.ToolName)
			}
		}
	case "init":
		content.Category = "lifecycle"
		content.Action = "starting"
		content.Phase = "started"
		content.Summary = "正在启动 Codex"
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
		content.Summary = "任务执行完成"
	case strings.Contains(msgType, "task_started"):
		content.Action = "thinking"
		content.Summary = "正在分析任务"
	case strings.Contains(msgType, "mcp_startup"):
		content.Action = "starting"
		content.Summary = "正在初始化工具"
		if content.Target != "" {
			content.Summary = fmt.Sprintf("正在初始化工具：%s", content.Target)
		}
		if content.Status != "" {
			content.Summary = fmt.Sprintf("工具 %s 状态：%s", content.Target, content.Status)
		}
	case strings.Contains(msgType, "search"):
		content.Action = "searching"
		content.Summary = "正在进行搜索"
	case strings.Contains(msgType, "read"):
		content.Action = "reading"
		content.Summary = "正在读取文件"
	case strings.Contains(msgType, "exec_command"):
		content.Category = "tool"
		content.Action = "tool_running"
		content.ToolName = fallback(content.ToolName, "bash")
		content.Summary = "正在执行命令"
		if content.Target != "" {
			content.Summary = fmt.Sprintf("正在执行命令：%s", content.Target)
		}
	case strings.Contains(msgType, "patch") || strings.Contains(msgType, "edit") || strings.Contains(msgType, "apply"):
		content.Category = "tool"
		content.Action = "editing"
		content.ToolName = fallback(content.ToolName, "edit")
		content.Summary = "正在修改代码"
	case strings.Contains(msgType, "agent_message"):
		content.Action = "responding"
		content.Summary = "正在组织回复"
	default:
		content.Summary = fmt.Sprintf("处理中：%s", msgType)
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
