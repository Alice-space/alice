package config

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/spf13/viper"
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

func LoadFromFile(path string) (Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")
	v.SetDefault("feishu_base_url", "https://open.feishu.cn")
	v.SetDefault("trigger_mode", TriggerModeAt)
	v.SetDefault("trigger_prefix", "")
	v.SetDefault("immediate_feedback_mode", DefaultImmediateFeedbackMode)
	v.SetDefault("immediate_feedback_reaction", DefaultImmediateFeedbackReaction)
	v.SetDefault("codex_command", "codex")
	v.SetDefault("codex_timeout_secs", 172800)
	v.SetDefault("codex_model", "")
	v.SetDefault("codex_model_reasoning_effort", "")
	v.SetDefault("claude_command", "claude")
	v.SetDefault("claude_timeout_secs", 172800)
	v.SetDefault("gemini_command", "gemini")
	v.SetDefault("gemini_timeout_secs", 172800)
	v.SetDefault("kimi_command", "kimi")
	v.SetDefault("kimi_timeout_secs", 172800)
	v.SetDefault("runtime_http_addr", DefaultRuntimeHTTPAddr)
	v.SetDefault("runtime_http_token", "")
	v.SetDefault("failure_message", "Codex 暂时不可用，请稍后重试。")
	v.SetDefault("thinking_message", "正在思考中...")
	v.SetDefault("image_generation.enabled", false)
	v.SetDefault("image_generation.provider", "openai")
	v.SetDefault("image_generation.model", "gpt-image-1.5")
	v.SetDefault("image_generation.base_url", "")
	v.SetDefault("image_generation.timeout_secs", 120)
	v.SetDefault("image_generation.moderation", "")
	v.SetDefault("image_generation.n", 0)
	v.SetDefault("image_generation.output_compression", -1)
	v.SetDefault("image_generation.response_format", "")
	v.SetDefault("image_generation.size", "1024x1536")
	v.SetDefault("image_generation.quality", "high")
	v.SetDefault("image_generation.background", "auto")
	v.SetDefault("image_generation.output_format", "png")
	v.SetDefault("image_generation.partial_images", -1)
	v.SetDefault("image_generation.stream", false)
	v.SetDefault("image_generation.style", "")
	v.SetDefault("image_generation.input_fidelity", "high")
	v.SetDefault("image_generation.mask_path", "")
	v.SetDefault("image_generation.use_current_attachments", true)
	v.SetDefault("alice_home", AliceHomeDir())
	v.SetDefault("workspace_dir", "")
	v.SetDefault("prompt_dir", "")
	v.SetDefault("codex_home", "")
	v.SetDefault("soul_path", "")
	v.SetDefault("env.HTTPS_PROXY", DefaultHTTPSProxy)
	v.SetDefault("env.ALL_PROXY", DefaultALLProxy)
	v.SetDefault("bot_name", "")
	v.SetDefault("permissions.runtime_message", true)
	v.SetDefault("permissions.runtime_automation", true)
	v.SetDefault("permissions.runtime_campaigns", true)
	v.SetDefault("permissions.codex.chat.sandbox", "workspace-write")
	v.SetDefault("permissions.codex.chat.ask_for_approval", "never")
	v.SetDefault("permissions.codex.work.sandbox", "danger-full-access")
	v.SetDefault("permissions.codex.work.ask_for_approval", "never")
	v.SetDefault("queue_capacity", 256)
	v.SetDefault("worker_concurrency", DefaultWorkerConcurrency)
	v.SetDefault("automation_task_timeout_secs", 6000)
	v.SetDefault("group_scenes.chat.enabled", false)
	v.SetDefault("group_scenes.chat.session_scope", GroupSceneSessionPerChat)
	v.SetDefault("group_scenes.chat.llm_profile", "")
	v.SetDefault("group_scenes.chat.no_reply_token", "[[NO_REPLY]]")
	v.SetDefault("group_scenes.chat.create_feishu_thread", false)
	v.SetDefault("group_scenes.work.enabled", false)
	v.SetDefault("group_scenes.work.trigger_tag", "#work")
	v.SetDefault("group_scenes.work.session_scope", GroupSceneSessionPerThread)
	v.SetDefault("group_scenes.work.llm_profile", "")
	v.SetDefault("group_scenes.work.no_reply_token", "")
	v.SetDefault("group_scenes.work.create_feishu_thread", true)
	v.SetDefault("log_level", "info")
	v.SetDefault("log_file", "")
	v.SetDefault("log_max_size_mb", 20)
	v.SetDefault("log_max_backups", 5)
	v.SetDefault("log_max_age_days", 7)
	v.SetDefault("log_compress", false)

	if err := v.ReadInConfig(); err != nil {
		return Config{}, fmt.Errorf("read config file %q failed: %w", path, err)
	}
	if err := validatePureMultiBotRootConfig(v); err != nil {
		return Config{}, err
	}
	if err := rejectRemovedImageProxyConfig(v); err != nil {
		return Config{}, err
	}
	setBotDefaults(v)

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse config yaml failed: %w", err)
	}

	cfg.FeishuAppID = strings.TrimSpace(cfg.FeishuAppID)
	cfg.FeishuAppSecret = strings.TrimSpace(cfg.FeishuAppSecret)
	cfg.FeishuBaseURL = strings.TrimSpace(cfg.FeishuBaseURL)
	cfg.FeishuBotOpenID = strings.TrimSpace(cfg.FeishuBotOpenID)
	cfg.FeishuBotUserID = strings.TrimSpace(cfg.FeishuBotUserID)
	cfg.TriggerMode = strings.ToLower(strings.TrimSpace(cfg.TriggerMode))
	cfg.TriggerPrefix = strings.TrimSpace(cfg.TriggerPrefix)
	cfg.ImmediateFeedbackMode = strings.ToLower(strings.TrimSpace(cfg.ImmediateFeedbackMode))
	cfg.ImmediateFeedbackReaction = strings.ToUpper(strings.TrimSpace(cfg.ImmediateFeedbackReaction))
	cfg.LLMProvider = strings.ToLower(strings.TrimSpace(cfg.LLMProvider))
	cfg.LLMProfiles = normalizeLLMProfiles(cfg.LLMProfiles)
	cfg.GroupScenes = normalizeGroupScenes(cfg.GroupScenes)
	cfg.CodexCommand = strings.TrimSpace(cfg.CodexCommand)
	cfg.CodexModel = strings.TrimSpace(cfg.CodexModel)
	cfg.CodexReasoningEffort = strings.ToLower(strings.TrimSpace(cfg.CodexReasoningEffort))
	cfg.CodexEnv = normalizeEnvMap(v.GetStringMapString("env"))
	cfg.CodexPromptPrefix = strings.TrimSpace(cfg.CodexPromptPrefix)
	cfg.ClaudeCommand = strings.TrimSpace(cfg.ClaudeCommand)
	cfg.ClaudePromptPrefix = strings.TrimSpace(cfg.ClaudePromptPrefix)
	cfg.GeminiCommand = strings.TrimSpace(cfg.GeminiCommand)
	cfg.GeminiPromptPrefix = strings.TrimSpace(cfg.GeminiPromptPrefix)
	cfg.KimiCommand = strings.TrimSpace(cfg.KimiCommand)
	cfg.KimiPromptPrefix = strings.TrimSpace(cfg.KimiPromptPrefix)
	cfg.RuntimeHTTPAddr = strings.TrimSpace(cfg.RuntimeHTTPAddr)
	cfg.RuntimeHTTPToken = strings.TrimSpace(cfg.RuntimeHTTPToken)
	cfg.FailureMessage = strings.TrimSpace(cfg.FailureMessage)
	cfg.ThinkingMessage = strings.TrimSpace(cfg.ThinkingMessage)
	cfg.ImageGeneration = normalizeImageGenerationConfig(cfg.ImageGeneration)
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

	return finalizeConfig(cfg, false)
}

