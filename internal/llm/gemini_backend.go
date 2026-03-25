package llm

import (
	"context"
	"strings"

	coregemini "github.com/Alice-space/alice/internal/llm/gemini"
	"github.com/Alice-space/alice/internal/prompting"
)

type geminiBackend struct {
	runner         coregemini.Runner
	profileRunners map[string]coregemini.Runner
}

func newGeminiBackend(cfg GeminiConfig, prompts *prompting.Loader) *geminiBackend {
	defaultRunner := coregemini.Runner{
		Command:      cfg.Command,
		Timeout:      cfg.Timeout,
		Env:          cfg.Env,
		PromptPrefix: cfg.PromptPrefix,
		WorkspaceDir: cfg.WorkspaceDir,
		Prompts:      prompts,
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
		if strings.TrimSpace(override.PromptPrefix) != "" {
			r.PromptPrefix = strings.TrimSpace(override.PromptPrefix)
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
	reply, nextThreadID, err := runner.RunWithThreadAndProgress(
		ctx,
		strings.TrimSpace(req.ThreadID),
		strings.TrimSpace(req.AgentName),
		req.UserText,
		strings.TrimSpace(req.Model),
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

var _ Backend = (*geminiBackend)(nil)
