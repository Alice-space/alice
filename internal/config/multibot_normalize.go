package config

import "strings"

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
		bot.TriggerMode = strings.ToLower(strings.TrimSpace(bot.TriggerMode))
		bot.TriggerPrefix = strings.TrimSpace(bot.TriggerPrefix)
		bot.ImmediateFeedbackMode = strings.ToLower(strings.TrimSpace(bot.ImmediateFeedbackMode))
		bot.ImmediateFeedbackReaction = strings.ToUpper(strings.TrimSpace(bot.ImmediateFeedbackReaction))
		bot.LLMProfiles = normalizeLLMProfiles(bot.LLMProfiles)
		if bot.GroupScenes != nil {
			normalized := normalizeGroupScenes(*bot.GroupScenes)
			bot.GroupScenes = &normalized
		}
		if bot.PrivateScenes != nil {
			normalized := normalizePrivateScenes(*bot.PrivateScenes)
			bot.PrivateScenes = &normalized
		}
		bot.RuntimeHTTPAddr = strings.TrimSpace(bot.RuntimeHTTPAddr)
		bot.RuntimeHTTPToken = strings.TrimSpace(bot.RuntimeHTTPToken)
		bot.FailureMessage = strings.TrimSpace(bot.FailureMessage)
		bot.ThinkingMessage = strings.TrimSpace(bot.ThinkingMessage)
		bot.AliceHome = strings.TrimSpace(bot.AliceHome)
		bot.WorkspaceDir = strings.TrimSpace(bot.WorkspaceDir)
		bot.PromptDir = strings.TrimSpace(bot.PromptDir)
		bot.CodexHome = strings.TrimSpace(bot.CodexHome)
		bot.SoulPath = strings.TrimSpace(bot.SoulPath)
		bot.Env = normalizeEnvMap(bot.Env)
		if bot.Permissions != nil {
			normalized := normalizeBotPermissions(*bot.Permissions)
			bot.Permissions = &normalized
		}
		out[id] = bot
	}
	return out
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

func boolPtr(value bool) *bool {
	v := value
	return &v
}
