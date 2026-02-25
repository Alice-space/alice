package llm

import (
	"context"
	"strings"

	coreclaude "gitee.com/alicespace/alice/internal/llm/claude"
)

type claudeBackend struct {
	runner coreclaude.Runner
}

func newClaudeBackend(cfg ClaudeConfig) *claudeBackend {
	return &claudeBackend{
		runner: coreclaude.Runner{
			Command:      cfg.Command,
			Timeout:      cfg.Timeout,
			Env:          cfg.Env,
			PromptPrefix: cfg.PromptPrefix,
			WorkspaceDir: cfg.WorkspaceDir,
		},
	}
}

func (b *claudeBackend) Run(ctx context.Context, req RunRequest) (RunResult, error) {
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

var _ Backend = (*claudeBackend)(nil)
