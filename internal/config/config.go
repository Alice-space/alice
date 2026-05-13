package config

import (
	"time"

	"github.com/go-playground/validator/v10"
)

// DefaultLLMProvider is the default LLM backend identifier.
const DefaultLLMProvider = "codex"

// LLMProviderClaude is the Claude provider name constant.
const LLMProviderClaude = "claude"

// LLMProviderKimi is the Kimi provider name constant.
const LLMProviderKimi = "kimi"

// LLMProviderOpenCode is the OpenCode provider name constant.
const LLMProviderOpenCode = "opencode"

// TriggerModeAt sets the trigger mode to at-mentions only.
const TriggerModeAt = "at"

// TriggerModePrefix sets the trigger mode to message prefix matching.
const TriggerModePrefix = "prefix"

// TriggerModeAll sets the trigger mode to all messages.
const TriggerModeAll = "all"

// TriggerModeWithoutPrefix accepts every message except those starting with trigger_prefix.
const TriggerModeWithoutPrefix = "without_prefix"

// ImmediateFeedbackModeReply sends a reply as immediate feedback.
const ImmediateFeedbackModeReply = "reply"

// ImmediateFeedbackModeReaction sends a reaction as immediate feedback.
const ImmediateFeedbackModeReaction = "reaction"

// DefaultImmediateFeedbackMode is the default immediate feedback mode.
const DefaultImmediateFeedbackMode = ImmediateFeedbackModeReaction

// DefaultImmediateFeedbackReaction is the default reaction emoji for immediate feedback.
const DefaultImmediateFeedbackReaction = "OK"

// DefaultRuntimeSocket is the default runtime API Unix socket filename, resolved relative to AliceHome.
const DefaultRuntimeSocket = "runtime.sock"

// DefaultWorkerConcurrency is the default worker pool size.
const DefaultWorkerConcurrency = 3

// DefaultLLMTimeoutSecs is the default LLM subprocess timeout in seconds.
const DefaultLLMTimeoutSecs = 172800

// DefaultAuthStatusTimeoutSecs is the default auth status check timeout in seconds.
const DefaultAuthStatusTimeoutSecs = 15

// DefaultRuntimeAPIShutdownTimeoutSecs is the default runtime API shutdown timeout in seconds.
const DefaultRuntimeAPIShutdownTimeoutSecs = 5

// DefaultLocalRuntimeStoreOpenTimeoutSecs is the default local runtime store open timeout in seconds.
const DefaultLocalRuntimeStoreOpenTimeoutSecs = 10

// DefaultCodexIdleTimeoutSecs is the default low idle timeout in seconds.
const DefaultCodexIdleTimeoutSecs = 900

// DefaultCodexHighIdleTimeoutSecs is the default high idle timeout in seconds.
const DefaultCodexHighIdleTimeoutSecs = 1800

// DefaultCodexXHighIdleTimeoutSecs is the default extreme idle timeout in seconds.
const DefaultCodexXHighIdleTimeoutSecs = 3600

var configValidator = validator.New()

const (
	// GroupSceneSessionPerChat scopes group sessions per chat.
	GroupSceneSessionPerChat = "per_chat"
	// GroupSceneSessionPerThread scopes group sessions per thread.
	GroupSceneSessionPerThread = "per_thread"
	// GroupSceneSessionPerUser scopes group sessions per user.
	GroupSceneSessionPerUser = "per_user"
	// GroupSceneSessionPerMessage scopes group sessions per message.
	GroupSceneSessionPerMessage = "per_message"
)

// LLMProfileConfig is an LLM backend profile configuration.
type LLMProfileConfig struct {
	Provider        string                 `mapstructure:"provider"`
	Command         string                 `mapstructure:"command"`
	TimeoutSecs     int                    `mapstructure:"timeout_secs"`
	Model           string                 `mapstructure:"model"`
	Profile         string                 `mapstructure:"profile"`
	ReasoningEffort string                 `mapstructure:"reasoning_effort"`
	Variant         string                 `mapstructure:"variant"`
	Personality     string                 `mapstructure:"personality"`
	PromptPrefix    string                 `mapstructure:"prompt_prefix"`
	Permissions     *CodexExecPolicyConfig `mapstructure:"permissions"`

	// Computed at finalization, not from YAML.
	Timeout time.Duration `mapstructure:"-"`
}

// GroupSceneConfig is a group chat scene configuration.
type GroupSceneConfig struct {
	Enabled              bool   `mapstructure:"enabled"`
	TriggerTag           string `mapstructure:"trigger_tag"`
	SessionScope         string `mapstructure:"session_scope"`
	LLMProfile           string `mapstructure:"llm_profile"`
	NoReplyToken         string `mapstructure:"no_reply_token"`
	CreateFeishuThread   bool   `mapstructure:"create_feishu_thread"`
	DisableIdentityHints *bool  `mapstructure:"disable_identity_hints"`
}

// GroupScenesConfig is a pair of group chat scene configurations.
type GroupScenesConfig struct {
	Chat GroupSceneConfig `mapstructure:"chat"`
	Work GroupSceneConfig `mapstructure:"work"`
}

// CodexExecPolicyConfig is the sandbox and approval policy configuration.
type CodexExecPolicyConfig struct {
	Sandbox        string   `mapstructure:"sandbox"`
	AskForApproval string   `mapstructure:"ask_for_approval"`
	AddDirs        []string `mapstructure:"add_dirs"`
}

// BotPermissionsConfig is bot-level permission controls.
type BotPermissionsConfig struct {
	RuntimeMessage    *bool    `mapstructure:"runtime_message"`
	RuntimeAutomation *bool    `mapstructure:"runtime_automation"`
	AllowedSkills     []string `mapstructure:"allowed_skills"`
}

// BotConfig is a per-bot configuration.
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
	PrivateScenes                    *GroupScenesConfig          `mapstructure:"private_scenes"`
	RuntimeSocket                    string                      `mapstructure:"runtime_socket"`
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
	ShowShellCommands                *bool                       `mapstructure:"show_shell_commands"`
	DisableIdentityHints             *bool                       `mapstructure:"disable_identity_hints"`
	Permissions                      *BotPermissionsConfig       `mapstructure:"permissions"`
}

// Config is the top-level runtime configuration.
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

	LLMProvider   string                      `mapstructure:"llm_provider"`
	LLMProfiles   map[string]LLMProfileConfig `mapstructure:"llm_profiles"`
	GroupScenes   GroupScenesConfig           `mapstructure:"group_scenes"`
	PrivateScenes GroupScenesConfig           `mapstructure:"private_scenes"`

	// Shared env for all LLM subprocesses (HTTPS_PROXY, API keys, etc.)
	CodexEnv  map[string]string `mapstructure:"env"`
	CodexHome string            `mapstructure:"codex_home"`

	RuntimeSocket    string `mapstructure:"runtime_socket"`
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
	ShowShellCommands                *bool         `mapstructure:"show_shell_commands"`
	DisableIdentityHints             *bool         `mapstructure:"disable_identity_hints"`

	LogLevel      string `mapstructure:"log_level"`
	LogFile       string `mapstructure:"log_file"`
	LogMaxSizeMB  int    `mapstructure:"log_max_size_mb"`
	LogMaxBackups int    `mapstructure:"log_max_backups"`
	LogMaxAgeDays int    `mapstructure:"log_max_age_days"`
	LogCompress   bool   `mapstructure:"log_compress"`
}
