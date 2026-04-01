package config

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadFromFile_BotsAreRequired(t *testing.T) {
	path := writeConfigFile(t, "log_level: info\n")

	_, err := LoadFromFile(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "bots is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFromFile_LegacyRootBotKeysRejected(t *testing.T) {
	path := writeConfigFile(t, "feishu_app_id: cli_old\nfeishu_app_secret: secret_old\n")

	_, err := LoadFromFile(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "root bot keys are no longer supported") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFromFile_WithDefaults(t *testing.T) {
	base := t.TempDir()
	t.Setenv("HOME", base)
	t.Setenv(EnvAliceHome, base)
	t.Setenv(EnvCodexHome, "")

	cfg, runtime := loadSingleBotRuntime(t, `
feishu_app_id: cli_xxx
feishu_app_secret: sss
`)

	if cfg.FeishuBaseURL != "https://open.feishu.cn" {
		t.Fatalf("unexpected feishu_base_url: %s", cfg.FeishuBaseURL)
	}
	if runtime.FeishuBaseURL != "https://open.feishu.cn" {
		t.Fatalf("unexpected runtime feishu_base_url: %s", runtime.FeishuBaseURL)
	}
	if runtime.TriggerMode != TriggerModeAt {
		t.Fatalf("unexpected trigger_mode: %s", runtime.TriggerMode)
	}
	if runtime.TriggerPrefix != "" {
		t.Fatalf("unexpected trigger_prefix: %q", runtime.TriggerPrefix)
	}
	if runtime.ImmediateFeedbackMode != DefaultImmediateFeedbackMode {
		t.Fatalf("unexpected immediate_feedback_mode: %q", runtime.ImmediateFeedbackMode)
	}
	if runtime.ImmediateFeedbackReaction != DefaultImmediateFeedbackReaction {
		t.Fatalf("unexpected immediate_feedback_reaction: %q", runtime.ImmediateFeedbackReaction)
	}
	if runtime.LLMProvider != DefaultLLMProvider {
		t.Fatalf("unexpected llm_provider: %s", runtime.LLMProvider)
	}
	if runtime.QueueCapacity != 256 {
		t.Fatalf("unexpected queue_capacity: %d", runtime.QueueCapacity)
	}
	if runtime.WorkerConcurrency != DefaultWorkerConcurrency {
		t.Fatalf("unexpected worker_concurrency: %d", runtime.WorkerConcurrency)
	}
	if runtime.AutomationTaskTimeoutSecs != 6000 {
		t.Fatalf("unexpected automation_task_timeout_secs: %d", runtime.AutomationTaskTimeoutSecs)
	}
	if runtime.AutomationTaskTimeout != 100*time.Minute {
		t.Fatalf("unexpected automation_task_timeout: %s", runtime.AutomationTaskTimeout)
	}
	if runtime.AuthStatusTimeoutSecs != DefaultAuthStatusTimeoutSecs {
		t.Fatalf("unexpected auth_status_timeout_secs: %d", runtime.AuthStatusTimeoutSecs)
	}
	if runtime.AuthStatusTimeout != time.Duration(DefaultAuthStatusTimeoutSecs)*time.Second {
		t.Fatalf("unexpected auth_status_timeout: %s", runtime.AuthStatusTimeout)
	}
	if runtime.CampaignNotificationTimeoutSecs != DefaultCampaignNotificationTimeoutSecs {
		t.Fatalf("unexpected campaign_notification_timeout_secs: %d", runtime.CampaignNotificationTimeoutSecs)
	}
	if runtime.CampaignNotificationTimeout != time.Duration(DefaultCampaignNotificationTimeoutSecs)*time.Second {
		t.Fatalf("unexpected campaign_notification_timeout: %s", runtime.CampaignNotificationTimeout)
	}
	if runtime.RuntimeAPIShutdownTimeoutSecs != DefaultRuntimeAPIShutdownTimeoutSecs {
		t.Fatalf("unexpected runtime_api_shutdown_timeout_secs: %d", runtime.RuntimeAPIShutdownTimeoutSecs)
	}
	if runtime.RuntimeAPIShutdownTimeout != time.Duration(DefaultRuntimeAPIShutdownTimeoutSecs)*time.Second {
		t.Fatalf("unexpected runtime_api_shutdown_timeout: %s", runtime.RuntimeAPIShutdownTimeout)
	}
	if runtime.LocalRuntimeStoreOpenTimeoutSecs != DefaultLocalRuntimeStoreOpenTimeoutSecs {
		t.Fatalf("unexpected local_runtime_store_open_timeout_secs: %d", runtime.LocalRuntimeStoreOpenTimeoutSecs)
	}
	if runtime.LocalRuntimeStoreOpenTimeout != time.Duration(DefaultLocalRuntimeStoreOpenTimeoutSecs)*time.Second {
		t.Fatalf("unexpected local_runtime_store_open_timeout: %s", runtime.LocalRuntimeStoreOpenTimeout)
	}
	if runtime.CodexIdleTimeoutSecs != DefaultCodexIdleTimeoutSecs {
		t.Fatalf("unexpected codex_idle_timeout_secs: %d", runtime.CodexIdleTimeoutSecs)
	}
	if runtime.CodexIdleTimeout != time.Duration(DefaultCodexIdleTimeoutSecs)*time.Second {
		t.Fatalf("unexpected codex_idle_timeout: %s", runtime.CodexIdleTimeout)
	}
	if runtime.CodexHighIdleTimeoutSecs != DefaultCodexHighIdleTimeoutSecs {
		t.Fatalf("unexpected codex_high_idle_timeout_secs: %d", runtime.CodexHighIdleTimeoutSecs)
	}
	if runtime.CodexHighIdleTimeout != time.Duration(DefaultCodexHighIdleTimeoutSecs)*time.Second {
		t.Fatalf("unexpected codex_high_idle_timeout: %s", runtime.CodexHighIdleTimeout)
	}
	if runtime.CodexXHighIdleTimeoutSecs != DefaultCodexXHighIdleTimeoutSecs {
		t.Fatalf("unexpected codex_xhigh_idle_timeout_secs: %d", runtime.CodexXHighIdleTimeoutSecs)
	}
	if runtime.CodexXHighIdleTimeout != time.Duration(DefaultCodexXHighIdleTimeoutSecs)*time.Second {
		t.Fatalf("unexpected codex_xhigh_idle_timeout: %s", runtime.CodexXHighIdleTimeout)
	}
	if runtime.ThinkingMessage != "正在思考中..." {
		t.Fatalf("unexpected thinking_message: %s", runtime.ThinkingMessage)
	}
	if len(runtime.LLMProfiles) != 0 {
		t.Fatalf("unexpected llm_profiles: %#v", runtime.LLMProfiles)
	}
	if runtime.CodexEnv["HTTPS_PROXY"] != DefaultHTTPSProxy {
		t.Fatalf("unexpected default HTTPS_PROXY: %q", runtime.CodexEnv["HTTPS_PROXY"])
	}
	if runtime.CodexEnv["ALL_PROXY"] != DefaultALLProxy {
		t.Fatalf("unexpected default ALL_PROXY: %q", runtime.CodexEnv["ALL_PROXY"])
	}
	if runtime.GroupScenes.Chat.Enabled {
		t.Fatal("chat scene should default to disabled")
	}
	if runtime.GroupScenes.Work.Enabled {
		t.Fatal("work scene should default to disabled")
	}
	wantAliceHome := filepath.Join(base, "bots", "main")
	if runtime.AliceHome != wantAliceHome {
		t.Fatalf("unexpected alice_home: %s", runtime.AliceHome)
	}
	if runtime.WorkspaceDir != filepath.Join(wantAliceHome, "workspace") {
		t.Fatalf("unexpected workspace_dir: %s", runtime.WorkspaceDir)
	}
	if runtime.PromptDir != filepath.Join(wantAliceHome, "prompts") {
		t.Fatalf("unexpected prompt_dir: %s", runtime.PromptDir)
	}
	if runtime.CodexHome != filepath.Join(base, ".codex") {
		t.Fatalf("unexpected codex_home: %s", runtime.CodexHome)
	}
	if runtime.SoulPath != filepath.Join(runtime.WorkspaceDir, "SOUL.md") {
		t.Fatalf("unexpected soul_path: %s", runtime.SoulPath)
	}
	if got, want := filepath.Dir(cfg.LogFile), LogDirForAliceHome(cfg.AliceHome); got != want {
		t.Fatalf("unexpected log_file dir: got=%q want=%q", got, want)
	}
	if _, err := time.ParseInLocation("2006-01-02.log", filepath.Base(cfg.LogFile), time.Local); err != nil {
		t.Fatalf("unexpected log_file basename: %q err=%v", filepath.Base(cfg.LogFile), err)
	}
}

func TestLoadFromFile_AliceHomeDrivesDefaultDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(EnvCodexHome, "")

	_, runtime := loadSingleBotRuntime(t, `
feishu_app_id: cli_xxx
feishu_app_secret: sss
alice_home: "~/.alice-custom"
`)

	wantAliceHome := filepath.Join(home, ".alice-custom")
	if runtime.AliceHome != wantAliceHome {
		t.Fatalf("unexpected alice_home got=%q want=%q", runtime.AliceHome, wantAliceHome)
	}
	if runtime.WorkspaceDir != filepath.Join(wantAliceHome, "workspace") {
		t.Fatalf("unexpected workspace_dir: %s", runtime.WorkspaceDir)
	}
	if runtime.PromptDir != filepath.Join(wantAliceHome, "prompts") {
		t.Fatalf("unexpected prompt_dir: %s", runtime.PromptDir)
	}
	if runtime.CodexHome != filepath.Join(home, ".codex") {
		t.Fatalf("unexpected shared codex_home got=%q want=%q", runtime.CodexHome, filepath.Join(home, ".codex"))
	}
}

func TestLoadFromFile_RequiredKeys(t *testing.T) {
	path := writeSingleBotConfig(t, `
feishu_app_id: cli_xxx
`)

	_, err := LoadFromFile(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "feishu_app_secret is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFromFile_Env(t *testing.T) {
	_, runtime := loadSingleBotRuntime(t, `
feishu_app_id: cli_xxx
feishu_app_secret: sss
env:
  HTTPS_PROXY: "  http://127.0.0.1:7890  "
  ALL_PROXY: "socks5://127.0.0.1:7891"
`)

	if runtime.CodexEnv["HTTPS_PROXY"] != "http://127.0.0.1:7890" {
		t.Fatalf("unexpected HTTPS_PROXY: %q", runtime.CodexEnv["HTTPS_PROXY"])
	}
	if runtime.CodexEnv["ALL_PROXY"] != "socks5://127.0.0.1:7891" {
		t.Fatalf("unexpected ALL_PROXY: %q", runtime.CodexEnv["ALL_PROXY"])
	}
}

func TestLoadFromFile_LLMProfileModelConfigTrimmed(t *testing.T) {
	_, runtime := loadSingleBotRuntime(t, `
feishu_app_id: cli_xxx
feishu_app_secret: sss
llm_profiles:
  main:
    provider: codex
    model: "  gpt-5.4  "
    reasoning_effort: "  HIGH  "
`)

	profile, ok := runtime.LLMProfiles["main"]
	if !ok {
		t.Fatal("expected llm_profiles.main to exist")
	}
	if profile.Model != "gpt-5.4" {
		t.Fatalf("unexpected model: %q", profile.Model)
	}
	if profile.ReasoningEffort != "high" {
		t.Fatalf("unexpected reasoning_effort: %q", profile.ReasoningEffort)
	}
}
