// Package gemini – event transformer for Gemini CLI executor.
package gemini

import (
	"github.com/supremeagent/executor/pkg/executor"
	"github.com/supremeagent/executor/pkg/executor/acp"
)

// EventTransformer converts Gemini executor logs into the unified event format.
// It delegates to the shared ACP transformer, appending the executor name.
func EventTransformer(input executor.TransformInput) executor.Event {
	return acp.EventTransformer(input)
}
