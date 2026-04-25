package config

import (
	"time"

	"github.com/go-playground/validator/v10"
)

const DefaultLLMProvider = "codex"
const LLMProviderClaude = "claude"
const LLMProviderGemini = "gemini"
const LLMProviderKimi = "kimi"
const LLMProviderOpenCode = "opencode"
const TriggerModeAt = "at"
const TriggerModePrefix = "prefix"
const ImmediateFeedbackModeReply = "reply"
const ImmediateFeedbackModeReaction = "reaction"
const DefaultImmediateFeedbackMode = ImmediateFeedbackModeReaction
const DefaultImmediateFeedbackReaction = "OK"
const DefaultRuntimeHTTPAddr = "127.0.0.1:7331"
const DefaultWorkerConcurrency = 3
const DefaultLLMTimeoutSecs = 172800
const DefaultAuthStatusTimeoutSecs = 15
const DefaultRuntimeAPIShutdownTimeoutSecs = 5
const DefaultLocalRuntimeStoreOpenTimeoutSecs = 10
const DefaultCodexIdleTimeoutSecs = 900
const DefaultCodexHighIdleTimeoutSecs = 1800
const DefaultCodexXHighIdleTimeoutSecs = 3600

var configValidator = validator.New()

const (
	GroupSceneSessionPerChat   = "per_chat"
	GroupSceneSessionPerThread = "per_thread"
)

type LLMProfileConfig struct {
	Provider        string                 `mapstructure:"provider"`
	Command         string                 `mapstructure:"command"`
	TimeoutSecs     int                    `mapstructure:"timeout_secs"`
	Model           string                 `mapstructure:"model"`
	Profile         string                 `mapstructure:"profile"`
	ReasoningEffort string                 `mapstructure:"reasoning_effort"`
	Personality     string                 `mapstructure:"personality"`
	PromptPrefix    string                 `mapstructure:"prompt_prefix"`
	Permissions     *CodexExecPolicyConfig `mapstructure:"permissions"`

	// Computed at finalization, not from YAML.
	Timeout time.Duration `mapstructure:"-"`
}

type GroupSceneConfig struct {
	Enabled            bool   `mapstructure:"enabled"`
	TriggerTag         string `mapstructure:"trigger_tag"`
	SessionScope       string `mapstructure:"session_scope"`
	LLMProfile         string `mapstructure:"llm_profile"`
	NoReplyToken       string `mapstructure:"no_reply_token"`
	CreateFeishuThread bool   `mapstructure:"create_feishu_thread"`
}

type GroupScenesConfig struct {
	Chat GroupSceneConfig `mapstructure:"chat"`
	Work GroupSceneConfig `mapstructure:"work"`
}

type CodexExecPolicyConfig struct {
	Sandbox        string   `mapstructure:"sandbox"`
	AskForApproval string   `mapstructure:"ask_for_approval"`
	AddDirs        []string `mapstructure:"add_dirs"`
}

type BotPermissionsConfig struct {
	RuntimeMessage    *bool    `mapstructure:"runtime_message"`
	RuntimeAutomation *bool    `mapstructure:"runtime_automation"`
	AllowedSkills     []string `mapstructure:"allowed_skills"`
}

type BotConfig struct {
	Name                             string                      `mapstructure:"name"`
	FeishuAppID                      string                      `mapstructure:"feishu_app_id"`
	FeishuAppSecret                  string                      `mapstructure:"feishu_app_secret"`
	FeishuBaseURL                    string                      `mapstructure:"feishu_base_url"`
	TriggerMode                      string                      `mapstructure:"trigger_mode"`
	TriggerPrefix                    string                      `mapstructure:"trigger_prefix"`
	ImmediateFeedbackMode            string                      `mapstructure:"immediate_feedback_mode"`
	ImmediateFeedbackReaction        string                      `mapstructure:"immediate_feedback_reaction"`
	LLMProfiles                      map[string]LLMProfileConfig `mapstructure:"llm_profiles"`
	GroupScenes                      *GroupScenesConfig          `mapstructure:"group_scenes"`
	RuntimeHTTPAddr                  string                      `mapstructure:"runtime_http_addr"`
	RuntimeHTTPToken                 string                      `mapstructure:"runtime_http_token"`
	FailureMessage                   string                      `mapstructure:"failure_message"`
	ThinkingMessage                  string                      `mapstructure:"thinking_message"`
	AliceHome                        string                      `mapstructure:"alice_home"`
	WorkspaceDir                     string                      `mapstructure:"workspace_dir"`
	PromptDir                        string                      `mapstructure:"prompt_dir"`
	CodexHome                        string                      `mapstructure:"codex_home"`
	SoulPath                         string                      `mapstructure:"soul_path"`
	Env                              map[string]string           `mapstructure:"env"`
	QueueCapacity                    int                         `mapstructure:"queue_capacity"`
	WorkerConcurrency                int                         `mapstructure:"worker_concurrency"`
	AutomationTaskTimeoutSecs        int                         `mapstructure:"automation_task_timeout_secs"`
	AuthStatusTimeoutSecs            int                         `mapstructure:"auth_status_timeout_secs"`
	RuntimeAPIShutdownTimeoutSecs    int                         `mapstructure:"runtime_api_shutdown_timeout_secs"`
	LocalRuntimeStoreOpenTimeoutSecs int                         `mapstructure:"local_runtime_store_open_timeout_secs"`
	CodexIdleTimeoutSecs             int                         `mapstructure:"codex_idle_timeout_secs"`
	CodexHighIdleTimeoutSecs         int                         `mapstructure:"codex_high_idle_timeout_secs"`
	CodexXHighIdleTimeoutSecs        int                         `mapstructure:"codex_xhigh_idle_timeout_secs"`
	Permissions                      *BotPermissionsConfig       `mapstructure:"permissions"`
}

