package llm

import (
	"context"
	"strings"

	coreclaude "github.com/Alice-space/alice/internal/llm/claude"
	"github.com/Alice-space/alice/internal/prompting"
)

type claudeBackend struct {
	runner         coreclaude.Runner
	profileRunners map[string]coreclaude.Runner
}

func newClaudeBackend(cfg ClaudeConfig, prompts *prompting.Loader) *claudeBackend {
	defaultRunner := coreclaude.Runner{
		Command:      cfg.Command,
		Timeout:      cfg.Timeout,
		Env:          cfg.Env,
		PromptPrefix: cfg.PromptPrefix,
		WorkspaceDir: cfg.WorkspaceDir,
		Prompts:      prompts,
	}
	profileRunners := make(map[string]coreclaude.Runner, len(cfg.ProfileOverrides))
	for name, override := range cfg.ProfileOverrides {
		r := defaultRunner
		if strings.TrimSpace(override.Command) != "" {
			r.Command = strings.TrimSpace(override.Command)
		}
		if override.Timeout > 0 {
			r.Timeout = override.Timeout
		}
		if strings.TrimSpace(override.PromptPrefix) != "" {
			r.PromptPrefix = strings.TrimSpace(override.PromptPrefix)
		}
		profileRunners[name] = r
	}
	return &claudeBackend{runner: defaultRunner, profileRunners: profileRunners}
}

func (b *claudeBackend) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	runner := b.runner
	if profile := strings.TrimSpace(req.Profile); profile != "" {
		if r, ok := b.profileRunners[profile]; ok {
			runner = r
		}
	}
	reply, nextThreadID, err := runner.RunWithThreadAndProgress(
		ctx,
		strings.TrimSpace(req.ThreadID),
		strings.TrimSpace(req.AgentName),
		req.UserText,
		strings.TrimSpace(req.Model),
		strings.TrimSpace(req.Profile),
		strings.TrimSpace(req.Personality),
		strings.TrimSpace(req.NoReplyToken),
		strings.TrimSpace(req.PromptPrefix),
		req.Env,
		req.OnProgress,
	)
	return RunResult{
		Reply:        reply,
		NextThreadID: strings.TrimSpace(nextThreadID),
	}, err
}

var _ Backend = (*claudeBackend)(nil)
