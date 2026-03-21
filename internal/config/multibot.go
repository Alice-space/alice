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
	if err := validateBaseConfig(cfg, requireCredentials); err != nil {
		return Config{}, err
	}
	if cfg.FeishuBaseURL == "" {
		cfg.FeishuBaseURL = "https://open.feishu.cn"
	}
	if cfg.LLMProvider == "" {
		cfg.LLMProvider = DefaultLLMProvider
	}
	if cfg.TriggerMode == "" {
		cfg.TriggerMode = TriggerModeAt
	}
	if cfg.ImmediateFeedbackMode == "" {
		cfg.ImmediateFeedbackMode = ImmediateFeedbackModeReply
	}
	if cfg.ImmediateFeedbackReaction == "" {
		cfg.ImmediateFeedbackReaction = DefaultImmediateFeedbackReaction
	}
	if cfg.CodexCommand == "" {
		cfg.CodexCommand = "codex"
	}
	if cfg.ClaudeCommand == "" {
		cfg.ClaudeCommand = "claude"
	}
	if cfg.KimiCommand == "" {
		cfg.KimiCommand = "kimi"
	}
	if cfg.RuntimeHTTPAddr == "" {
		cfg.RuntimeHTTPAddr = "127.0.0.1:7331"
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
	if cfg.BotName == "" {
		switch {
		case strings.TrimSpace(cfg.BotID) != "":
			cfg.BotName = strings.TrimSpace(cfg.BotID)
		default:
			cfg.BotName = "Alice"
		}
	}

	switch cfg.LLMProvider {
	case DefaultLLMProvider, LLMProviderClaude, LLMProviderKimi:
	default:
		return Config{}, fmt.Errorf("unsupported llm_provider %q", cfg.LLMProvider)
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
	cfg.KimiTimeout = time.Duration(cfg.KimiTimeoutSecs) * time.Second
	cfg.AutomationTaskTimeout = time.Duration(cfg.AutomationTaskTimeoutSecs) * time.Second

	if len(cfg.Bots) == 0 {
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
		case "", DefaultLLMProvider, LLMProviderClaude, LLMProviderKimi:
		default:
			return fmt.Errorf("llm_profiles.%s.provider %q is unsupported", name, profile.Provider)
		}
	}
	if cfg.GroupScenes.Chat.Enabled {
		if cfg.GroupScenes.Chat.LLMProfile == "" {
			return errors.New("group_scenes.chat.llm_profile is required when chat scene is enabled")
		}
		profile, ok := cfg.LLMProfiles[cfg.GroupScenes.Chat.LLMProfile]
		if !ok {
			return fmt.Errorf("group_scenes.chat.llm_profile %q is undefined", cfg.GroupScenes.Chat.LLMProfile)
		}
		if profile.Provider != "" && profile.Provider != cfg.LLMProvider {
			return fmt.Errorf("group_scenes.chat.llm_profile %q provider %q does not match current llm_provider %q", cfg.GroupScenes.Chat.LLMProfile, profile.Provider, cfg.LLMProvider)
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
		profile, ok := cfg.LLMProfiles[cfg.GroupScenes.Work.LLMProfile]
		if !ok {
			return fmt.Errorf("group_scenes.work.llm_profile %q is undefined", cfg.GroupScenes.Work.LLMProfile)
		}
		if profile.Provider != "" && profile.Provider != cfg.LLMProvider {
			return fmt.Errorf("group_scenes.work.llm_profile %q provider %q does not match current llm_provider %q", cfg.GroupScenes.Work.LLMProfile, profile.Provider, cfg.LLMProvider)
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
		bot.KimiCommand = strings.TrimSpace(bot.KimiCommand)
		bot.KimiPromptPrefix = strings.TrimSpace(bot.KimiPromptPrefix)
		bot.RuntimeHTTPAddr = strings.TrimSpace(bot.RuntimeHTTPAddr)
		bot.RuntimeHTTPToken = strings.TrimSpace(bot.RuntimeHTTPToken)
		bot.FailureMessage = strings.TrimSpace(bot.FailureMessage)
		bot.ThinkingMessage = strings.TrimSpace(bot.ThinkingMessage)
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
		single := cfg
		single.Bots = nil
		single.PrimaryBotID = ""
		if strings.TrimSpace(single.BotID) == "" {
			single.BotID = "default"
		}
		if strings.TrimSpace(single.BotName) == "" {
			single.BotName = "Alice"
		}
		if err := validateSceneConfig(single); err != nil {
			return nil, err
		}
		return []Config{single}, nil
	}

	primaryBotID, err := cfg.resolvePrimaryBotID()
	if err != nil {
		return nil, err
	}
	ordered := orderBotIDs(cfg.Bots, primaryBotID)
	runtimes := make([]Config, 0, len(ordered))
	for idx, botID := range ordered {
		runtime, err := cfg.deriveBotRuntimeConfig(botID, cfg.Bots[botID], idx, primaryBotID)
		if err != nil {
			return nil, err
		}
		runtimes = append(runtimes, runtime)
	}
	return runtimes, nil
}

func (cfg Config) resolvePrimaryBotID() (string, error) {
	if len(cfg.Bots) == 0 {
		return "", nil
	}
	if id := strings.ToLower(strings.TrimSpace(cfg.PrimaryBotID)); id != "" {
		if _, ok := cfg.Bots[id]; !ok {
			return "", fmt.Errorf("primary_bot %q is undefined", id)
		}
		return id, nil
	}
	if len(cfg.Bots) == 1 {
		for id := range cfg.Bots {
			return id, nil
		}
	}

	rootAppID := strings.TrimSpace(cfg.FeishuAppID)
	rootBotOpenID := strings.TrimSpace(cfg.FeishuBotOpenID)
	rootBotUserID := strings.TrimSpace(cfg.FeishuBotUserID)
	for id, bot := range cfg.Bots {
		appID := bot.FeishuAppID
		if appID == "" {
			appID = rootAppID
		}
		openID := bot.FeishuBotOpenID
		if openID == "" {
			openID = rootBotOpenID
		}
		userID := bot.FeishuBotUserID
		if userID == "" {
			userID = rootBotUserID
		}
		if rootAppID != "" && appID == rootAppID {
			if rootBotOpenID != "" && openID == rootBotOpenID {
				return id, nil
			}
			if rootBotUserID != "" && userID == rootBotUserID {
				return id, nil
			}
		}
	}

	ids := make([]string, 0, len(cfg.Bots))
	for id := range cfg.Bots {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids[0], nil
}

func orderBotIDs(bots map[string]BotConfig, primaryBotID string) []string {
	ids := make([]string, 0, len(bots))
	for id := range bots {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if primaryBotID == "" {
		return ids
	}
	ordered := make([]string, 0, len(ids))
	ordered = append(ordered, primaryBotID)
	for _, id := range ids {
		if id == primaryBotID {
			continue
		}
		ordered = append(ordered, id)
	}
	return ordered
}

func (cfg Config) deriveBotRuntimeConfig(botID string, bot BotConfig, index int, primaryBotID string) (Config, error) {
	runtime := cfg
	runtime.Bots = nil
	runtime.PrimaryBotID = ""
	runtime.BotID = strings.TrimSpace(botID)

	if bot.Name != "" {
		runtime.BotName = bot.Name
	}
	if runtime.BotName == "" {
		runtime.BotName = runtime.BotID
	}

	if bot.FeishuAppID != "" {
		runtime.FeishuAppID = bot.FeishuAppID
	}
	if bot.FeishuAppSecret != "" {
		runtime.FeishuAppSecret = bot.FeishuAppSecret
	}
	if bot.FeishuBaseURL != "" {
		runtime.FeishuBaseURL = bot.FeishuBaseURL
	}
	if bot.FeishuBotOpenID != "" {
		runtime.FeishuBotOpenID = bot.FeishuBotOpenID
	}
	if bot.FeishuBotUserID != "" {
		runtime.FeishuBotUserID = bot.FeishuBotUserID
	}
	if bot.TriggerMode != "" {
		runtime.TriggerMode = bot.TriggerMode
	}
	if bot.TriggerPrefix != "" {
		runtime.TriggerPrefix = bot.TriggerPrefix
	}
	if bot.ImmediateFeedbackMode != "" {
		runtime.ImmediateFeedbackMode = bot.ImmediateFeedbackMode
	}
	if bot.ImmediateFeedbackReaction != "" {
		runtime.ImmediateFeedbackReaction = bot.ImmediateFeedbackReaction
	}
	if bot.LLMProvider != "" {
		runtime.LLMProvider = bot.LLMProvider
	}
	runtime.LLMProfiles = mergeLLMProfiles(runtime.LLMProfiles, bot.LLMProfiles)
	if bot.GroupScenes != nil {
		runtime.GroupScenes = *bot.GroupScenes
	}
	if bot.CodexCommand != "" {
		runtime.CodexCommand = bot.CodexCommand
	}
	if bot.CodexTimeoutSecs > 0 {
		runtime.CodexTimeoutSecs = bot.CodexTimeoutSecs
	}
	if bot.CodexModel != "" {
		runtime.CodexModel = bot.CodexModel
	}
	if bot.CodexReasoningEffort != "" {
		runtime.CodexReasoningEffort = bot.CodexReasoningEffort
	}
	if bot.CodexPromptPrefix != "" {
		runtime.CodexPromptPrefix = bot.CodexPromptPrefix
	}
	if bot.ClaudeCommand != "" {
		runtime.ClaudeCommand = bot.ClaudeCommand
	}
	if bot.ClaudeTimeoutSecs > 0 {
		runtime.ClaudeTimeoutSecs = bot.ClaudeTimeoutSecs
	}
	if bot.ClaudePromptPrefix != "" {
		runtime.ClaudePromptPrefix = bot.ClaudePromptPrefix
	}
	if bot.KimiCommand != "" {
		runtime.KimiCommand = bot.KimiCommand
	}
	if bot.KimiTimeoutSecs > 0 {
		runtime.KimiTimeoutSecs = bot.KimiTimeoutSecs
	}
	if bot.KimiPromptPrefix != "" {
		runtime.KimiPromptPrefix = bot.KimiPromptPrefix
	}
	if bot.FailureMessage != "" {
		runtime.FailureMessage = bot.FailureMessage
	}
	if bot.ThinkingMessage != "" {
		runtime.ThinkingMessage = bot.ThinkingMessage
	}
	if bot.QueueCapacity > 0 {
		runtime.QueueCapacity = bot.QueueCapacity
	}
	if bot.WorkerConcurrency > 0 {
		runtime.WorkerConcurrency = bot.WorkerConcurrency
	}
	if bot.AutomationTaskTimeoutSecs > 0 {
		runtime.AutomationTaskTimeoutSecs = bot.AutomationTaskTimeoutSecs
	}
	runtime.CodexEnv = mergeStringMap(runtime.CodexEnv, bot.CodexEnv)
	runtime.Permissions = mergeBotPermissions(runtime.Permissions, bot.Permissions)

	isPrimary := runtime.BotID == primaryBotID
	runtime.AliceHome = deriveBotAliceHome(cfg, bot, runtime.BotID, isPrimary)
	runtime.WorkspaceDir = deriveBotWorkspaceDir(cfg, bot, runtime.AliceHome, isPrimary)
	runtime.PromptDir = deriveBotPromptDir(cfg, bot, runtime.AliceHome, isPrimary)
	runtime.CodexHome = deriveBotCodexHome(cfg, bot, runtime.AliceHome, isPrimary)
	runtime.SoulPath = deriveBotSoulPath(cfg, bot, runtime.WorkspaceDir, isPrimary)
	runtime.RuntimeHTTPAddr = deriveBotRuntimeHTTPAddr(cfg, bot, index, isPrimary)
	if bot.RuntimeHTTPToken != "" {
		runtime.RuntimeHTTPToken = bot.RuntimeHTTPToken
	} else if !isPrimary {
		runtime.RuntimeHTTPToken = ""
	}

	runtime, err := finalizeConfig(runtime, true)
	if err != nil {
		return Config{}, fmt.Errorf("bots.%s: %w", runtime.BotID, err)
	}
	if err := validateSceneConfig(runtime); err != nil {
		return Config{}, fmt.Errorf("bots.%s: %w", runtime.BotID, err)
	}
	return runtime, nil
}

func deriveBotAliceHome(root Config, bot BotConfig, botID string, isPrimary bool) string {
	if bot.AliceHome != "" {
		return bot.AliceHome
	}
	if isPrimary {
		return root.AliceHome
	}
	return filepath.Join(root.AliceHome, "bots", botID)
}

func deriveBotWorkspaceDir(root Config, bot BotConfig, aliceHome string, isPrimary bool) string {
	if bot.WorkspaceDir != "" {
		return bot.WorkspaceDir
	}
	if isPrimary {
		return root.WorkspaceDir
	}
	return WorkspaceDirForAliceHome(aliceHome)
}

func deriveBotPromptDir(root Config, bot BotConfig, aliceHome string, isPrimary bool) string {
	if bot.PromptDir != "" {
		return bot.PromptDir
	}
	if isPrimary {
		return root.PromptDir
	}
	return PromptDirForAliceHome(aliceHome)
}

func deriveBotCodexHome(root Config, bot BotConfig, aliceHome string, isPrimary bool) string {
	if bot.CodexHome != "" {
		return bot.CodexHome
	}
	if isPrimary && root.CodexHome != "" {
		return root.CodexHome
	}
	return CodexHomeForAliceHome(aliceHome)
}

func deriveBotSoulPath(root Config, bot BotConfig, workspaceDir string, isPrimary bool) string {
	if bot.SoulPath != "" {
		return bot.SoulPath
	}
	if isPrimary && root.SoulPath != "" {
		return root.SoulPath
	}
	return filepath.Join(workspaceDir, "SOUL.md")
}

func deriveBotRuntimeHTTPAddr(root Config, bot BotConfig, index int, isPrimary bool) string {
	if bot.RuntimeHTTPAddr != "" {
		return bot.RuntimeHTTPAddr
	}
	if isPrimary {
		return root.RuntimeHTTPAddr
	}
	addr, err := incrementHostPort(root.RuntimeHTTPAddr, index)
	if err != nil {
		return root.RuntimeHTTPAddr
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
