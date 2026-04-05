package bootstrap

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/oklog/run"

	agentbridge "github.com/Alice-space/agentbridge"
	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/config"
	"github.com/Alice-space/alice/internal/connector"
	"github.com/Alice-space/alice/internal/prompting"
	"github.com/Alice-space/alice/internal/runtimeapi"
)

type ConnectorRuntime struct {
	App                 *connector.App
	Processor           *connector.Processor
	AutomationEngine    *automation.Engine
	RuntimeAPI          *runtimeapi.Server
	RuntimeAPIBaseURL   string
	RuntimeAPIToken     string
	AutomationStatePath string
	SessionStatePath    string
	PromptLoader        *prompting.Loader
	Config              config.Config
	mu                  sync.Mutex
}

// buildFactoryConfig derives per-provider FactoryConfig from llm_profiles.
// For each provider, the first profile (alphabetically by name) that matches
// provides the provider-level command, timeout, and default model/reasoning_effort/prompt_prefix.
// All profiles are also stored as per-profile runner overrides so that selecting a specific
// profile by its outer map name applies that profile's command, timeout, prompt_prefix,
// and provider-specific profile selector.
func buildFactoryConfig(cfg config.Config) agentbridge.FactoryConfig {
	defaultEnv := applyLLMProcessEnvDefaults(cfg.CodexEnv, cfg.CodexHome)

	type providerDefaults struct {
		command         string
		timeout         time.Duration
		model           string
		reasoningEffort string
		execPolicy      agentbridge.ExecPolicyConfig
	}
	defaults := map[string]*providerDefaults{}

	// Per-provider per-profile overrides: profileOverrides[provider][outerProfileName] = override.
	profileOverrides := map[string]map[string]agentbridge.ProfileRunnerConfig{}

	// Collect sorted profile names for deterministic first-profile selection.
	profileNames := make([]string, 0, len(cfg.LLMProfiles))
	for name := range cfg.LLMProfiles {
		profileNames = append(profileNames, name)
	}
	sort.Strings(profileNames)

	for _, name := range profileNames {
		profile := cfg.LLMProfiles[name]
		provider := strings.ToLower(strings.TrimSpace(profile.Provider))
		if provider == "" {
			provider = config.DefaultLLMProvider
		}
		if _, exists := defaults[provider]; !exists {
			defaults[provider] = &providerDefaults{
				command:         profile.Command,
				timeout:         profile.Timeout,
				model:           profile.Model,
				reasoningEffort: profile.ReasoningEffort,
				execPolicy:      buildCodexExecPolicy(derefCodexExecPolicy(profile.Permissions)),
			}
		}
		// Register per-profile runner override keyed by outer profile name.
		if _, ok := profileOverrides[provider]; !ok {
			profileOverrides[provider] = map[string]agentbridge.ProfileRunnerConfig{}
		}
		profileOverrides[provider][name] = agentbridge.ProfileRunnerConfig{
			Command:         profile.Command,
			Timeout:         profile.Timeout,
			ProviderProfile: profile.Profile,
			ExecPolicy:      buildCodexExecPolicy(derefCodexExecPolicy(profile.Permissions)),
		}
	}

	get := func(provider, fallbackCmd string) providerDefaults {
		if d, ok := defaults[provider]; ok {
			return *d
		}
		return providerDefaults{
			command: fallbackCmd,
			timeout: time.Duration(config.DefaultLLMTimeoutSecs) * time.Second,
		}
	}

	getOverrides := func(provider string) map[string]agentbridge.ProfileRunnerConfig {
		if m, ok := profileOverrides[provider]; ok {
			return m
		}
		return nil
	}

	codex := get(config.DefaultLLMProvider, "codex")
	claude := get(config.LLMProviderClaude, "claude")
	gemini := get(config.LLMProviderGemini, "gemini")
	kimi := get(config.LLMProviderKimi, "kimi")

	return agentbridge.FactoryConfig{
		Provider: cfg.LLMProvider,
		Codex: agentbridge.CodexConfig{
			Command:            codex.command,
			Timeout:            codex.timeout,
			DefaultIdleTimeout: cfg.CodexIdleTimeout,
			HighIdleTimeout:    cfg.CodexHighIdleTimeout,
			XHighIdleTimeout:   cfg.CodexXHighIdleTimeout,
			Model:              codex.model,
			ReasoningEffort:    codex.reasoningEffort,
			Env:                defaultEnv,
			WorkspaceDir:       cfg.WorkspaceDir,
			DefaultExecPolicy:  codex.execPolicy,
			ProfileOverrides:   getOverrides(config.DefaultLLMProvider),
		},
		Claude: agentbridge.ClaudeConfig{
			Command:          claude.command,
			Timeout:          claude.timeout,
			Env:              defaultEnv,
			WorkspaceDir:     cfg.WorkspaceDir,
			ProfileOverrides: getOverrides(config.LLMProviderClaude),
		},
		Gemini: agentbridge.GeminiConfig{
			Command:          gemini.command,
			Timeout:          gemini.timeout,
			Env:              defaultEnv,
			WorkspaceDir:     cfg.WorkspaceDir,
			ProfileOverrides: getOverrides(config.LLMProviderGemini),
		},
		Kimi: agentbridge.KimiConfig{
			Command:          kimi.command,
			Timeout:          kimi.timeout,
			Env:              defaultEnv,
			WorkspaceDir:     cfg.WorkspaceDir,
			ProfileOverrides: getOverrides(config.LLMProviderKimi),
		},
	}
}

