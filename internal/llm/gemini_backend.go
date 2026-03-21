package llm

import (
	"context"
	"strings"

	coregemini "github.com/Alice-space/alice/internal/llm/gemini"
	"github.com/Alice-space/alice/internal/prompting"
)

type geminiBackend struct {
	runner coregemini.Runner
}

func newGeminiBackend(cfg GeminiConfig, prompts *prompting.Loader) *geminiBackend {
	return &geminiBackend{
		runner: coregemini.Runner{
			Command:      cfg.Command,
			Timeout:      cfg.Timeout,
			Env:          cfg.Env,
			PromptPrefix: cfg.PromptPrefix,
			WorkspaceDir: cfg.WorkspaceDir,
			Prompts:      prompts,
		},
	}
}

func (b *geminiBackend) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	reply, nextThreadID, err := b.runner.RunWithThreadAndProgress(
		ctx,
		strings.TrimSpace(req.ThreadID),
		strings.TrimSpace(req.AgentName),
		req.UserText,
		strings.TrimSpace(req.Model),
		strings.TrimSpace(req.Personality),
		strings.TrimSpace(req.NoReplyToken),
		req.Env,
		req.OnProgress,
	)
	return RunResult{
		Reply:        reply,
		NextThreadID: strings.TrimSpace(nextThreadID),
	}, err
}

var _ Backend = (*geminiBackend)(nil)
