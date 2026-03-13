package llm

import (
	"context"
	"strings"

	corekimi "github.com/Alice-space/alice/internal/llm/kimi"
	"github.com/Alice-space/alice/internal/prompting"
)

type kimiBackend struct {
	runner corekimi.Runner
}

func newKimiBackend(cfg KimiConfig, prompts *prompting.Loader) *kimiBackend {
	return &kimiBackend{
		runner: corekimi.Runner{
			Command:      cfg.Command,
			Timeout:      cfg.Timeout,
			Env:          cfg.Env,
			PromptPrefix: cfg.PromptPrefix,
			WorkspaceDir: cfg.WorkspaceDir,
			Prompts:      prompts,
		},
	}
}

func (b *kimiBackend) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	reply, nextThreadID, err := b.runner.RunWithThreadAndProgress(
		ctx,
		strings.TrimSpace(req.ThreadID),
		strings.TrimSpace(req.AgentName),
		req.UserText,
		strings.TrimSpace(req.Model),
		req.Env,
		req.OnProgress,
	)
	return RunResult{
		Reply:        reply,
		NextThreadID: strings.TrimSpace(nextThreadID),
	}, err
}

var _ Backend = (*kimiBackend)(nil)
