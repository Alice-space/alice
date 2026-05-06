package connector

import (
	"context"

	"github.com/Alice-space/alice/internal/automation"
	llm "github.com/Alice-space/alice/internal/llm"
)

// NewGoalRunHelper creates a GoalRunHelper that delegates to the Processor's
// RunGoalMessage pipeline, giving goals the same OnProgress and OnRawEvent
// dispatching that regular user messages receive.
func NewGoalRunHelper(processor *Processor) automation.GoalRunHelper {
	if processor == nil {
		return nil
	}
	return &goalRunHelper{processor: processor}
}

type goalRunHelper struct {
	processor *Processor
}

func (h *goalRunHelper) Run(ctx context.Context, threadID string, userText string,
	scene string, env map[string]string,
	onProgress llm.ProgressFunc) (llm.RunResult, error) {

	return h.processor.RunGoalMessage(ctx, threadID, userText, scene, env, onProgress)
}
