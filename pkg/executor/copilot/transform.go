// Package copilot – event transformer for GitHub Copilot CLI executor.
package copilot

import (
	"github.com/supremeagent/executor/pkg/executor"
	"github.com/supremeagent/executor/pkg/executor/acp"
)

// EventTransformer converts Copilot executor logs into the unified event format.
// It delegates to the shared ACP transformer.
func EventTransformer(input executor.TransformInput) executor.Event {
	return acp.EventTransformer(input)
}
