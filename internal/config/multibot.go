package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

const (
	CodexSandboxReadOnly         = "read-only"
	CodexSandboxWorkspaceWrite   = "workspace-write"
	CodexSandboxDangerFullAccess = "danger-full-access"

	CodexApprovalUntrusted = "untrusted"
	CodexApprovalOnRequest = "on-request"
	CodexApprovalNever     = "never"
)

type bundledSkillSpec struct {
	Name    string
	Allowed func(Config) bool
}

var defaultBundledSkills = []bundledSkillSpec{
	{Name: "alice-code-army", Allowed: allowRuntimeCampaignSkill},
	{Name: "alice-message", Allowed: allowRuntimeMessageSkill},
	{Name: "alice-scheduler", Allowed: allowRuntimeAutomationSkill},
	{Name: "file-printing"},
}

func allowRuntimeMessageSkill(cfg Config) bool {
	return cfg.Permissions.RuntimeMessage == nil || *cfg.Permissions.RuntimeMessage
}

func allowRuntimeAutomationSkill(cfg Config) bool {
	return cfg.Permissions.RuntimeAutomation == nil || *cfg.Permissions.RuntimeAutomation
}

func allowRuntimeCampaignSkill(cfg Config) bool {
	return cfg.Permissions.RuntimeCampaigns == nil || *cfg.Permissions.RuntimeCampaigns
}

