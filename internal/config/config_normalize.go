package config

import "strings"

func normalizeLoadedConfig(cfg Config, rootEnv map[string]string) Config {
	cfg.FeishuAppID = strings.TrimSpace(cfg.FeishuAppID)
	cfg.FeishuAppSecret = strings.TrimSpace(cfg.FeishuAppSecret)
	cfg.FeishuBaseURL = strings.TrimSpace(cfg.FeishuBaseURL)
	cfg.TriggerMode = strings.ToLower(strings.TrimSpace(cfg.TriggerMode))
	cfg.TriggerPrefix = strings.TrimSpace(cfg.TriggerPrefix)
	cfg.ImmediateFeedbackMode = strings.ToLower(strings.TrimSpace(cfg.ImmediateFeedbackMode))
	cfg.ImmediateFeedbackReaction = strings.ToUpper(strings.TrimSpace(cfg.ImmediateFeedbackReaction))
	cfg.LLMProvider = strings.ToLower(strings.TrimSpace(cfg.LLMProvider))
	cfg.LLMProfiles = normalizeLLMProfiles(cfg.LLMProfiles)
	cfg.GroupScenes = normalizeGroupScenes(cfg.GroupScenes)
	cfg.PrivateScenes = normalizePrivateScenes(cfg.PrivateScenes)
	cfg.CodexEnv = normalizeEnvMap(rootEnv)
	cfg.RuntimeHTTPAddr = strings.TrimSpace(cfg.RuntimeHTTPAddr)
	cfg.RuntimeHTTPToken = strings.TrimSpace(cfg.RuntimeHTTPToken)
	cfg.FailureMessage = strings.TrimSpace(cfg.FailureMessage)
	cfg.ThinkingMessage = strings.TrimSpace(cfg.ThinkingMessage)
	cfg.AliceHome = strings.TrimSpace(cfg.AliceHome)
	cfg.WorkspaceDir = strings.TrimSpace(cfg.WorkspaceDir)
	cfg.PromptDir = strings.TrimSpace(cfg.PromptDir)
	cfg.CodexHome = strings.TrimSpace(cfg.CodexHome)
	cfg.SoulPath = strings.TrimSpace(cfg.SoulPath)
	cfg.BotName = strings.TrimSpace(cfg.BotName)
	cfg.Permissions = normalizeBotPermissions(cfg.Permissions)
	cfg.Bots = normalizeBots(cfg.Bots)
	cfg.LogLevel = strings.ToLower(strings.TrimSpace(cfg.LogLevel))
	cfg.LogFile = strings.TrimSpace(cfg.LogFile)
	return cfg
}

func normalizeEnvMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}

	out := make(map[string]string, len(in))
	for key, value := range in {
		normalizedKey := strings.ToUpper(strings.TrimSpace(key))
		out[normalizedKey] = strings.TrimSpace(value)
	}
	return out
}

func applyDefaultCodexEnv(in map[string]string) map[string]string {
	return normalizeEnvMap(in)
}

func normalizeLLMProfiles(in map[string]LLMProfileConfig) map[string]LLMProfileConfig {
	if len(in) == 0 {
		return map[string]LLMProfileConfig{}
	}
	out := make(map[string]LLMProfileConfig, len(in))
	for rawName, profile := range in {
		name := strings.ToLower(strings.TrimSpace(rawName))
		if name == "" {
			continue
		}
		profile.Provider = strings.ToLower(strings.TrimSpace(profile.Provider))
		profile.Command = strings.TrimSpace(profile.Command)
		profile.Model = strings.TrimSpace(profile.Model)
		profile.Profile = strings.TrimSpace(profile.Profile)
		profile.ReasoningEffort = strings.ToLower(strings.TrimSpace(profile.ReasoningEffort))
		profile.Variant = strings.ToLower(strings.TrimSpace(profile.Variant))
		profile.Personality = strings.ToLower(strings.TrimSpace(profile.Personality))
		profile.PromptPrefix = strings.TrimSpace(profile.PromptPrefix)
		if profile.Permissions != nil {
			normalized := normalizeCodexExecPolicy(*profile.Permissions)
			profile.Permissions = &normalized
		}
		out[name] = profile
	}
	return out
}

func normalizeLLMProvider(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func normalizeGroupScenes(in GroupScenesConfig) GroupScenesConfig {
	in.Chat.TriggerTag = strings.TrimSpace(in.Chat.TriggerTag)
	in.Chat.SessionScope = strings.ToLower(strings.TrimSpace(in.Chat.SessionScope))
	in.Chat.LLMProfile = strings.ToLower(strings.TrimSpace(in.Chat.LLMProfile))
	in.Chat.NoReplyToken = strings.TrimSpace(in.Chat.NoReplyToken)
	if in.Chat.SessionScope == "" {
		in.Chat.SessionScope = GroupSceneSessionPerChat
	}

	in.Work.TriggerTag = strings.TrimSpace(in.Work.TriggerTag)
	in.Work.SessionScope = strings.ToLower(strings.TrimSpace(in.Work.SessionScope))
	in.Work.LLMProfile = strings.ToLower(strings.TrimSpace(in.Work.LLMProfile))
	in.Work.NoReplyToken = strings.TrimSpace(in.Work.NoReplyToken)
	if in.Work.SessionScope == "" {
		in.Work.SessionScope = GroupSceneSessionPerThread
	}
	return in
}

func normalizePrivateScenes(in GroupScenesConfig) GroupScenesConfig {
	in.Chat.TriggerTag = strings.TrimSpace(in.Chat.TriggerTag)
	in.Chat.SessionScope = strings.ToLower(strings.TrimSpace(in.Chat.SessionScope))
	in.Chat.LLMProfile = strings.ToLower(strings.TrimSpace(in.Chat.LLMProfile))
	in.Chat.NoReplyToken = strings.TrimSpace(in.Chat.NoReplyToken)
	if in.Chat.SessionScope == "" {
		in.Chat.SessionScope = GroupSceneSessionPerUser
	}

	in.Work.TriggerTag = strings.TrimSpace(in.Work.TriggerTag)
	in.Work.SessionScope = strings.ToLower(strings.TrimSpace(in.Work.SessionScope))
	in.Work.LLMProfile = strings.ToLower(strings.TrimSpace(in.Work.LLMProfile))
	in.Work.NoReplyToken = strings.TrimSpace(in.Work.NoReplyToken)
	if in.Work.SessionScope == "" {
		in.Work.SessionScope = GroupSceneSessionPerMessage
	}
	return in
}
