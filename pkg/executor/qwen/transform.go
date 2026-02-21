// Package qwen – event transformer for Qwen Code executor.
package qwen

import (
	"github.com/supremeagent/executor/pkg/executor"
	"github.com/supremeagent/executor/pkg/executor/acp"
)

// EventTransformer converts Qwen executor logs into the unified event format.
// It delegates to the shared ACP transformer.
func EventTransformer(input executor.TransformInput) executor.Event {
	return acp.EventTransformer(input)
}
