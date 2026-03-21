package config

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/spf13/viper"
)

const DefaultLLMProvider = "codex"
const LLMProviderClaude = "claude"
const LLMProviderKimi = "kimi"
const TriggerModeAt = "at"
const TriggerModePrefix = "prefix"
const ImmediateFeedbackModeReply = "reply"
const ImmediateFeedbackModeReaction = "reaction"
const DefaultImmediateFeedbackReaction = "SMILE"

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
	KimiCommand               string                      `mapstructure:"kimi_command"`
	KimiTimeoutSecs           int                         `mapstructure:"kimi_timeout_secs"`
	KimiPromptPrefix          string                      `mapstructure:"kimi_prompt_prefix"`
	RuntimeHTTPAddr           string                      `mapstructure:"runtime_http_addr"`
	RuntimeHTTPToken          string                      `mapstructure:"runtime_http_token"`
	FailureMessage            string                      `mapstructure:"failure_message"`
	ThinkingMessage           string                      `mapstructure:"thinking_message"`
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

	CodexCommand         string               `mapstructure:"codex_command"`
	CodexTimeout         time.Duration        `mapstructure:"-"`
	CodexTimeoutSecs     int                  `mapstructure:"codex_timeout_secs"`
	CodexModel           string               `mapstructure:"codex_model"`
	CodexReasoningEffort string               `mapstructure:"codex_model_reasoning_effort"`
	CodexEnv             map[string]string    `mapstructure:"env"`
	CodexPromptPrefix    string               `mapstructure:"codex_prompt_prefix"`
	ClaudeCommand        string               `mapstructure:"claude_command"`
	ClaudeTimeout        time.Duration        `mapstructure:"-"`
	ClaudeTimeoutSecs    int                  `mapstructure:"claude_timeout_secs"`
	ClaudePromptPrefix   string               `mapstructure:"claude_prompt_prefix"`
	KimiCommand          string               `mapstructure:"kimi_command"`
	KimiTimeout          time.Duration        `mapstructure:"-"`
	KimiTimeoutSecs      int                  `mapstructure:"kimi_timeout_secs"`
	KimiPromptPrefix     string               `mapstructure:"kimi_prompt_prefix"`
	RuntimeHTTPAddr      string               `mapstructure:"runtime_http_addr"`
	RuntimeHTTPToken     string               `mapstructure:"runtime_http_token"`
	FailureMessage       string               `mapstructure:"failure_message"`
	ThinkingMessage      string               `mapstructure:"thinking_message"`
	AliceHome            string               `mapstructure:"alice_home"`
	WorkspaceDir         string               `mapstructure:"workspace_dir"`
	PromptDir            string               `mapstructure:"prompt_dir"`
	CodexHome            string               `mapstructure:"codex_home"`
	SoulPath             string               `mapstructure:"soul_path"`
	Permissions          BotPermissionsConfig `mapstructure:"permissions"`
	PrimaryBotID         string               `mapstructure:"primary_bot"`
	Bots                 map[string]BotConfig `mapstructure:"bots"`

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
	v.SetDefault("immediate_feedback_mode", ImmediateFeedbackModeReply)
	v.SetDefault("immediate_feedback_reaction", DefaultImmediateFeedbackReaction)
	v.SetDefault("llm_provider", DefaultLLMProvider)
	v.SetDefault("codex_command", "codex")
	v.SetDefault("codex_timeout_secs", 172800)
	v.SetDefault("codex_model", "")
	v.SetDefault("codex_model_reasoning_effort", "")
	v.SetDefault("claude_command", "claude")
	v.SetDefault("claude_timeout_secs", 172800)
	v.SetDefault("kimi_command", "kimi")
	v.SetDefault("kimi_timeout_secs", 172800)
	v.SetDefault("runtime_http_addr", "127.0.0.1:7331")
	v.SetDefault("runtime_http_token", "")
	v.SetDefault("failure_message", "Codex 暂时不可用，请稍后重试。")
	v.SetDefault("thinking_message", "正在思考中...")
	v.SetDefault("alice_home", AliceHomeDir())
	v.SetDefault("workspace_dir", "")
	v.SetDefault("prompt_dir", "")
	v.SetDefault("codex_home", "")
	v.SetDefault("soul_path", "")
	v.SetDefault("bot_name", "")
	v.SetDefault("primary_bot", "")
	v.SetDefault("permissions.runtime_message", true)
	v.SetDefault("permissions.runtime_automation", true)
	v.SetDefault("permissions.runtime_campaigns", true)
	v.SetDefault("permissions.codex.chat.sandbox", "workspace-write")
	v.SetDefault("permissions.codex.chat.ask_for_approval", "never")
	v.SetDefault("permissions.codex.work.sandbox", "danger-full-access")
	v.SetDefault("permissions.codex.work.ask_for_approval", "never")
	v.SetDefault("queue_capacity", 256)
	v.SetDefault("worker_concurrency", 1)
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
	cfg.KimiCommand = strings.TrimSpace(cfg.KimiCommand)
	cfg.KimiPromptPrefix = strings.TrimSpace(cfg.KimiPromptPrefix)
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
	cfg.PrimaryBotID = strings.TrimSpace(cfg.PrimaryBotID)
	cfg.Permissions = normalizeBotPermissions(cfg.Permissions)
	cfg.Bots = normalizeBots(cfg.Bots)
	cfg.LogLevel = strings.ToLower(strings.TrimSpace(cfg.LogLevel))
	cfg.LogFile = strings.TrimSpace(cfg.LogFile)

	return finalizeConfig(cfg, len(cfg.Bots) == 0)
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
	FeishuAppID               string `validate:"required"`
	FeishuAppSecret           string `validate:"required"`
	QueueCapacity             int    `validate:"gt=0"`
	WorkerConcurrency         int    `validate:"gt=0"`
	AutomationTaskTimeoutSecs int    `validate:"gt=0"`
}