func finalizeConfig(cfg Config, requireCredentials bool) (Config, error) {
	resolvedProvider, err := resolveLLMProvider(cfg)
	if err != nil {
		return Config{}, err
	}
	cfg.LLMProvider = resolvedProvider

	if err := validateBaseConfig(cfg, requireCredentials); err != nil {
		return Config{}, err
	}
	if cfg.FeishuBaseURL == "" {
		cfg.FeishuBaseURL = "https://open.feishu.cn"
	}
	if cfg.TriggerMode == "" {
		cfg.TriggerMode = TriggerModeAt
	}
	if cfg.ImmediateFeedbackMode == "" {
		cfg.ImmediateFeedbackMode = DefaultImmediateFeedbackMode
	}
	if cfg.ImmediateFeedbackReaction == "" {
		cfg.ImmediateFeedbackReaction = DefaultImmediateFeedbackReaction
	}
	cfg.CodexEnv = applyDefaultCodexEnv(cfg.CodexEnv)
	if cfg.RuntimeHTTPAddr == "" {
		cfg.RuntimeHTTPAddr = DefaultRuntimeHTTPAddr
	}
	if cfg.AliceHome == "" {
		cfg.AliceHome = AliceHomeDir()
	} else {
		cfg.AliceHome = ResolveAliceHomeDir(cfg.AliceHome)
	}
	if cfg.WorkspaceDir == "" {
		cfg.WorkspaceDir = WorkspaceDirForAliceHome(cfg.AliceHome)
	} else {
		cfg.WorkspaceDir = normalizeHomePath(cfg.WorkspaceDir)
	}
	if cfg.PromptDir == "" {
		cfg.PromptDir = PromptDirForAliceHome(cfg.AliceHome)
	} else {
		cfg.PromptDir = normalizeHomePath(cfg.PromptDir)
	}
	cfg.CodexHome = ResolveCodexHomeDir(cfg.CodexHome)
	if cfg.SoulPath == "" {
		cfg.SoulPath = filepath.Join(cfg.WorkspaceDir, "SOUL.md")
	} else if !filepath.IsAbs(cfg.SoulPath) {
		cfg.SoulPath = filepath.Join(cfg.WorkspaceDir, cfg.SoulPath)
	}
	cfg.SoulPath = filepath.Clean(cfg.SoulPath)
	if cfg.LogFile == "" {
		cfg.LogFile = LogFilePathForAliceHome(cfg.AliceHome)
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}
	if cfg.FailureMessage == "" {
		cfg.FailureMessage = "Codex 暂时不可用，请稍后重试。"
	}
	if cfg.ThinkingMessage == "" {
		cfg.ThinkingMessage = "正在思考中..."
	}
	cfg.ImageGeneration = normalizeImageGenerationConfig(cfg.ImageGeneration)
	if cfg.ImageGeneration.Provider == "" {
		cfg.ImageGeneration.Provider = "openai"
	}
	if cfg.ImageGeneration.Model == "" {
		cfg.ImageGeneration.Model = "gpt-image-1.5"
	}
	if cfg.ImageGeneration.TimeoutSecs <= 0 {
		cfg.ImageGeneration.TimeoutSecs = 120
	}
	if cfg.ImageGeneration.Size == "" {
		cfg.ImageGeneration.Size = "1024x1536"
	}
	if cfg.ImageGeneration.Quality == "" {
		cfg.ImageGeneration.Quality = "high"
	}
	if cfg.ImageGeneration.Background == "" {
		cfg.ImageGeneration.Background = "auto"
	}
	if cfg.ImageGeneration.OutputFormat == "" {
		cfg.ImageGeneration.OutputFormat = "png"
	}
	if cfg.ImageGeneration.InputFidelity == "" {
		cfg.ImageGeneration.InputFidelity = "high"
	}
	if cfg.BotName == "" {
		switch {
		case strings.TrimSpace(cfg.BotID) != "":
			cfg.BotName = strings.TrimSpace(cfg.BotID)
		default:
			cfg.BotName = "Alice"
		}
	}

	switch cfg.TriggerMode {
	case TriggerModeAt, TriggerModePrefix:
	default:
		return Config{}, fmt.Errorf("unsupported trigger_mode %q", cfg.TriggerMode)
	}
	switch cfg.ImmediateFeedbackMode {
	case ImmediateFeedbackModeReply, ImmediateFeedbackModeReaction:
	default:
		return Config{}, fmt.Errorf("unsupported immediate_feedback_mode %q", cfg.ImmediateFeedbackMode)
	}
	if cfg.TriggerMode == TriggerModePrefix && cfg.TriggerPrefix == "" {
		return Config{}, errors.New("trigger_prefix is required when trigger_mode is prefix")
	}

	for key := range cfg.CodexEnv {
		if key == "" {
			return Config{}, errors.New("env key must not be empty")
		}
		if strings.ContainsRune(key, '=') {
			return Config{}, fmt.Errorf("env key %q must not contain '='", key)
		}
	}
	if cfg.LogMaxSizeMB <= 0 {
		cfg.LogMaxSizeMB = 20
	}
	if cfg.LogMaxBackups <= 0 {
		cfg.LogMaxBackups = 5
	}
	if cfg.LogMaxAgeDays <= 0 {
		cfg.LogMaxAgeDays = 7
	}
	cfg.Permissions = normalizeBotPermissions(cfg.Permissions)
	if err := validateBotPermissions(cfg.Permissions); err != nil {
		return Config{}, err
	}
	cfg.LLMProfiles = finalizeLLMProfiles(cfg.LLMProfiles)
	cfg.AutomationTaskTimeout = time.Duration(cfg.AutomationTaskTimeoutSecs) * time.Second
	cfg.AuthStatusTimeout = time.Duration(cfg.AuthStatusTimeoutSecs) * time.Second
	cfg.CampaignNotificationTimeout = time.Duration(cfg.CampaignNotificationTimeoutSecs) * time.Second
	cfg.RuntimeAPIShutdownTimeout = time.Duration(cfg.RuntimeAPIShutdownTimeoutSecs) * time.Second
	cfg.LocalRuntimeStoreOpenTimeout = time.Duration(cfg.LocalRuntimeStoreOpenTimeoutSecs) * time.Second
	cfg.CodexIdleTimeout = time.Duration(cfg.CodexIdleTimeoutSecs) * time.Second
	cfg.CodexHighIdleTimeout = time.Duration(cfg.CodexHighIdleTimeoutSecs) * time.Second
	cfg.CodexXHighIdleTimeout = time.Duration(cfg.CodexXHighIdleTimeoutSecs) * time.Second

	if len(cfg.Bots) == 0 {
		if strings.TrimSpace(cfg.BotID) == "" {
			return Config{}, errors.New("bots is required")
		}
		if err := validateSceneConfig(cfg); err != nil {
			return Config{}, err
		}
		return cfg, nil
	}
	if _, err := cfg.RuntimeConfigs(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func finalizeLLMProfiles(in map[string]LLMProfileConfig) map[string]LLMProfileConfig {
	if len(in) == 0 {
		return in
	}
	out := make(map[string]LLMProfileConfig, len(in))
	for name, profile := range in {
		defaultSandbox := defaultSandboxForProvider(profile.Provider)
		if profile.Command == "" {
			switch profile.Provider {
			case LLMProviderClaude:
				profile.Command = "claude"
			case LLMProviderGemini:
				profile.Command = "gemini"
			case LLMProviderKimi:
				profile.Command = "kimi"
			default:
				profile.Command = "codex"
			}
		}
		if profile.TimeoutSecs <= 0 {
			profile.TimeoutSecs = DefaultLLMTimeoutSecs
		}
		profile.Timeout = time.Duration(profile.TimeoutSecs) * time.Second
		if profile.Permissions == nil {
			profile.Permissions = &CodexExecPolicyConfig{
				Sandbox:        defaultSandbox,
				AskForApproval: CodexApprovalNever,
			}
		} else {
			if profile.Permissions.Sandbox == "" {
				profile.Permissions.Sandbox = defaultSandbox
			}
			if profile.Permissions.AskForApproval == "" {
				profile.Permissions.AskForApproval = CodexApprovalNever
			}
		}
		out[name] = profile
	}
	return out
}

func defaultSandboxForProvider(provider string) string {
	switch normalizeLLMProvider(provider) {
	case LLMProviderClaude, LLMProviderKimi:
		return CodexSandboxDangerFullAccess
	default:
		return CodexSandboxWorkspaceWrite
	}
}

func (cfg Config) AllowedBundledSkills() []string {
	if len(cfg.Permissions.AllowedSkills) > 0 {
		return append([]string(nil), cfg.Permissions.AllowedSkills...)
	}
	allowed := make([]string, 0, len(defaultBundledSkills))
	for _, skill := range defaultBundledSkills {
		if skill.Allowed != nil && !skill.Allowed(cfg) {
			continue
		}
		allowed = append(allowed, skill.Name)
	}
	return allowed
}
