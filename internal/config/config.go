package config

import (
	"time"

	"github.com/go-playground/validator/v10"
)

const DefaultLLMProvider = "codex"
const LLMProviderClaude = "claude"
const LLMProviderGemini = "gemini"
const LLMProviderKimi = "kimi"
const TriggerModeAt = "at"
const TriggerModePrefix = "prefix"
const ImmediateFeedbackModeReply = "reply"
const ImmediateFeedbackModeReaction = "reaction"
const DefaultImmediateFeedbackMode = ImmediateFeedbackModeReaction
const DefaultImmediateFeedbackReaction = "OK"
const DefaultRuntimeHTTPAddr = "127.0.0.1:7331"
const DefaultWorkerConcurrency = 3
const DefaultHTTPSProxy = "http://127.0.0.1:8080"
const DefaultALLProxy = "http://127.0.0.1:8080"

var configValidator = validator.New()

const (
	GroupSceneSessionPerChat   = "per_chat"
	GroupSceneSessionPerThread = "per_thread"
)

type LLMProfileConfig struct {
	Provider        string `mapstructure:"provider"`
	Model           string `mapstructure:"model"`
	Profile         string `mapstructure:"profile"`
	ReasoningEffort string `mapstructure:"reasoning_effort"`
	Personality     string `mapstructure:"personality"`
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

type ImageGenerationConfig struct {
	Enabled               bool   `mapstructure:"enabled"`
	Provider              string `mapstructure:"provider"`
	Model                 string `mapstructure:"model"`
	BaseURL               string `mapstructure:"base_url"`
	TimeoutSecs           int    `mapstructure:"timeout_secs"`
	Moderation            string `mapstructure:"moderation"`
	N                     int    `mapstructure:"n"`
	OutputCompression     int    `mapstructure:"output_compression"`
	ResponseFormat        string `mapstructure:"response_format"`
	Size                  string `mapstructure:"size"`
	Quality               string `mapstructure:"quality"`
	Background            string `mapstructure:"background"`
	OutputFormat          string `mapstructure:"output_format"`
	PartialImages         int    `mapstructure:"partial_images"`
	Stream                bool   `mapstructure:"stream"`
	Style                 string `mapstructure:"style"`
	InputFidelity         string `mapstructure:"input_fidelity"`
	MaskPath              string `mapstructure:"mask_path"`
	UseCurrentAttachments bool   `mapstructure:"use_current_attachments"`
}

type CodexExecPolicyConfig struct {
	Sandbox        string   `mapstructure:"sandbox"`
	AskForApproval string   `mapstructure:"ask_for_approval"`
	AddDirs        []string `mapstructure:"add_dirs"`
}

type SceneCodexPoliciesConfig struct {
	Chat CodexExecPolicyConfig `mapstructure:"chat"`
	Work CodexExecPolicyConfig `mapstructure:"work"`
}

type BotPermissionsConfig struct {
	RuntimeMessage    *bool                    `mapstructure:"runtime_message"`
	RuntimeAutomation *bool                    `mapstructure:"runtime_automation"`
	RuntimeCampaigns  *bool                    `mapstructure:"runtime_campaigns"`
	AllowedSkills     []string                 `mapstructure:"allowed_skills"`
	Codex             SceneCodexPoliciesConfig `mapstructure:"codex"`
}

type BotConfig struct {
	Name                      string                      `mapstructure:"name"`
	FeishuAppID               string                      `mapstructure:"feishu_app_id"`
	FeishuAppSecret           string                      `mapstructure:"feishu_app_secret"`
	FeishuBaseURL             string                      `mapstructure:"feishu_base_url"`
	FeishuBotOpenID           string                      `mapstructure:"feishu_bot_open_id"`
	FeishuBotUserID           string                      `mapstructure:"feishu_bot_user_id"`
	TriggerMode               string                      `mapstructure:"trigger_mode"`
	TriggerPrefix             string                      `mapstructure:"trigger_prefix"`
	ImmediateFeedbackMode     string                      `mapstructure:"immediate_feedback_mode"`
	ImmediateFeedbackReaction string                      `mapstructure:"immediate_feedback_reaction"`
	LLMProvider               string                      `mapstructure:"llm_provider"`
	LLMProfiles               map[string]LLMProfileConfig `mapstructure:"llm_profiles"`
	GroupScenes               *GroupScenesConfig          `mapstructure:"group_scenes"`
	CodexCommand              string                      `mapstructure:"codex_command"`
	CodexTimeoutSecs          int                         `mapstructure:"codex_timeout_secs"`
	CodexModel                string                      `mapstructure:"codex_model"`
	CodexReasoningEffort      string                      `mapstructure:"codex_model_reasoning_effort"`
	CodexPromptPrefix         string                      `mapstructure:"codex_prompt_prefix"`
	ClaudeCommand             string                      `mapstructure:"claude_command"`
	ClaudeTimeoutSecs         int                         `mapstructure:"claude_timeout_secs"`
	ClaudePromptPrefix        string                      `mapstructure:"claude_prompt_prefix"`
	GeminiCommand             string                      `mapstructure:"gemini_command"`
	GeminiTimeoutSecs         int                         `mapstructure:"gemini_timeout_secs"`
	GeminiPromptPrefix        string                      `mapstructure:"gemini_prompt_prefix"`
	KimiCommand               string                      `mapstructure:"kimi_command"`
	KimiTimeoutSecs           int                         `mapstructure:"kimi_timeout_secs"`
	KimiPromptPrefix          string                      `mapstructure:"kimi_prompt_prefix"`
	RuntimeHTTPAddr           string                      `mapstructure:"runtime_http_addr"`
	RuntimeHTTPToken          string                      `mapstructure:"runtime_http_token"`
	FailureMessage            string                      `mapstructure:"failure_message"`
	ThinkingMessage           string                      `mapstructure:"thinking_message"`
	ImageGeneration           ImageGenerationConfig       `mapstructure:"image_generation"`
	AliceHome                 string                      `mapstructure:"alice_home"`
	WorkspaceDir              string                      `mapstructure:"workspace_dir"`
	PromptDir                 string                      `mapstructure:"prompt_dir"`
	CodexHome                 string                      `mapstructure:"codex_home"`
	SoulPath                  string                      `mapstructure:"soul_path"`
	CodexEnv                  map[string]string           `mapstructure:"env"`
	QueueCapacity             int                         `mapstructure:"queue_capacity"`
	WorkerConcurrency         int                         `mapstructure:"worker_concurrency"`
	AutomationTaskTimeoutSecs int                         `mapstructure:"automation_task_timeout_secs"`
	Permissions               *BotPermissionsConfig       `mapstructure:"permissions"`
}

type Config struct {
	BotID                     string `mapstructure:"-"`
	BotName                   string `mapstructure:"bot_name"`
	FeishuAppID               string `mapstructure:"feishu_app_id"`
	FeishuAppSecret           string `mapstructure:"feishu_app_secret"`
	FeishuBaseURL             string `mapstructure:"feishu_base_url"`
	FeishuBotOpenID           string `mapstructure:"feishu_bot_open_id"`
	FeishuBotUserID           string `mapstructure:"feishu_bot_user_id"`
	TriggerMode               string `mapstructure:"trigger_mode"`
	TriggerPrefix             string `mapstructure:"trigger_prefix"`
	ImmediateFeedbackMode     string `mapstructure:"immediate_feedback_mode"`
	ImmediateFeedbackReaction string `mapstructure:"immediate_feedback_reaction"`

	LLMProvider string                      `mapstructure:"llm_provider"`
	LLMProfiles map[string]LLMProfileConfig `mapstructure:"llm_profiles"`
	GroupScenes GroupScenesConfig           `mapstructure:"group_scenes"`

	CodexCommand         string                `mapstructure:"codex_command"`
	CodexTimeout         time.Duration         `mapstructure:"-"`
	CodexTimeoutSecs     int                   `mapstructure:"codex_timeout_secs"`
	CodexModel           string                `mapstructure:"codex_model"`
	CodexReasoningEffort string                `mapstructure:"codex_model_reasoning_effort"`
	CodexEnv             map[string]string     `mapstructure:"env"`
	CodexPromptPrefix    string                `mapstructure:"codex_prompt_prefix"`
	ClaudeCommand        string                `mapstructure:"claude_command"`
	ClaudeTimeout        time.Duration         `mapstructure:"-"`
	ClaudeTimeoutSecs    int                   `mapstructure:"claude_timeout_secs"`
	ClaudePromptPrefix   string                `mapstructure:"claude_prompt_prefix"`
	GeminiCommand        string                `mapstructure:"gemini_command"`
	GeminiTimeout        time.Duration         `mapstructure:"-"`
	GeminiTimeoutSecs    int                   `mapstructure:"gemini_timeout_secs"`
	GeminiPromptPrefix   string                `mapstructure:"gemini_prompt_prefix"`
	KimiCommand          string                `mapstructure:"kimi_command"`
	KimiTimeout          time.Duration         `mapstructure:"-"`
	KimiTimeoutSecs      int                   `mapstructure:"kimi_timeout_secs"`
	KimiPromptPrefix     string                `mapstructure:"kimi_prompt_prefix"`
	RuntimeHTTPAddr      string                `mapstructure:"runtime_http_addr"`
	RuntimeHTTPToken     string                `mapstructure:"runtime_http_token"`
	FailureMessage       string                `mapstructure:"failure_message"`
	ThinkingMessage      string                `mapstructure:"thinking_message"`
	ImageGeneration      ImageGenerationConfig `mapstructure:"image_generation"`
	AliceHome            string                `mapstructure:"alice_home"`
	WorkspaceDir         string                `mapstructure:"workspace_dir"`
	PromptDir            string                `mapstructure:"prompt_dir"`
	CodexHome            string                `mapstructure:"codex_home"`
	SoulPath             string                `mapstructure:"soul_path"`
	Permissions          BotPermissionsConfig  `mapstructure:"permissions"`
	Bots                 map[string]BotConfig  `mapstructure:"bots"`

	QueueCapacity             int           `mapstructure:"queue_capacity"`
	WorkerConcurrency         int           `mapstructure:"worker_concurrency"`
	AutomationTaskTimeoutSecs int           `mapstructure:"automation_task_timeout_secs"`
	AutomationTaskTimeout     time.Duration `mapstructure:"-"`

	LogLevel      string `mapstructure:"log_level"`
	LogFile       string `mapstructure:"log_file"`
	LogMaxSizeMB  int    `mapstructure:"log_max_size_mb"`
	LogMaxBackups int    `mapstructure:"log_max_backups"`
	LogMaxAgeDays int    `mapstructure:"log_max_age_days"`
	LogCompress   bool   `mapstructure:"log_compress"`
}
