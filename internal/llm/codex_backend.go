package llm

import (
	"context"
	"strings"

	corecodex "gitee.com/alicespace/alice/internal/llm/codex"
)

type codexBackend struct {
	runner corecodex.Runner
}

func newCodexBackend(cfg CodexConfig) *codexBackend {
	return &codexBackend{
		runner: corecodex.Runner{
			Command:      cfg.Command,
			Timeout:      cfg.Timeout,
			Env:          cfg.Env,
			PromptPrefix: cfg.PromptPrefix,
			WorkspaceDir: cfg.WorkspaceDir,
		},
	}
}

func (b *codexBackend) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	reply, nextThreadID, err := b.runner.RunWithThreadAndProgress(
		ctx,
		strings.TrimSpace(req.ThreadID),
		req.UserText,
		strings.TrimSpace(req.Model),
		strings.TrimSpace(req.Profile),
		req.Env,
		req.OnProgress,
	)
	return RunResult{
		Reply:        reply,
		NextThreadID: strings.TrimSpace(nextThreadID),
	}, err
}

var _ Backend = (*codexBackend)(nil)