func setBotDefaults(v *viper.Viper) {
	if v == nil {
		return
	}
	for rawBotID := range v.GetStringMap("bots") {
		prefix := "bots." + rawBotID + "."
		v.SetDefault(prefix+"feishu_base_url", "https://open.feishu.cn")
		v.SetDefault(prefix+"trigger_mode", TriggerModeAt)
		v.SetDefault(prefix+"trigger_prefix", "")
		v.SetDefault(prefix+"immediate_feedback_mode", DefaultImmediateFeedbackMode)
		v.SetDefault(prefix+"immediate_feedback_reaction", DefaultImmediateFeedbackReaction)
		v.SetDefault(prefix+"codex_command", "codex")
		v.SetDefault(prefix+"codex_timeout_secs", 172800)
		v.SetDefault(prefix+"codex_model", "")
		v.SetDefault(prefix+"codex_model_reasoning_effort", "")
		v.SetDefault(prefix+"claude_command", "claude")
		v.SetDefault(prefix+"claude_timeout_secs", 172800)
		v.SetDefault(prefix+"gemini_command", "gemini")
		v.SetDefault(prefix+"gemini_timeout_secs", 172800)
		v.SetDefault(prefix+"kimi_command", "kimi")
		v.SetDefault(prefix+"kimi_timeout_secs", 172800)
		v.SetDefault(prefix+"runtime_http_token", "")
		v.SetDefault(prefix+"failure_message", "Codex 暂时不可用，请稍后重试。")
		v.SetDefault(prefix+"thinking_message", "正在思考中...")
		v.SetDefault(prefix+"image_generation.enabled", false)
		v.SetDefault(prefix+"image_generation.provider", "openai")
		v.SetDefault(prefix+"image_generation.model", "gpt-image-1.5")
		v.SetDefault(prefix+"image_generation.base_url", "")
		v.SetDefault(prefix+"image_generation.timeout_secs", 120)
		v.SetDefault(prefix+"image_generation.moderation", "")
		v.SetDefault(prefix+"image_generation.n", 0)
		v.SetDefault(prefix+"image_generation.output_compression", -1)
		v.SetDefault(prefix+"image_generation.response_format", "")
		v.SetDefault(prefix+"image_generation.size", "1024x1536")
		v.SetDefault(prefix+"image_generation.quality", "high")
		v.SetDefault(prefix+"image_generation.background", "auto")
		v.SetDefault(prefix+"image_generation.output_format", "png")
		v.SetDefault(prefix+"image_generation.partial_images", -1)
		v.SetDefault(prefix+"image_generation.stream", false)
		v.SetDefault(prefix+"image_generation.style", "")
		v.SetDefault(prefix+"image_generation.input_fidelity", "high")
		v.SetDefault(prefix+"image_generation.mask_path", "")
		v.SetDefault(prefix+"image_generation.use_current_attachments", true)
		v.SetDefault(prefix+"env.HTTPS_PROXY", DefaultHTTPSProxy)
		v.SetDefault(prefix+"env.ALL_PROXY", DefaultALLProxy)
		v.SetDefault(prefix+"queue_capacity", 256)
		v.SetDefault(prefix+"worker_concurrency", DefaultWorkerConcurrency)
		v.SetDefault(prefix+"automation_task_timeout_secs", 6000)
		v.SetDefault(prefix+"permissions.runtime_message", true)
		v.SetDefault(prefix+"permissions.runtime_automation", true)
		v.SetDefault(prefix+"permissions.runtime_campaigns", true)
		v.SetDefault(prefix+"permissions.codex.chat.sandbox", "workspace-write")
		v.SetDefault(prefix+"permissions.codex.chat.ask_for_approval", "never")
		v.SetDefault(prefix+"permissions.codex.work.sandbox", "danger-full-access")
		v.SetDefault(prefix+"permissions.codex.work.ask_for_approval", "never")
		v.SetDefault(prefix+"group_scenes.chat.enabled", false)
		v.SetDefault(prefix+"group_scenes.chat.session_scope", GroupSceneSessionPerChat)
		v.SetDefault(prefix+"group_scenes.chat.llm_profile", "")
		v.SetDefault(prefix+"group_scenes.chat.no_reply_token", "[[NO_REPLY]]")
		v.SetDefault(prefix+"group_scenes.chat.create_feishu_thread", false)
		v.SetDefault(prefix+"group_scenes.work.enabled", false)
		v.SetDefault(prefix+"group_scenes.work.trigger_tag", "#work")
		v.SetDefault(prefix+"group_scenes.work.session_scope", GroupSceneSessionPerThread)
		v.SetDefault(prefix+"group_scenes.work.llm_profile", "")
		v.SetDefault(prefix+"group_scenes.work.no_reply_token", "")
		v.SetDefault(prefix+"group_scenes.work.create_feishu_thread", true)
	}
}

