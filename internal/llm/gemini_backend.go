package llm

import (
	"context"
	"strings"

	coregemini "github.com/Alice-space/alice/internal/llm/providers/gemini"
)

type geminiBackend struct {
	runner         coregemini.Runner
	profileRunners map[string]coregemini.Runner
}

func newGeminiBackend(cfg GeminiConfig) *geminiBackend {
	defaultRunner := coregemini.Runner{
		Command:      cfg.Command,
		Timeout:      cfg.Timeout,
		Env:          cfg.Env,
		WorkspaceDir: cfg.WorkspaceDir,
	}
	profileRunners := make(map[string]coregemini.Runner, len(cfg.ProfileOverrides))
	for name, override := range cfg.ProfileOverrides {
		r := defaultRunner
		if strings.TrimSpace(override.Command) != "" {
			r.Command = strings.TrimSpace(override.Command)
		}
		if override.Timeout > 0 {
			r.Timeout = override.Timeout
		}
		profileRunners[name] = r
	}
	return &geminiBackend{runner: defaultRunner, profileRunners: profileRunners}
}

func (b *geminiBackend) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	runner := b.runner
	if profile := strings.TrimSpace(req.Profile); profile != "" {
		if r, ok := b.profileRunners[profile]; ok {
			runner = r
		}
	}
	if strings.TrimSpace(req.WorkspaceDir) != "" {
		runner.WorkspaceDir = strings.TrimSpace(req.WorkspaceDir)
	}
	reply, nextThreadID, inputTokens, cachedInputTokens, outputTokens, err := runner.RunWithThreadAndProgress(
		ctx,
		strings.TrimSpace(req.ThreadID),
		req.UserText,
		strings.TrimSpace(req.Model),
		req.Env,
		req.OnProgress,
	)
	return RunResult{
		Reply:        reply,
		NextThreadID: strings.TrimSpace(nextThreadID),
		Usage: Usage{
			InputTokens:       inputTokens,
			CachedInputTokens: cachedInputTokens,
			OutputTokens:      outputTokens,
		},
	}, err
}

var _ Backend = (*geminiBackend)(nil)
