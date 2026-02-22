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
		content.Summary = "Execution completed"
		eventType = "done"

	case "acp_done":
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

	case "command":
		content.Category = "lifecycle"
		content.Action = "starting"
		content.Phase = "started"
		content.Summary = "Starting executor"
		eventType = "progress"

	case "session_start":
		content.Category = "lifecycle"
		content.Action = "starting"
		content.Phase = "started"
		content.Summary = "Session started"
		eventType = "progress"

	case "control_request":
		content.Category = "approval"
		content.Action = "approval_required"
		content.Phase = "requested"
		content.Summary = "Waiting for user approval"
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
				content.Summary = fmt.Sprintf("Waiting for approval: %s", content.ToolName)
			}
		}

	// ACP event types
	case string(EventTypeMessage):
		content.Category = "message"
		content.Action = "responding"
		content.Summary = "Generating reply"
		if obj, ok := parseJSONObject(input.Log.Content); ok {
			if txt := extractTextFromACPContent(obj); txt != "" {
				content.Text = txt
			}
		}

	case string(EventTypeThought):
		content.Category = "progress"
		content.Action = "thinking"
		content.Summary = "Thinking deeply"
		eventType = "progress"

	case string(EventTypeToolCall), string(EventTypeToolUpdate):
		applyACPToolMapping(&content, input.Log.Content)
		eventType = "tool"

	case string(EventTypePlan):
		content.Category = "progress"
		content.Action = "thinking"
		content.Summary = "Making a plan"
		eventType = "progress"

	default:
		content.Category = "progress"
		content.Action = "thinking"
		content.Summary = "Processing"
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
		content.Summary = "Calling tool"
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
		content.Summary = "Reading file"
		if content.ToolName != "" {
			content.Summary = fmt.Sprintf("Reading %s", content.ToolName)
		}
	case ToolKindEdit:
		content.Action = "editing"
		content.Summary = "Modifying code"
		if content.ToolName != "" {
			content.Summary = fmt.Sprintf("Editing %s", content.ToolName)
		}
	case ToolKindExecute:
		content.Action = "tool_running"
		content.Summary = "Executing command"
		if content.ToolName != "" {
			content.Summary = fmt.Sprintf("Executing: %s", content.ToolName)
		}
	case ToolKindSearch:
		content.Action = "searching"
		content.Summary = "Searching"
		if content.ToolName != "" {
			content.Summary = fmt.Sprintf("Searching: %s", content.ToolName)
		}
	case ToolKindFetch:
		content.Action = "searching"
		content.Summary = "Fetching webpage"
	case ToolKindDelete:
		content.Action = "editing"
		content.Summary = "Deleting file"
	case ToolKindThink:
		content.Category = "progress"
		content.Action = "thinking"
		content.Summary = "Thinking deeply"
	default:
		content.Action = "tool_running"
		content.Summary = "Calling tool"
		if content.ToolName != "" {
			content.Summary = fmt.Sprintf("Calling tool: %s", content.ToolName)
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
		return "Thinking deeply"
	case "reading":
		return "Reading file"
	case "searching":
		return "Searching"
	case "editing":
		return "Modifying code"
	case "tool_running":
		return "Calling tool"
	case "responding":
		return "Generating reply"
	case "completed":
		return "Execution completed"
	case "failed":
		return "Execution failed"
	case "starting":
		return "Starting"
	default:
		return "Processing"
	}
}
