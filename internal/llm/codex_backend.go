package llm

import (
	"context"
	"strings"

	corecodex "github.com/Alice-space/alice/internal/llm/codex"
	"github.com/Alice-space/alice/internal/prompting"
)

type codexBackend struct {
	runner         corecodex.Runner
	profileRunners map[string]corecodex.Runner
}

func newCodexBackend(cfg CodexConfig, prompts *prompting.Loader) *codexBackend {
	defaultRunner := corecodex.Runner{
		Command:                cfg.Command,
		Timeout:                cfg.Timeout,
		DefaultModel:           cfg.Model,
		DefaultReasoningEffort: cfg.ReasoningEffort,
		Env:                    cfg.Env,
		PromptPrefix:           cfg.PromptPrefix,
		WorkspaceDir:           cfg.WorkspaceDir,
		Prompts:                prompts,
	}
	profileRunners := make(map[string]corecodex.Runner, len(cfg.ProfileOverrides))
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
	return &codexBackend{runner: defaultRunner, profileRunners: profileRunners}
}

func (b *codexBackend) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	runner := b.runner
	if profile := strings.TrimSpace(req.Profile); profile != "" {
		if r, ok := b.profileRunners[profile]; ok {
			runner = r
		}
	}
	policy := corecodex.ExecPolicyConfig{
		Sandbox:        strings.TrimSpace(req.ExecPolicy.Sandbox),
		AskForApproval: strings.TrimSpace(req.ExecPolicy.AskForApproval),
		AddDirs:        append([]string(nil), req.ExecPolicy.AddDirs...),
	}
	reply, nextThreadID, usage, err := runner.RunWithThreadAndProgressAndUsage(
		ctx,
		strings.TrimSpace(req.ThreadID),
		strings.TrimSpace(req.AgentName),
		req.UserText,
		policy,
		strings.TrimSpace(req.PromptPrefix),
		strings.TrimSpace(req.Model),
		strings.TrimSpace(req.Profile),
		strings.TrimSpace(req.ReasoningEffort),
		strings.TrimSpace(req.Personality),
		strings.TrimSpace(req.NoReplyToken),
		req.Env,
		req.OnProgress,
	)
	return RunResult{
		Reply:        reply,
		NextThreadID: strings.TrimSpace(nextThreadID),
		Usage: Usage{
			InputTokens:       usage.InputTokens,
			CachedInputTokens: usage.CachedInputTokens,
			OutputTokens:      usage.OutputTokens,
		},
	}, err
}

var _ Backend = (*codexBackend)(nil)