type Config struct {
	BotID                     string `mapstructure:"-"`
	BotName                   string `mapstructure:"bot_name"`
	FeishuAppID               string `mapstructure:"feishu_app_id"`
	FeishuAppSecret           string `mapstructure:"feishu_app_secret"`
	FeishuBaseURL             string `mapstructure:"feishu_base_url"`
	TriggerMode               string `mapstructure:"trigger_mode"`
	TriggerPrefix             string `mapstructure:"trigger_prefix"`
	ImmediateFeedbackMode     string `mapstructure:"immediate_feedback_mode"`
	ImmediateFeedbackReaction string `mapstructure:"immediate_feedback_reaction"`

	LLMProvider string                      `mapstructure:"llm_provider"`
	LLMProfiles map[string]LLMProfileConfig `mapstructure:"llm_profiles"`
	GroupScenes GroupScenesConfig           `mapstructure:"group_scenes"`

	// Shared env for all LLM subprocesses (HTTPS_PROXY, API keys, etc.)
	CodexEnv  map[string]string `mapstructure:"env"`
	CodexHome string            `mapstructure:"codex_home"`

	RuntimeHTTPAddr  string `mapstructure:"runtime_http_addr"`
	RuntimeHTTPToken string `mapstructure:"runtime_http_token"`
	FailureMessage   string `mapstructure:"failure_message"`
	ThinkingMessage  string `mapstructure:"thinking_message"`

	AliceHome    string               `mapstructure:"alice_home"`
	WorkspaceDir string               `mapstructure:"workspace_dir"`
	PromptDir    string               `mapstructure:"prompt_dir"`
	SoulPath     string               `mapstructure:"soul_path"`
	Permissions  BotPermissionsConfig `mapstructure:"permissions"`
	Bots         map[string]BotConfig `mapstructure:"bots"`

	QueueCapacity                    int           `mapstructure:"queue_capacity"`
	WorkerConcurrency                int           `mapstructure:"worker_concurrency"`
	AutomationTaskTimeoutSecs        int           `mapstructure:"automation_task_timeout_secs"`
	AutomationTaskTimeout            time.Duration `mapstructure:"-"`
	AuthStatusTimeoutSecs            int           `mapstructure:"auth_status_timeout_secs"`
	AuthStatusTimeout                time.Duration `mapstructure:"-"`
	RuntimeAPIShutdownTimeoutSecs    int           `mapstructure:"runtime_api_shutdown_timeout_secs"`
	RuntimeAPIShutdownTimeout        time.Duration `mapstructure:"-"`
	LocalRuntimeStoreOpenTimeoutSecs int           `mapstructure:"local_runtime_store_open_timeout_secs"`
	LocalRuntimeStoreOpenTimeout     time.Duration `mapstructure:"-"`
	CodexIdleTimeoutSecs             int           `mapstructure:"codex_idle_timeout_secs"`
	CodexIdleTimeout                 time.Duration `mapstructure:"-"`
	CodexHighIdleTimeoutSecs         int           `mapstructure:"codex_high_idle_timeout_secs"`
	CodexHighIdleTimeout             time.Duration `mapstructure:"-"`
	CodexXHighIdleTimeoutSecs        int           `mapstructure:"codex_xhigh_idle_timeout_secs"`
	CodexXHighIdleTimeout            time.Duration `mapstructure:"-"`

	LogLevel      string `mapstructure:"log_level"`
	LogFile       string `mapstructure:"log_file"`
	LogMaxSizeMB  int    `mapstructure:"log_max_size_mb"`
	LogMaxBackups int    `mapstructure:"log_max_backups"`
	LogMaxAgeDays int    `mapstructure:"log_max_age_days"`
	LogCompress   bool   `mapstructure:"log_compress"`
}
