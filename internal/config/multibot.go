package config

import (
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"sort"
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

var defaultBundledSkills = []string{
	"alice-code-army",
	"alice-memory",
	"alice-message",
	"alice-scheduler",
	"feishu-task",
	"file-printing",
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
	if cfg.CodexCommand == "" {
		cfg.CodexCommand = "codex"
	}
	if cfg.ClaudeCommand == "" {
		cfg.ClaudeCommand = "claude"
	}
	if cfg.GeminiCommand == "" {
		cfg.GeminiCommand = "gemini"
	}
	if cfg.KimiCommand == "" {
		cfg.KimiCommand = "kimi"
	}
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
	if cfg.CodexHome == "" {
		cfg.CodexHome = CodexHomeForAliceHome(cfg.AliceHome)
	} else {
		cfg.CodexHome = normalizeHomePath(cfg.CodexHome)
	}
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

	if cfg.CodexTimeoutSecs <= 0 {
		if cfg.LLMProvider == DefaultLLMProvider {
			return Config{}, errors.New("codex_timeout_secs must be > 0")
		}
		cfg.CodexTimeoutSecs = 172800
	}
	if cfg.ClaudeTimeoutSecs <= 0 {
		if cfg.LLMProvider == LLMProviderClaude {
			return Config{}, errors.New("claude_timeout_secs must be > 0")
		}
		cfg.ClaudeTimeoutSecs = 172800
	}
	if cfg.GeminiTimeoutSecs <= 0 {
		if cfg.LLMProvider == LLMProviderGemini {
			return Config{}, errors.New("gemini_timeout_secs must be > 0")
		}
		cfg.GeminiTimeoutSecs = 172800
	}
	if cfg.KimiTimeoutSecs <= 0 {
		if cfg.LLMProvider == LLMProviderKimi {
			return Config{}, errors.New("kimi_timeout_secs must be > 0")
		}
		cfg.KimiTimeoutSecs = 172800
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
	cfg.CodexTimeout = time.Duration(cfg.CodexTimeoutSecs) * time.Second
	cfg.ClaudeTimeout = time.Duration(cfg.ClaudeTimeoutSecs) * time.Second
	cfg.GeminiTimeout = time.Duration(cfg.GeminiTimeoutSecs) * time.Second
	cfg.KimiTimeout = time.Duration(cfg.KimiTimeoutSecs) * time.Second
	cfg.AutomationTaskTimeout = time.Duration(cfg.AutomationTaskTimeoutSecs) * time.Second

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

func validateSceneConfig(cfg Config) error {
	for name, profile := range cfg.LLMProfiles {
		switch profile.Provider {
		case "", DefaultLLMProvider, LLMProviderClaude, LLMProviderGemini, LLMProviderKimi:
		default:
			return fmt.Errorf("llm_profiles.%s.provider %q is unsupported", name, profile.Provider)
		}
	}
	if cfg.GroupScenes.Chat.Enabled {
		if cfg.GroupScenes.Chat.LLMProfile == "" {
			return errors.New("group_scenes.chat.llm_profile is required when chat scene is enabled")
		}
		if _, ok := cfg.LLMProfiles[cfg.GroupScenes.Chat.LLMProfile]; !ok {
			return fmt.Errorf("group_scenes.chat.llm_profile %q is undefined", cfg.GroupScenes.Chat.LLMProfile)
		}
		if cfg.GroupScenes.Chat.SessionScope != GroupSceneSessionPerChat {
			return fmt.Errorf("group_scenes.chat.session_scope must be %q", GroupSceneSessionPerChat)
		}
	}
	if cfg.GroupScenes.Work.Enabled {
		if cfg.GroupScenes.Work.LLMProfile == "" {
			return errors.New("group_scenes.work.llm_profile is required when work scene is enabled")
		}
		if cfg.GroupScenes.Work.TriggerTag == "" {
			return errors.New("group_scenes.work.trigger_tag is required when work scene is enabled")
		}
		if _, ok := cfg.LLMProfiles[cfg.GroupScenes.Work.LLMProfile]; !ok {
			return fmt.Errorf("group_scenes.work.llm_profile %q is undefined", cfg.GroupScenes.Work.LLMProfile)
		}
		if cfg.GroupScenes.Work.SessionScope != GroupSceneSessionPerThread {
			return fmt.Errorf("group_scenes.work.session_scope must be %q", GroupSceneSessionPerThread)
		}
	}
	return nil
}

func normalizeBots(in map[string]BotConfig) map[string]BotConfig {
	if len(in) == 0 {
		return map[string]BotConfig{}
	}
	out := make(map[string]BotConfig, len(in))
	for rawID, bot := range in {
		id := strings.ToLower(strings.TrimSpace(rawID))
		if id == "" {
			continue
		}
		bot.Name = strings.TrimSpace(bot.Name)
		bot.FeishuAppID = strings.TrimSpace(bot.FeishuAppID)
		bot.FeishuAppSecret = strings.TrimSpace(bot.FeishuAppSecret)
		bot.FeishuBaseURL = strings.TrimSpace(bot.FeishuBaseURL)
		bot.FeishuBotOpenID = strings.TrimSpace(bot.FeishuBotOpenID)
		bot.FeishuBotUserID = strings.TrimSpace(bot.FeishuBotUserID)
		bot.TriggerMode = strings.ToLower(strings.TrimSpace(bot.TriggerMode))
		bot.TriggerPrefix = strings.TrimSpace(bot.TriggerPrefix)
		bot.ImmediateFeedbackMode = strings.ToLower(strings.TrimSpace(bot.ImmediateFeedbackMode))
		bot.ImmediateFeedbackReaction = strings.ToUpper(strings.TrimSpace(bot.ImmediateFeedbackReaction))
		bot.LLMProvider = strings.ToLower(strings.TrimSpace(bot.LLMProvider))
		bot.LLMProfiles = normalizeLLMProfiles(bot.LLMProfiles)
		if bot.GroupScenes != nil {
			normalized := normalizeGroupScenes(*bot.GroupScenes)
			bot.GroupScenes = &normalized
		}
		bot.CodexCommand = strings.TrimSpace(bot.CodexCommand)
		bot.CodexModel = strings.TrimSpace(bot.CodexModel)
		bot.CodexReasoningEffort = strings.ToLower(strings.TrimSpace(bot.CodexReasoningEffort))
		bot.CodexPromptPrefix = strings.TrimSpace(bot.CodexPromptPrefix)
		bot.ClaudeCommand = strings.TrimSpace(bot.ClaudeCommand)
		bot.ClaudePromptPrefix = strings.TrimSpace(bot.ClaudePromptPrefix)
		bot.GeminiCommand = strings.TrimSpace(bot.GeminiCommand)
		bot.GeminiPromptPrefix = strings.TrimSpace(bot.GeminiPromptPrefix)
		bot.KimiCommand = strings.TrimSpace(bot.KimiCommand)
		bot.KimiPromptPrefix = strings.TrimSpace(bot.KimiPromptPrefix)
		bot.RuntimeHTTPAddr = strings.TrimSpace(bot.RuntimeHTTPAddr)
		bot.RuntimeHTTPToken = strings.TrimSpace(bot.RuntimeHTTPToken)
		bot.FailureMessage = strings.TrimSpace(bot.FailureMessage)
		bot.ThinkingMessage = strings.TrimSpace(bot.ThinkingMessage)
		bot.ImageGeneration = normalizeImageGenerationConfig(bot.ImageGeneration)
		bot.AliceHome = strings.TrimSpace(bot.AliceHome)
		bot.WorkspaceDir = strings.TrimSpace(bot.WorkspaceDir)
		bot.PromptDir = strings.TrimSpace(bot.PromptDir)
		bot.CodexHome = strings.TrimSpace(bot.CodexHome)
		bot.SoulPath = strings.TrimSpace(bot.SoulPath)
		bot.CodexEnv = normalizeEnvMap(bot.CodexEnv)
		if bot.Permissions != nil {
			normalized := normalizeBotPermissions(*bot.Permissions)
			bot.Permissions = &normalized
		}
		out[id] = bot
	}
	return out
}

func normalizeBotPermissions(in BotPermissionsConfig) BotPermissionsConfig {
	if in.RuntimeMessage == nil {
		in.RuntimeMessage = boolPtr(true)
	}
	if in.RuntimeAutomation == nil {
		in.RuntimeAutomation = boolPtr(true)
	}
	if in.RuntimeCampaigns == nil {
		in.RuntimeCampaigns = boolPtr(true)
	}
	in.AllowedSkills = normalizeStringSlice(in.AllowedSkills)
	in.Codex.Chat = normalizeCodexExecPolicy(in.Codex.Chat)
	in.Codex.Work = normalizeCodexExecPolicy(in.Codex.Work)
	if in.Codex.Chat.Sandbox == "" {
		in.Codex.Chat.Sandbox = CodexSandboxWorkspaceWrite
	}
	if in.Codex.Chat.AskForApproval == "" {
		in.Codex.Chat.AskForApproval = CodexApprovalNever
	}
	if in.Codex.Work.Sandbox == "" {
		in.Codex.Work.Sandbox = CodexSandboxDangerFullAccess
	}
	if in.Codex.Work.AskForApproval == "" {
		in.Codex.Work.AskForApproval = CodexApprovalNever
	}
	return in
}

func normalizeCodexExecPolicy(in CodexExecPolicyConfig) CodexExecPolicyConfig {
	in.Sandbox = strings.ToLower(strings.TrimSpace(in.Sandbox))
	in.AskForApproval = strings.ToLower(strings.TrimSpace(in.AskForApproval))
	in.AddDirs = normalizePathSlice(in.AddDirs)
	return in
}

func normalizeStringSlice(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, raw := range in {
		item := strings.ToLower(strings.TrimSpace(raw))
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func normalizePathSlice(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, raw := range in {
		item := strings.TrimSpace(raw)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func validateBotPermissions(cfg BotPermissionsConfig) error {
	if err := validateCodexExecPolicy(cfg.Codex.Chat, "permissions.codex.chat"); err != nil {
		return err
	}
	if err := validateCodexExecPolicy(cfg.Codex.Work, "permissions.codex.work"); err != nil {
		return err
	}
	return nil
}

func validateCodexExecPolicy(policy CodexExecPolicyConfig, field string) error {
	switch policy.Sandbox {
	case "", CodexSandboxReadOnly, CodexSandboxWorkspaceWrite, CodexSandboxDangerFullAccess:
	default:
		return fmt.Errorf("%s.sandbox %q is unsupported", field, policy.Sandbox)
	}
	switch policy.AskForApproval {
	case "", CodexApprovalUntrusted, CodexApprovalOnRequest, CodexApprovalNever:
	default:
		return fmt.Errorf("%s.ask_for_approval %q is unsupported", field, policy.AskForApproval)
	}
	return nil
}

func (cfg Config) RuntimeConfigs() ([]Config, error) {
	if len(cfg.Bots) == 0 {
		if strings.TrimSpace(cfg.BotID) == "" {
			return nil, errors.New("bots is required")
		}
		if err := validateSceneConfig(cfg); err != nil {
			return nil, err
		}
		return []Config{cfg}, nil
	}

	ordered := orderBotIDs(cfg.Bots)
	runtimes := make([]Config, 0, len(ordered))
	for idx, botID := range ordered {
		runtime, err := cfg.deriveBotRuntimeConfig(botID, cfg.Bots[botID], idx)
		if err != nil {
			return nil, err
		}
		runtimes = append(runtimes, runtime)
	}
	return runtimes, nil
}

func (cfg Config) RuntimeConfigForBot(botID string) (Config, error) {
	botID = strings.ToLower(strings.TrimSpace(botID))
	if botID == "" {
		return Config{}, errors.New("bot id is empty")
	}
	if len(cfg.Bots) == 0 {
		if strings.EqualFold(strings.TrimSpace(cfg.BotID), botID) {
			return cfg, nil
		}
		return Config{}, fmt.Errorf("bot %q is undefined", botID)
	}
	bot, ok := cfg.Bots[botID]
	if !ok {
		return Config{}, fmt.Errorf("bot %q is undefined", botID)
	}
	ordered := orderBotIDs(cfg.Bots)
	for idx, orderedID := range ordered {
		if orderedID != botID {
			continue
		}
		return cfg.deriveBotRuntimeConfig(botID, bot, idx)
	}
	return Config{}, fmt.Errorf("bot %q is undefined", botID)
}

func orderBotIDs(bots map[string]BotConfig) []string {
	ids := make([]string, 0, len(bots))
	for id := range bots {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (cfg Config) deriveBotRuntimeConfig(botID string, bot BotConfig, index int) (Config, error) {
	runtime := Config{
		BotID:           strings.TrimSpace(botID),
		LogLevel:        cfg.LogLevel,
		LogFile:         cfg.LogFile,
		LogMaxSizeMB:    cfg.LogMaxSizeMB,
		LogMaxBackups:   cfg.LogMaxBackups,
		LogMaxAgeDays:   cfg.LogMaxAgeDays,
		LogCompress:     cfg.LogCompress,
		CodexEnv:        map[string]string{},
		LLMProfiles:     map[string]LLMProfileConfig{},
		Permissions:     normalizeBotPermissions(BotPermissionsConfig{}),
		RuntimeHTTPAddr: "",
	}
	if bot.Name != "" {
		runtime.BotName = bot.Name
	} else {
		runtime.BotName = runtime.BotID
	}

	runtime.FeishuAppID = bot.FeishuAppID
	runtime.FeishuAppSecret = bot.FeishuAppSecret
	runtime.FeishuBaseURL = bot.FeishuBaseURL
	runtime.FeishuBotOpenID = bot.FeishuBotOpenID
	runtime.FeishuBotUserID = bot.FeishuBotUserID
	runtime.TriggerMode = bot.TriggerMode
	runtime.TriggerPrefix = bot.TriggerPrefix
	runtime.ImmediateFeedbackMode = bot.ImmediateFeedbackMode
	runtime.ImmediateFeedbackReaction = bot.ImmediateFeedbackReaction
	runtime.LLMProvider = bot.LLMProvider
	runtime.LLMProfiles = mergeLLMProfiles(nil, bot.LLMProfiles)
	if bot.GroupScenes != nil {
		runtime.GroupScenes = *bot.GroupScenes
	}
	runtime.CodexCommand = bot.CodexCommand
	runtime.CodexTimeoutSecs = bot.CodexTimeoutSecs
	runtime.CodexModel = bot.CodexModel
	runtime.CodexReasoningEffort = bot.CodexReasoningEffort
	runtime.CodexPromptPrefix = bot.CodexPromptPrefix
	runtime.ClaudeCommand = bot.ClaudeCommand
	runtime.ClaudeTimeoutSecs = bot.ClaudeTimeoutSecs
	runtime.ClaudePromptPrefix = bot.ClaudePromptPrefix
	runtime.GeminiCommand = bot.GeminiCommand
	runtime.GeminiTimeoutSecs = bot.GeminiTimeoutSecs
	runtime.GeminiPromptPrefix = bot.GeminiPromptPrefix
	runtime.KimiCommand = bot.KimiCommand
	runtime.KimiTimeoutSecs = bot.KimiTimeoutSecs
	runtime.KimiPromptPrefix = bot.KimiPromptPrefix
	runtime.RuntimeHTTPAddr = deriveBotRuntimeHTTPAddr(bot, index)
	runtime.RuntimeHTTPToken = bot.RuntimeHTTPToken
	runtime.FailureMessage = bot.FailureMessage
	runtime.ThinkingMessage = bot.ThinkingMessage
	runtime.ImageGeneration = bot.ImageGeneration
	runtime.AliceHome = deriveBotAliceHome(bot, runtime.BotID)
	runtime.WorkspaceDir = deriveBotWorkspaceDir(bot, runtime.AliceHome)
	runtime.PromptDir = deriveBotPromptDir(bot, runtime.AliceHome)
	runtime.CodexHome = deriveBotCodexHome(bot, runtime.AliceHome)
	runtime.SoulPath = deriveBotSoulPath(bot, runtime.WorkspaceDir)
	runtime.CodexEnv = mergeStringMap(nil, bot.CodexEnv)
	runtime.QueueCapacity = bot.QueueCapacity
	runtime.WorkerConcurrency = bot.WorkerConcurrency
	runtime.AutomationTaskTimeoutSecs = bot.AutomationTaskTimeoutSecs
	runtime.Permissions = mergeBotPermissions(BotPermissionsConfig{}, bot.Permissions)

	runtime, err := finalizeConfig(runtime, true)
	if err != nil {
		return Config{}, fmt.Errorf("bots.%s: %w", runtime.BotID, err)
	}
	if err := validateSceneConfig(runtime); err != nil {
		return Config{}, fmt.Errorf("bots.%s: %w", runtime.BotID, err)
	}
	return runtime, nil
}

func deriveBotAliceHome(bot BotConfig, botID string) string {
	if bot.AliceHome != "" {
		return bot.AliceHome
	}
	return filepath.Join(AliceHomeDir(), "bots", botID)
}

func deriveBotWorkspaceDir(bot BotConfig, aliceHome string) string {
	if bot.WorkspaceDir != "" {
		return bot.WorkspaceDir
	}
	return WorkspaceDirForAliceHome(aliceHome)
}

func deriveBotPromptDir(bot BotConfig, aliceHome string) string {
	if bot.PromptDir != "" {
		return bot.PromptDir
	}
	return PromptDirForAliceHome(aliceHome)
}

func deriveBotCodexHome(bot BotConfig, aliceHome string) string {
	if bot.CodexHome != "" {
		return bot.CodexHome
	}
	return CodexHomeForAliceHome(aliceHome)
}

func deriveBotSoulPath(bot BotConfig, workspaceDir string) string {
	if bot.SoulPath != "" {
		return bot.SoulPath
	}
	return filepath.Join(workspaceDir, "SOUL.md")
}

func deriveBotRuntimeHTTPAddr(bot BotConfig, index int) string {
	if bot.RuntimeHTTPAddr != "" {
		return bot.RuntimeHTTPAddr
	}
	addr, err := incrementHostPort(DefaultRuntimeHTTPAddr, index)
	if err != nil {
		return DefaultRuntimeHTTPAddr
	}
	return addr
}

func incrementHostPort(addr string, delta int) (string, error) {
	host, portStr, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return "", err
	}
	basePort := 0
	if _, err := fmt.Sscanf(portStr, "%d", &basePort); err != nil {
		return "", err
	}
	return net.JoinHostPort(host, fmt.Sprintf("%d", basePort+delta)), nil
}

func mergeLLMProfiles(base, override map[string]LLMProfileConfig) map[string]LLMProfileConfig {
	if len(base) == 0 && len(override) == 0 {
		return map[string]LLMProfileConfig{}
	}
	out := make(map[string]LLMProfileConfig, len(base)+len(override))
	for key, value := range base {
		out[key] = value
	}
	for key, value := range override {
		out[key] = value
	}
	return normalizeLLMProfiles(out)
}

func mergeStringMap(base, override map[string]string) map[string]string {
	if len(base) == 0 && len(override) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(base)+len(override))
	for key, value := range base {
		out[key] = value
	}
	for key, value := range override {
		out[key] = value
	}
	return normalizeEnvMap(out)
}

func mergeBotPermissions(base BotPermissionsConfig, override *BotPermissionsConfig) BotPermissionsConfig {
	merged := normalizeBotPermissions(base)
	if override == nil {
		return merged
	}
	if override.RuntimeMessage != nil {
		merged.RuntimeMessage = boolPtr(*override.RuntimeMessage)
	}
	if override.RuntimeAutomation != nil {
		merged.RuntimeAutomation = boolPtr(*override.RuntimeAutomation)
	}
	if override.RuntimeCampaigns != nil {
		merged.RuntimeCampaigns = boolPtr(*override.RuntimeCampaigns)
	}
	if len(override.AllowedSkills) > 0 {
		merged.AllowedSkills = normalizeStringSlice(override.AllowedSkills)
	}
	if override.Codex.Chat.Sandbox != "" {
		merged.Codex.Chat.Sandbox = override.Codex.Chat.Sandbox
	}
	if override.Codex.Chat.AskForApproval != "" {
		merged.Codex.Chat.AskForApproval = override.Codex.Chat.AskForApproval
	}
	if len(override.Codex.Chat.AddDirs) > 0 {
		merged.Codex.Chat.AddDirs = normalizePathSlice(override.Codex.Chat.AddDirs)
	}
	if override.Codex.Work.Sandbox != "" {
		merged.Codex.Work.Sandbox = override.Codex.Work.Sandbox
	}
	if override.Codex.Work.AskForApproval != "" {
		merged.Codex.Work.AskForApproval = override.Codex.Work.AskForApproval
	}
	if len(override.Codex.Work.AddDirs) > 0 {
		merged.Codex.Work.AddDirs = normalizePathSlice(override.Codex.Work.AddDirs)
	}
	return normalizeBotPermissions(merged)
}

func boolPtr(value bool) *bool {
	v := value
	return &v
}

func (cfg Config) AllowedBundledSkills() []string {
	if len(cfg.Permissions.AllowedSkills) > 0 {
		return append([]string(nil), cfg.Permissions.AllowedSkills...)
	}
	allowed := make([]string, 0, len(defaultBundledSkills))
	for _, skill := range defaultBundledSkills {
		switch skill {
		case "alice-message":
			if cfg.Permissions.RuntimeMessage != nil && !*cfg.Permissions.RuntimeMessage {
				continue
			}
		case "alice-scheduler":
			if cfg.Permissions.RuntimeAutomation != nil && !*cfg.Permissions.RuntimeAutomation {
				continue
			}
		case "alice-code-army":
			if cfg.Permissions.RuntimeCampaigns != nil && !*cfg.Permissions.RuntimeCampaigns {
				continue
			}
		}
		allowed = append(allowed, skill)
	}
	return allowed
}
