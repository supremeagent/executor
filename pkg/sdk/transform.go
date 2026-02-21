package sdk

import (
	"github.com/supremeagent/executor/pkg/executor"
	"github.com/supremeagent/executor/pkg/executor/claude"
	"github.com/supremeagent/executor/pkg/executor/codex"
)

func defaultEventTransformers() map[string]executor.EventTransformer {
	return map[string]executor.EventTransformer{
		string(executor.ExecutorCodex):      codex.EventTransformer,
		string(executor.ExecutorClaudeCode): claude.EventTransformer,
	}
}

func (c *Client) transformEvent(sessionID, executorName string, logEntry executor.Log) executor.Event {
	evt := executor.Event{
		SessionID: sessionID,
		Executor:  executorName,
		Type:      logEntry.Type,
		Content:   logEntry.Content,
	}

	if tf, ok := c.transforms[executorName]; ok && tf != nil {
		transformed := tf(executor.TransformInput{
			SessionID: sessionID,
			Executor:  executorName,
			Log:       logEntry,
		})

		if transformed.SessionID == "" {
			transformed.SessionID = sessionID
		}
		if transformed.Executor == "" {
			transformed.Executor = executorName
		}
		if transformed.Type == "" {
			transformed.Type = logEntry.Type
		}
		if transformed.Content == nil {
			transformed.Content = logEntry.Content
		}

		evt = transformed
	}

	return evt
}
