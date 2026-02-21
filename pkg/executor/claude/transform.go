package claude

import "github.com/supremeagent/executor/pkg/executor"

func EventTransformer(input executor.TransformInput) executor.Event {
	category := "message"
	switch input.Log.Type {
	case "done":
		category = "done"
	case "stderr", "error":
		category = "error"
	}

	return executor.Event{
		Type: category,
		Content: executor.UnifiedContent{
			Source:     input.Executor,
			SourceType: input.Log.Type,
			Category:   category,
			Text:       executor.StringifyContent(input.Log.Content),
			Raw:        input.Log.Content,
		},
	}
}