func buildLLMBackend(cfg config.Config) (agentbridge.Backend, error) {
	factoryCfg := buildFactoryConfig(cfg)
	providers := cfg.ResolvedLLMProviders()
	backends := make(map[string]agentbridge.Backend, len(providers))
	for _, provider := range providers {
		providerCfg := factoryCfg
		providerCfg.Provider = provider
		backend, err := agentbridge.NewBackend(providerCfg)
		if err != nil {
			return nil, err
		}
		backends[provider] = backend
	}
	return agentbridge.NewMultiBackend(cfg.LLMProvider, backends)
}

func buildCodexExecPolicy(policy config.CodexExecPolicyConfig) agentbridge.ExecPolicyConfig {
	return agentbridge.ExecPolicyConfig{
		Sandbox:        strings.TrimSpace(policy.Sandbox),
		AskForApproval: strings.TrimSpace(policy.AskForApproval),
		AddDirs:        append([]string(nil), policy.AddDirs...),
	}
}

func derefCodexExecPolicy(policy *config.CodexExecPolicyConfig) config.CodexExecPolicyConfig {
	if policy == nil {
		return config.CodexExecPolicyConfig{}
	}
	return *policy
}

func applyLLMProcessEnvDefaults(raw map[string]string, codexHome string) map[string]string {
	out := make(map[string]string, len(raw)+1)
	for key, value := range raw {
		trimmedKey := strings.TrimSpace(key)
		trimmedValue := strings.TrimSpace(value)
		if trimmedKey == "" || trimmedValue == "" {
			continue
		}
		out[trimmedKey] = trimmedValue
	}
	if strings.TrimSpace(out[config.EnvCodexHome]) == "" {
		if strings.TrimSpace(codexHome) != "" {
			out[config.EnvCodexHome] = strings.TrimSpace(codexHome)
		} else {
			out[config.EnvCodexHome] = config.DefaultCodexHome()
		}
	}
	return out
}

func NewLLMBackend(cfg config.Config) (agentbridge.Backend, error) {
	return buildLLMBackend(cfg)
}

func BuildConnectorRuntime(cfg config.Config, backend agentbridge.Backend) (*ConnectorRuntime, error) {
	builder, err := newConnectorRuntimeBuilder(cfg, backend)
	if err != nil {
		return nil, err
	}
	return builder.Build()
}

func (r *ConnectorRuntime) Run(ctx context.Context) error {
	return r.run(ctx, false)
}

func (r *ConnectorRuntime) RunRuntimeOnly(ctx context.Context) error {
	return r.run(ctx, true)
}

func (r *ConnectorRuntime) run(ctx context.Context, runtimeOnly bool) error {
	if r == nil || r.App == nil {
		return errors.New("connector runtime is nil")
	}

	var group run.Group
	appCtx, cancelApp := context.WithCancel(ctx)
	group.Add(func() error {
		if runtimeOnly {
			return r.App.RunWithoutConnector(appCtx)
		}
		return r.App.Run(appCtx)
	}, func(error) {
		cancelApp()
	})
	if r.RuntimeAPI != nil {
		apiCtx, cancelAPI := context.WithCancel(ctx)
		group.Add(func() error {
			return r.RuntimeAPI.Run(apiCtx)
		}, func(error) {
			cancelAPI()
		})
	}
	return group.Run()
}