func validatePureMultiBotRootConfig(v *viper.Viper) error {
	if v == nil {
		return nil
	}
	legacyKeys := []string{
		"bot_name",
		"feishu_app_id",
		"feishu_app_secret",
		"feishu_base_url",
		"feishu_bot_open_id",
		"feishu_bot_user_id",
		"trigger_mode",
		"trigger_prefix",
		"immediate_feedback_mode",
		"immediate_feedback_reaction",
		"llm_provider",
		"llm_profiles",
		"group_scenes",
		"codex_command",
		"codex_timeout_secs",
		"codex_model",
		"codex_model_reasoning_effort",
		"codex_prompt_prefix",
		"claude_command",
		"claude_timeout_secs",
		"claude_prompt_prefix",
		"gemini_command",
		"gemini_timeout_secs",
		"gemini_prompt_prefix",
		"kimi_command",
		"kimi_timeout_secs",
		"kimi_prompt_prefix",
		"runtime_http_addr",
		"runtime_http_token",
		"failure_message",
		"thinking_message",
		"image_generation",
		"alice_home",
		"workspace_dir",
		"prompt_dir",
		"codex_home",
		"soul_path",
		"env",
		"permissions",
		"queue_capacity",
		"worker_concurrency",
		"automation_task_timeout_secs",
	}
	setKeys := make([]string, 0, len(legacyKeys))
	for _, key := range legacyKeys {
		if v.InConfig(key) {
			setKeys = append(setKeys, key)
		}
	}
	if len(setKeys) == 0 {
		return nil
	}
	sort.Strings(setKeys)
	return fmt.Errorf(
		"root bot keys are no longer supported: %s; move them under bots.<id>",
		strings.Join(setKeys, ", "),
	)
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

func normalizeImageGenerationConfig(in ImageGenerationConfig) ImageGenerationConfig {
	in.Provider = strings.ToLower(strings.TrimSpace(in.Provider))
	in.Model = strings.TrimSpace(in.Model)
	in.BaseURL = strings.TrimSpace(in.BaseURL)
	in.Moderation = strings.ToLower(strings.TrimSpace(in.Moderation))
	in.ResponseFormat = strings.ToLower(strings.TrimSpace(in.ResponseFormat))
	in.Size = strings.ToLower(strings.TrimSpace(in.Size))
	in.Quality = strings.ToLower(strings.TrimSpace(in.Quality))
	in.Background = strings.ToLower(strings.TrimSpace(in.Background))
	in.OutputFormat = strings.ToLower(strings.TrimSpace(in.OutputFormat))
	in.Style = strings.ToLower(strings.TrimSpace(in.Style))
	in.InputFidelity = strings.ToLower(strings.TrimSpace(in.InputFidelity))
	in.MaskPath = strings.TrimSpace(in.MaskPath)
	return in
}

func rejectRemovedImageProxyConfig(v *viper.Viper) error {
	if v == nil {
		return nil
	}
	if v.IsSet("image_generation.proxy") {
		return errors.New("image_generation.proxy has been removed; use env.OPENAI_*_PROXY instead")
	}
	for botID := range v.GetStringMap("bots") {
		if v.IsSet(fmt.Sprintf("bots.%s.image_generation.proxy", botID)) {
			return fmt.Errorf("bots.%s.image_generation.proxy has been removed; use bots.%s.env.OPENAI_*_PROXY instead", botID, botID)
		}
	}
	return nil
}

func applyDefaultCodexEnv(in map[string]string) map[string]string {
	out := normalizeEnvMap(in)
	if _, ok := out["HTTPS_PROXY"]; !ok {
		out["HTTPS_PROXY"] = DefaultHTTPSProxy
	}
	if _, ok := out["ALL_PROXY"]; !ok {
		out["ALL_PROXY"] = DefaultALLProxy
	}
	return out
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
		profile.Model = strings.TrimSpace(profile.Model)
		profile.Profile = strings.TrimSpace(profile.Profile)
		profile.ReasoningEffort = strings.ToLower(strings.TrimSpace(profile.ReasoningEffort))
		profile.Personality = strings.ToLower(strings.TrimSpace(profile.Personality))
		out[name] = profile
	}
	return out
}

