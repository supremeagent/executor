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
		content.Summary = "Starting Droid"
		eventType = "progress"

	case "droid_system":
		content.Category = "lifecycle"
		content.Action = "starting"
		content.Phase = "started"
		content.Summary = "Initializing session"
		eventType = "progress"
		if evt, ok := parseDroidEvent(input.Log.Content); ok && evt.Model != "" {
			content.Summary = fmt.Sprintf("Initializing session, model: %s", evt.Model)
		}

	case "droid_message":
		if evt, ok := parseDroidEvent(input.Log.Content); ok {
			switch evt.Role {
			case "assistant":
				content.Category = "message"
				content.Action = "responding"
				content.Summary = "Generating reply"
				content.Text = evt.Text
			case "user":
				content.Category = "message"
				content.Action = "responding"
				content.Summary = "User message"
				content.Text = evt.Text
				eventType = "progress"
			default:
				content.Category = "message"
				content.Action = "responding"
				content.Summary = "Message"
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
		content.Summary = "Task execution completed"
		eventType = "done"
		if evt, ok := parseDroidEvent(input.Log.Content); ok && evt.FinalText != "" {
			content.Text = evt.FinalText
		}

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

// applyDroidToolMapping maps a Droid tool name to human-readable action fields.
func applyDroidToolMapping(content *executor.UnifiedContent, toolName string) {
	name := strings.ToLower(toolName)

	switch {
	case name == "read" || name == "ls":
		content.Action = "reading"
		content.Summary = "Reading file"
		if content.ToolName != "" {
			content.Summary = fmt.Sprintf("Reading %s", content.ToolName)
		}
	case name == "grep" || name == "glob" || strings.Contains(name, "search") || strings.Contains(name, "websearch"):
		content.Action = "searching"
		content.Summary = "Searching"
	case name == "edit" || name == "multiedit" || name == "create" || name == "applypatch":
		content.Action = "editing"
		content.Summary = "Modifying code"
		if content.ToolName != "" {
			content.Summary = fmt.Sprintf("Editing file")
		}
	case name == "execute":
		content.Action = "tool_running"
		content.Summary = "Executing command"
	case name == "todowrite":
		content.Category = "progress"
		content.Action = "thinking"
		content.Summary = "Updating task list"
	case strings.Contains(name, "fetch") || strings.Contains(name, "url"):
		content.Action = "searching"
		content.Summary = "Fetching webpage"
	default:
		content.Action = "tool_running"
		content.Summary = "Calling tool"
		if content.ToolName != "" {
			content.Summary = fmt.Sprintf("Calling tool: %s", content.ToolName)
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
