package automation

import (
	"context"
	"strings"

	llm "github.com/Alice-space/alice/internal/llm"
	"github.com/Alice-space/alice/internal/logging"
)

// GoalRunHelper runs goal prompts through the same LLM pipeline used for
// regular user messages, providing full OnProgress and OnRawEvent dispatching
// so that the agent's tool calls, reasoning, and progress are properly logged
// and forwarded to Feishu.
type GoalRunHelper interface {
	Run(ctx context.Context, threadID string, userText string,
		scene string, env map[string]string, workspaceDir string,
		onProgress llm.ProgressFunc) (llm.RunResult, error)
}

// goalRawEventDispatcher creates a fallback OnRawEvent handler for goals when
// GoalRunHelper is not available. It logs reasoning, tool_use, and tool_call
// events to the debug log so that agent activity is at least partially
// traceable.
func goalRawEventDispatcher(goal GoalTask) llm.RawEventFunc {
	scope := string(goal.Scope.Kind) + ":" + goal.Scope.ID
	return func(event llm.RawEvent) {
		if !logging.IsDebugEnabled() {
			return
		}
		switch event.Kind {
		case "reasoning":
			logging.Debugf("goal reasoning scope=%s detail=%q", scope, clipGoalDetail(event.Detail, 500))
		case "tool_use":
			logging.Debugf("goal tool_use scope=%s detail=%q", scope, clipGoalDetail(event.Detail, 500))
		case "tool_call":
			logging.Debugf("goal tool_call scope=%s detail=%q", scope, clipGoalDetail(event.Detail, 500))
		}
	}
}

func clipGoalDetail(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