func normalizeLLMProvider(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func isSupportedLLMProvider(provider string) bool {
	switch normalizeLLMProvider(provider) {
	case "", DefaultLLMProvider, LLMProviderClaude, LLMProviderGemini, LLMProviderKimi:
		return true
	default:
		return false
	}
}

func collectResolvedSceneProfileProviders(cfg Config) []string {
	names := make([]string, 0, 2)
	if cfg.GroupScenes.Chat.Enabled {
		names = append(names, strings.TrimSpace(cfg.GroupScenes.Chat.LLMProfile))
	}
	if cfg.GroupScenes.Work.Enabled {
		names = append(names, strings.TrimSpace(cfg.GroupScenes.Work.LLMProfile))
	}
	if len(names) == 0 {
		for name := range cfg.LLMProfiles {
			names = append(names, name)
		}
		sort.Strings(names)
	}

	providers := make([]string, 0, len(names))
	seenProviders := map[string]struct{}{}
	for _, name := range names {
		if name == "" {
			continue
		}
		profile, ok := cfg.LLMProfiles[name]
		if !ok {
			continue
		}
		provider := normalizeLLMProvider(profile.Provider)
		if provider == "" {
			continue
		}
		if _, exists := seenProviders[provider]; exists {
			continue
		}
		seenProviders[provider] = struct{}{}
		providers = append(providers, provider)
	}
	sort.Strings(providers)
	return providers
}

func resolveLLMProvider(cfg Config) (string, error) {
	explicit := normalizeLLMProvider(cfg.LLMProvider)
	if explicit != "" && !isSupportedLLMProvider(explicit) {
		return "", fmt.Errorf("unsupported llm_provider %q", explicit)
	}

	for name, profile := range cfg.LLMProfiles {
		if !isSupportedLLMProvider(profile.Provider) {
			return "", fmt.Errorf("llm_profiles.%s.provider %q is unsupported", name, profile.Provider)
		}
	}

	if explicit != "" {
		return explicit, nil
	}
	providers := collectResolvedSceneProfileProviders(cfg)
	if len(providers) == 1 {
		return providers[0], nil
	}
	return DefaultLLMProvider, nil
}

func (cfg Config) ResolvedLLMProviders() []string {
	defaultProvider := normalizeLLMProvider(cfg.LLMProvider)
	if defaultProvider == "" {
		defaultProvider = DefaultLLMProvider
	}
	set := map[string]struct{}{
		defaultProvider: {},
	}
	for _, profile := range cfg.LLMProfiles {
		provider := normalizeLLMProvider(profile.Provider)
		if provider == "" {
			continue
		}
		set[provider] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for provider := range set {
		out = append(out, provider)
	}
	sort.Strings(out)
	return out
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

type baseConfigValidation struct {
	QueueCapacity             int `validate:"gt=0"`
	WorkerConcurrency         int `validate:"gt=0"`
	AutomationTaskTimeoutSecs int `validate:"gt=0"`
}

func validateBaseConfig(cfg Config, requireCredentials bool) error {
	base := baseConfigValidation{
		QueueCapacity:             cfg.QueueCapacity,
		WorkerConcurrency:         cfg.WorkerConcurrency,
		AutomationTaskTimeoutSecs: cfg.AutomationTaskTimeoutSecs,
	}
	if requireCredentials {
		if strings.TrimSpace(cfg.FeishuAppID) == "" {
			return errors.New("feishu_app_id is required")
		}
		if strings.TrimSpace(cfg.FeishuAppSecret) == "" {
			return errors.New("feishu_app_secret is required")
		}
	}
	if err := configValidator.Struct(base); err != nil {
		var validationErrs validator.ValidationErrors
		if !errors.As(err, &validationErrs) {
			return fmt.Errorf("validate config failed: %w", err)
		}
		for _, validationErr := range validationErrs {
			switch validationErr.Field() {
			case "QueueCapacity":
				return errors.New("queue_capacity must be > 0")
			case "WorkerConcurrency":
				return errors.New("worker_concurrency must be > 0")
			case "AutomationTaskTimeoutSecs":
				return errors.New("automation_task_timeout_secs must be > 0")
			}
		}
		return err
	}
	for name, profile := range cfg.LLMProfiles {
		switch profile.Provider {
		case "", DefaultLLMProvider, LLMProviderClaude, LLMProviderGemini, LLMProviderKimi:
		default:
			return fmt.Errorf("llm_profiles.%s.provider %q is unsupported", name, profile.Provider)
		}
	}
	switch cfg.ImageGeneration.Provider {
	case "", "openai":
	default:
		return fmt.Errorf("image_generation.provider %q is unsupported", cfg.ImageGeneration.Provider)
	}
	if cfg.ImageGeneration.TimeoutSecs <= 0 {
		return errors.New("image_generation.timeout_secs must be > 0")
	}
	if cfg.ImageGeneration.N < 0 {
		return errors.New("image_generation.n must be >= 0")
	}
	if cfg.ImageGeneration.OutputCompression < -1 || cfg.ImageGeneration.OutputCompression > 100 {
		return errors.New("image_generation.output_compression must be between 0 and 100, or -1 to leave unset")
	}
	if cfg.ImageGeneration.PartialImages < -1 || cfg.ImageGeneration.PartialImages > 3 {
		return errors.New("image_generation.partial_images must be between 0 and 3, or -1 to leave unset")
	}
	if cfg.GroupScenes.Chat.Enabled {
		if cfg.GroupScenes.Chat.LLMProfile == "" {
			return errors.New("group_scenes.chat.llm_profile is required when chat scene is enabled")
		}
		if _, ok := cfg.LLMProfiles[cfg.GroupScenes.Chat.LLMProfile]; !ok {
			return fmt.Errorf("group_scenes.chat.llm_profile %q is undefined", cfg.GroupScenes.Chat.LLMProfile)
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
		if _, ok := cfg.LLMProfiles[cfg.GroupScenes.Work.LLMProfile]; !ok {
			return fmt.Errorf("group_scenes.work.llm_profile %q is undefined", cfg.GroupScenes.Work.LLMProfile)
		}
		if cfg.GroupScenes.Work.SessionScope != GroupSceneSessionPerThread {
			return fmt.Errorf("group_scenes.work.session_scope must be %q", GroupSceneSessionPerThread)
		}
	}
	return nil
}
