package runtimecfg

import (
	"strings"

	"github.com/Alice-space/alice/internal/config"
)

const (
	SceneChat = "chat"
	SceneWork = "work"
)

func CloneLLMProfiles(in map[string]config.LLMProfileConfig) map[string]config.LLMProfileConfig {
	if len(in) == 0 {
		return map[string]config.LLMProfileConfig{}
	}
	out := make(map[string]config.LLMProfileConfig, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func DetectScene(sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	switch {
	case strings.Contains(sessionKey, "|scene:"+SceneWork):
		return SceneWork
	case strings.Contains(sessionKey, "|scene:"+SceneChat):
		return SceneChat
	default:
		return ""
	}
}

func ResolveSceneLLMProfile(
	llmProfiles map[string]config.LLMProfileConfig,
	groupScenes config.GroupScenesConfig,
	sessionKey string,
) (config.LLMProfileConfig, bool) {
	var name string
	switch DetectScene(sessionKey) {
	case SceneWork:
		name = strings.TrimSpace(groupScenes.Work.LLMProfile)
	case SceneChat:
		name = strings.TrimSpace(groupScenes.Chat.LLMProfile)
	default:
		return config.LLMProfileConfig{}, false
	}
	profile, ok := llmProfiles[name]
	return profile, ok
}

func ThreadReplyPreferred(groupScenes config.GroupScenesConfig, sessionKey, chatType string) bool {
	switch DetectScene(sessionKey) {
	case SceneChat:
		return groupScenes.Chat.CreateFeishuThread
	case SceneWork:
		return groupScenes.Work.CreateFeishuThread
	}
	switch strings.TrimSpace(chatType) {
	case "group", "topic_group":
		return true
	default:
		return false
	}
}
