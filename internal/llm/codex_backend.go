package llm

import (
	"context"
	"strings"

	corecodex "github.com/Alice-space/alice/internal/llm/codex"
	"github.com/Alice-space/alice/internal/prompting"
)

type codexBackend struct {
	runner            corecodex.Runner
	profileRunners    map[string]corecodex.Runner
	providerProfiles  map[string]string
	defaultExecPolicy corecodex.ExecPolicyConfig
	profilePolicies   map[string]corecodex.ExecPolicyConfig
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
	providerProfiles := make(map[string]string, len(cfg.ProfileOverrides))
	profilePolicies := make(map[string]corecodex.ExecPolicyConfig, len(cfg.ProfileOverrides))
	for name, override := range cfg.ProfileOverrides {
		providerProfiles[name] = strings.TrimSpace(override.ProviderProfile)
		profilePolicies[name] = toCoreCodexExecPolicy(override.ExecPolicy)
	}
	return &codexBackend{
		runner:            defaultRunner,
		profileRunners:    profileRunners,
		providerProfiles:  providerProfiles,
		defaultExecPolicy: toCoreCodexExecPolicy(cfg.DefaultExecPolicy),
		profilePolicies:   profilePolicies,
	}
}

func (b *codexBackend) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	runner := b.runner
	providerProfile := strings.TrimSpace(req.Profile)
	if profile := strings.TrimSpace(req.Profile); profile != "" {
		if r, ok := b.profileRunners[profile]; ok {
			runner = r
			if resolved := strings.TrimSpace(b.providerProfiles[profile]); resolved != "" {
				providerProfile = resolved
			}
		}
	}
	policy := b.resolveExecPolicy(req)
	reply, nextThreadID, usage, err := runner.RunWithThreadAndProgressAndUsage(
		ctx,
		strings.TrimSpace(req.ThreadID),
		strings.TrimSpace(req.AgentName),
		req.UserText,
		policy,
		strings.TrimSpace(req.PromptPrefix),
		strings.TrimSpace(req.Model),
		providerProfile,
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

func (b *codexBackend) resolveExecPolicy(req RunRequest) corecodex.ExecPolicyConfig {
	policy := b.defaultExecPolicy
	if profile := strings.TrimSpace(req.Profile); profile != "" {
		if profilePolicy, ok := b.profilePolicies[profile]; ok {
			policy = mergeCoreCodexExecPolicy(policy, profilePolicy)
		}
	}
	return mergeCoreCodexExecPolicy(policy, toCoreCodexExecPolicy(req.ExecPolicy))
}

func toCoreCodexExecPolicy(policy ExecPolicyConfig) corecodex.ExecPolicyConfig {
	return corecodex.ExecPolicyConfig{
		Sandbox:        strings.TrimSpace(policy.Sandbox),
		AskForApproval: strings.TrimSpace(policy.AskForApproval),
		AddDirs:        append([]string(nil), policy.AddDirs...),
	}
}

func mergeCoreCodexExecPolicy(base, override corecodex.ExecPolicyConfig) corecodex.ExecPolicyConfig {
	out := corecodex.ExecPolicyConfig{
		Sandbox:        strings.TrimSpace(base.Sandbox),
		AskForApproval: strings.TrimSpace(base.AskForApproval),
		AddDirs:        append([]string(nil), base.AddDirs...),
	}
	if sandbox := strings.TrimSpace(override.Sandbox); sandbox != "" {
		out.Sandbox = sandbox
	}
	if approval := strings.TrimSpace(override.AskForApproval); approval != "" {
		out.AskForApproval = approval
	}
	if len(override.AddDirs) > 0 {
		out.AddDirs = append(out.AddDirs, override.AddDirs...)
	}
	return out
}

var _ Backend = (*codexBackend)(nil)
