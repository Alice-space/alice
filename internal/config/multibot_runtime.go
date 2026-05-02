package config

import (
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"sort"
	"strings"
)

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
	if err := validateUniqueRuntimeHTTPAddrs(runtimes); err != nil {
		return nil, err
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
	runtimes, err := cfg.RuntimeConfigs()
	if err != nil {
		return Config{}, err
	}
	for _, runtime := range runtimes {
		if runtime.BotID == botID {
			return runtime, nil
		}
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
	var err error
	if bot.Name != "" {
		runtime.BotName = bot.Name
	} else {
		runtime.BotName = runtime.BotID
	}

	runtime.FeishuAppID = bot.FeishuAppID
	runtime.FeishuAppSecret = bot.FeishuAppSecret
	runtime.FeishuBaseURL = bot.FeishuBaseURL
	runtime.TriggerMode = bot.TriggerMode
	runtime.TriggerPrefix = bot.TriggerPrefix
	runtime.ImmediateFeedbackMode = bot.ImmediateFeedbackMode
	runtime.ImmediateFeedbackReaction = bot.ImmediateFeedbackReaction
	runtime.LLMProfiles = mergeLLMProfiles(nil, bot.LLMProfiles)
	if bot.GroupScenes != nil {
		runtime.GroupScenes = *bot.GroupScenes
	}
	if bot.PrivateScenes != nil {
		runtime.PrivateScenes = *bot.PrivateScenes
	}
	runtime.RuntimeHTTPAddr, err = deriveBotRuntimeHTTPAddr(bot, index)
	if err != nil {
		return Config{}, fmt.Errorf("bots.%s: derive runtime_http_addr failed: %w", runtime.BotID, err)
	}
	runtime.RuntimeHTTPToken = bot.RuntimeHTTPToken
	runtime.FailureMessage = bot.FailureMessage
	runtime.ThinkingMessage = bot.ThinkingMessage
	runtime.AliceHome = deriveBotAliceHome(bot, runtime.BotID)
	runtime.WorkspaceDir = deriveBotWorkspaceDir(bot, runtime.AliceHome)
	runtime.PromptDir = deriveBotPromptDir(bot, runtime.AliceHome)
	runtime.CodexHome = deriveBotCodexHome(bot, runtime.AliceHome)
	runtime.SoulPath = deriveBotSoulPath(bot, runtime.AliceHome)
	runtime.CodexEnv = mergeStringMap(nil, bot.Env)
	runtime.QueueCapacity = bot.QueueCapacity
	runtime.WorkerConcurrency = bot.WorkerConcurrency
	runtime.AutomationTaskTimeoutSecs = bot.AutomationTaskTimeoutSecs
	runtime.AuthStatusTimeoutSecs = bot.AuthStatusTimeoutSecs
	runtime.RuntimeAPIShutdownTimeoutSecs = bot.RuntimeAPIShutdownTimeoutSecs
	runtime.LocalRuntimeStoreOpenTimeoutSecs = bot.LocalRuntimeStoreOpenTimeoutSecs
	runtime.CodexIdleTimeoutSecs = bot.CodexIdleTimeoutSecs
	runtime.CodexHighIdleTimeoutSecs = bot.CodexHighIdleTimeoutSecs
	runtime.CodexXHighIdleTimeoutSecs = bot.CodexXHighIdleTimeoutSecs
	runtime.Permissions = mergeBotPermissions(BotPermissionsConfig{}, bot.Permissions)

	runtime, err = finalizeConfig(runtime, true)
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

func deriveBotCodexHome(bot BotConfig, _ string) string {
	if bot.CodexHome != "" {
		return ResolveCodexHomeDir(bot.CodexHome)
	}
	return DefaultCodexHome()
}

func deriveBotSoulPath(bot BotConfig, aliceHome string) string {
	if bot.SoulPath != "" {
		return bot.SoulPath
	}
	return SoulPathForAliceHome(aliceHome)
}

func deriveBotRuntimeHTTPAddr(bot BotConfig, index int) (string, error) {
	if bot.RuntimeHTTPAddr != "" {
		return strings.TrimSpace(bot.RuntimeHTTPAddr), nil
	}
	return incrementHostPort(DefaultRuntimeHTTPAddr, index)
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

func validateUniqueRuntimeHTTPAddrs(runtimes []Config) error {
	seen := make(map[string]string, len(runtimes))
	for _, runtime := range runtimes {
		addr := strings.TrimSpace(runtime.RuntimeHTTPAddr)
		if addr == "" {
			continue
		}
		if existing, ok := seen[addr]; ok {
			return fmt.Errorf("runtime_http_addr %q is duplicated between bots %q and %q", addr, existing, runtime.BotID)
		}
		seen[addr] = runtime.BotID
	}
	return nil
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
