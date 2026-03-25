package bootstrap

import (
	"reflect"
	"testing"
	"time"

	"github.com/Alice-space/alice/internal/config"
)

func TestDiffRestartRequiredFields(t *testing.T) {
	current := config.Config{
		FeishuAppID:       "cli_a",
		FeishuAppSecret:   "sec_a",
		FeishuBaseURL:     "https://open.feishu.cn",
		RuntimeHTTPAddr:   "127.0.0.1:7331",
		RuntimeHTTPToken:  "token_a",
		WorkspaceDir:      "/workspace/a",
		PromptDir:         "prompts",
		QueueCapacity:     256,
		WorkerConcurrency: 1,
	}
	next := current
	next.TriggerMode = "prefix"
	next.TriggerPrefix = "!alice"
	next.RuntimeHTTPAddr = "127.0.0.1:7332"
	next.QueueCapacity = 512
	next.WorkerConcurrency = 3

	got := diffRestartRequiredFields(current, next)
	want := []string{
		"queue_capacity",
		"runtime_http_addr",
		"worker_concurrency",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected restart-required fields: got=%v want=%v", got, want)
	}
}

func TestApplyReloadableFields(t *testing.T) {
	current := config.Config{
		TriggerMode:               config.TriggerModeAt,
		TriggerPrefix:             "",
		FeishuBotOpenID:           "ou_old",
		FeishuBotUserID:           "bot_user_old",
		FailureMessage:            "old fail",
		ThinkingMessage:           "old thinking",
		ImmediateFeedbackMode:     config.ImmediateFeedbackModeReply,
		ImmediateFeedbackReaction: "SMILE",
		LLMProvider:               config.DefaultLLMProvider,
		CodexEnv:                  map[string]string{"HTTPS_PROXY": "http://127.0.0.1:7890"},
		AutomationTaskTimeoutSecs: 6000,
		AutomationTaskTimeout:     100 * time.Minute,
		LLMProfiles: map[string]config.LLMProfileConfig{
			"chat": {Provider: config.DefaultLLMProvider, Model: "gpt-5.4-mini", ReasoningEffort: "low", Personality: "friendly"},
		},
		GroupScenes: config.GroupScenesConfig{
			Chat: config.GroupSceneConfig{Enabled: true, LLMProfile: "chat", SessionScope: config.GroupSceneSessionPerChat, NoReplyToken: "[[NO_REPLY]]"},
		},
		LogLevel:          "info",
		LogFile:           "",
		LogMaxSizeMB:      20,
		LogMaxBackups:     5,
		LogMaxAgeDays:     7,
		LogCompress:       false,
		QueueCapacity:     256,
		WorkerConcurrency: 1,
	}
	next := current
	next.TriggerMode = config.TriggerModePrefix
	next.TriggerPrefix = "!alice"
	next.FeishuBotOpenID = "ou_new"
	next.FeishuBotUserID = "bot_user_new"
	next.CodexEnv = map[string]string{"HTTPS_PROXY": "http://127.0.0.1:9999", "ALL_PROXY": "socks5://127.0.0.1:1080"}
	next.LogLevel = "debug"
	next.AutomationTaskTimeoutSecs = 900
	next.AutomationTaskTimeout = 15 * time.Minute
	next.LLMProfiles = map[string]config.LLMProfileConfig{
		"chat": {Provider: config.DefaultLLMProvider, Model: "gpt-5.4-mini", ReasoningEffort: "medium", Personality: "friendly"},
		"work": {Provider: config.DefaultLLMProvider, Model: "gpt-5.4", ReasoningEffort: "xhigh", Personality: "pragmatic"},
	}
	next.GroupScenes = config.GroupScenesConfig{
		Chat: config.GroupSceneConfig{Enabled: true, LLMProfile: "chat", SessionScope: config.GroupSceneSessionPerChat, NoReplyToken: "[[NO_REPLY]]"},
		Work: config.GroupSceneConfig{Enabled: true, LLMProfile: "work", SessionScope: config.GroupSceneSessionPerThread, TriggerTag: "#work", CreateFeishuThread: true},
	}
	next.QueueCapacity = 1024
	next.WorkerConcurrency = 8

	changed := make(map[string]struct{})
	applyReloadableFields(&current, next, changed)

	if current.TriggerMode != config.TriggerModePrefix || current.TriggerPrefix != "!alice" {
		t.Fatalf("trigger settings should be hot-reloaded, got mode=%q prefix=%q", current.TriggerMode, current.TriggerPrefix)
	}
	if !stringMapEqual(current.CodexEnv, next.CodexEnv) {
		t.Fatalf("env should be hot-reloaded, got=%v want=%v", current.CodexEnv, next.CodexEnv)
	}
	if !llmProfileMapEqual(current.LLMProfiles, next.LLMProfiles) {
		t.Fatalf("llm_profiles should be hot-reloaded, got=%v want=%v", current.LLMProfiles, next.LLMProfiles)
	}
	if current.GroupScenes != next.GroupScenes {
		t.Fatalf("group_scenes should be hot-reloaded, got=%#v want=%#v", current.GroupScenes, next.GroupScenes)
	}
	if current.LogLevel != "debug" {
		t.Fatalf("log_level should be hot-reloaded, got=%q", current.LogLevel)
	}
	if current.AutomationTaskTimeout != 15*time.Minute {
		t.Fatalf("automation timeout should be hot-reloaded, got=%s", current.AutomationTaskTimeout)
	}
	if current.QueueCapacity != 256 {
		t.Fatalf("queue_capacity should remain unchanged without restart, got=%d", current.QueueCapacity)
	}
	if current.WorkerConcurrency != 1 {
		t.Fatalf("worker_concurrency should remain unchanged without restart, got=%d", current.WorkerConcurrency)
	}
	if _, ok := changed["trigger_mode"]; !ok {
		t.Fatalf("changed set should include trigger_mode, got=%v", changed)
	}
	if _, ok := changed["env"]; !ok {
		t.Fatalf("changed set should include env, got=%v", changed)
	}
	if _, ok := changed["llm_profiles"]; !ok {
		t.Fatalf("changed set should include llm_profiles, got=%v", changed)
	}
	if _, ok := changed["group_scenes"]; !ok {
		t.Fatalf("changed set should include group_scenes, got=%v", changed)
	}
	if _, ok := changed["log_level"]; !ok {
		t.Fatalf("changed set should include log_level, got=%v", changed)
	}
}