func validateBaseConfig(cfg Config, requireCredentials bool) error {
	base := baseConfigValidation{
		QueueCapacity:             cfg.QueueCapacity,
		WorkerConcurrency:         cfg.WorkerConcurrency,
		AutomationTaskTimeoutSecs: cfg.AutomationTaskTimeoutSecs,
	}
	if requireCredentials {
		base.FeishuAppID = cfg.FeishuAppID
		base.FeishuAppSecret = cfg.FeishuAppSecret
	}
	if err := configValidator.Struct(base); err != nil {
		var validationErrs validator.ValidationErrors
		if !errors.As(err, &validationErrs) {
			return fmt.Errorf("validate config failed: %w", err)
		}
		for _, validationErr := range validationErrs {
			switch validationErr.Field() {
			case "FeishuAppID":
				return errors.New("feishu_app_id is required")
			case "FeishuAppSecret":
				return errors.New("feishu_app_secret is required")
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
		case "", DefaultLLMProvider, LLMProviderClaude, LLMProviderKimi:
		default:
			return fmt.Errorf("llm_profiles.%s.provider %q is unsupported", name, profile.Provider)
		}
	}
	if cfg.GroupScenes.Chat.Enabled {
		if cfg.GroupScenes.Chat.LLMProfile == "" {
			return errors.New("group_scenes.chat.llm_profile is required when chat scene is enabled")
		}
		profile, ok := cfg.LLMProfiles[cfg.GroupScenes.Chat.LLMProfile]
		if !ok {
			return fmt.Errorf("group_scenes.chat.llm_profile %q is undefined", cfg.GroupScenes.Chat.LLMProfile)
		}
		if profile.Provider != "" && profile.Provider != cfg.LLMProvider {
			return fmt.Errorf("group_scenes.chat.llm_profile %q provider %q does not match current llm_provider %q", cfg.GroupScenes.Chat.LLMProfile, profile.Provider, cfg.LLMProvider)
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
		profile, ok := cfg.LLMProfiles[cfg.GroupScenes.Work.LLMProfile]
		if !ok {
			return fmt.Errorf("group_scenes.work.llm_profile %q is undefined", cfg.GroupScenes.Work.LLMProfile)
		}
		if profile.Provider != "" && profile.Provider != cfg.LLMProvider {
			return fmt.Errorf("group_scenes.work.llm_profile %q provider %q does not match current llm_provider %q", cfg.GroupScenes.Work.LLMProfile, profile.Provider, cfg.LLMProvider)
		}
		if cfg.GroupScenes.Work.SessionScope != GroupSceneSessionPerThread {
			return fmt.Errorf("group_scenes.work.session_scope must be %q", GroupSceneSessionPerThread)
		}
	}
	return nil
}
