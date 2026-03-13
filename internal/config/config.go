package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

const DefaultConfigPath = "config.yaml"
const DefaultLLMProvider = "codex"
const LLMProviderClaude = "claude"
const LLMProviderKimi = "kimi"
const TriggerModeAt = "at"
const TriggerModeActive = "active"
const TriggerModePrefix = "prefix"
const ImmediateFeedbackModeReply = "reply"
const ImmediateFeedbackModeReaction = "reaction"
const DefaultImmediateFeedbackReaction = "SMILE"

type Config struct {
	FeishuAppID               string `mapstructure:"feishu_app_id"`
	FeishuAppSecret           string `mapstructure:"feishu_app_secret"`
	FeishuBaseURL             string `mapstructure:"feishu_base_url"`
	FeishuBotOpenID           string `mapstructure:"feishu_bot_open_id"`
	FeishuBotUserID           string `mapstructure:"feishu_bot_user_id"`
	TriggerMode               string `mapstructure:"trigger_mode"`
	TriggerPrefix             string `mapstructure:"trigger_prefix"`
	ImmediateFeedbackMode     string `mapstructure:"immediate_feedback_mode"`
	ImmediateFeedbackReaction string `mapstructure:"immediate_feedback_reaction"`

	LLMProvider string `mapstructure:"llm_provider"`

	CodexCommand           string            `mapstructure:"codex_command"`
	CodexTimeout           time.Duration     `mapstructure:"-"`
	CodexTimeoutSecs       int               `mapstructure:"codex_timeout_secs"`
	CodexEnv               map[string]string `mapstructure:"env"`
	CodexPromptPrefix      string            `mapstructure:"codex_prompt_prefix"`
	CodexMCPAutoRegister   bool              `mapstructure:"codex_mcp_auto_register"`
	CodexMCPRegisterStrict bool              `mapstructure:"codex_mcp_register_strict"`
	CodexMCPServerName     string            `mapstructure:"codex_mcp_server_name"`
	ClaudeCommand          string            `mapstructure:"claude_command"`
	ClaudeTimeout          time.Duration     `mapstructure:"-"`
	ClaudeTimeoutSecs      int               `mapstructure:"claude_timeout_secs"`
	ClaudePromptPrefix     string            `mapstructure:"claude_prompt_prefix"`
	KimiCommand            string            `mapstructure:"kimi_command"`
	KimiTimeout            time.Duration     `mapstructure:"-"`
	KimiTimeoutSecs        int               `mapstructure:"kimi_timeout_secs"`
	KimiPromptPrefix       string            `mapstructure:"kimi_prompt_prefix"`
	RuntimeHTTPAddr        string            `mapstructure:"runtime_http_addr"`
	RuntimeHTTPToken       string            `mapstructure:"runtime_http_token"`
	FailureMessage         string            `mapstructure:"failure_message"`
	ThinkingMessage        string            `mapstructure:"thinking_message"`
	WorkspaceDir           string            `mapstructure:"workspace_dir"`
	MemoryDir              string            `mapstructure:"memory_dir"`
	PromptDir              string            `mapstructure:"prompt_dir"`

	QueueCapacity             int           `mapstructure:"queue_capacity"`
	WorkerConcurrency         int           `mapstructure:"worker_concurrency"`
	AutomationTaskTimeoutSecs int           `mapstructure:"automation_task_timeout_secs"`
	AutomationTaskTimeout     time.Duration `mapstructure:"-"`
	IdleSummaryHours          int           `mapstructure:"idle_summary_hours"`
	IdleSummaryIdle           time.Duration

	GroupContextWindowMinutes int           `mapstructure:"group_context_window_minutes"`
	GroupContextWindowTTL     time.Duration `mapstructure:"-"`

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
	v.SetDefault("codex_timeout_secs", 120)
	v.SetDefault("codex_mcp_auto_register", true)
	v.SetDefault("codex_mcp_register_strict", false)
	v.SetDefault("codex_mcp_server_name", "alice-feishu")
	v.SetDefault("claude_command", "claude")
	v.SetDefault("claude_timeout_secs", 120)
	v.SetDefault("kimi_command", "kimi")
	v.SetDefault("kimi_timeout_secs", 120)
	v.SetDefault("runtime_http_addr", "127.0.0.1:7331")
	v.SetDefault("runtime_http_token", "")
	v.SetDefault("failure_message", "Codex 暂时不可用，请稍后重试。")
	v.SetDefault("thinking_message", "正在思考中...")
	v.SetDefault("workspace_dir", ".")
	v.SetDefault("memory_dir", ".memory")
	v.SetDefault("prompt_dir", "prompts")
	v.SetDefault("queue_capacity", 256)
	v.SetDefault("worker_concurrency", 1)
	v.SetDefault("automation_task_timeout_secs", 600)
	v.SetDefault("idle_summary_hours", 8)
	v.SetDefault("group_context_window_minutes", 5)
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
	envMap, err := loadEnvFromYAML(path)
	if err != nil {
		return Config{}, err
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
	cfg.CodexCommand = strings.TrimSpace(cfg.CodexCommand)
	cfg.CodexEnv = normalizeEnvMap(envMap)
	cfg.CodexPromptPrefix = strings.TrimSpace(cfg.CodexPromptPrefix)
	cfg.CodexMCPServerName = strings.TrimSpace(cfg.CodexMCPServerName)
	cfg.ClaudeCommand = strings.TrimSpace(cfg.ClaudeCommand)
	cfg.ClaudePromptPrefix = strings.TrimSpace(cfg.ClaudePromptPrefix)
	cfg.KimiCommand = strings.TrimSpace(cfg.KimiCommand)
	cfg.KimiPromptPrefix = strings.TrimSpace(cfg.KimiPromptPrefix)
	cfg.RuntimeHTTPAddr = strings.TrimSpace(cfg.RuntimeHTTPAddr)
	cfg.RuntimeHTTPToken = strings.TrimSpace(cfg.RuntimeHTTPToken)
	cfg.FailureMessage = strings.TrimSpace(cfg.FailureMessage)
	cfg.ThinkingMessage = strings.TrimSpace(cfg.ThinkingMessage)
	cfg.WorkspaceDir = strings.TrimSpace(cfg.WorkspaceDir)
	cfg.MemoryDir = strings.TrimSpace(cfg.MemoryDir)
	cfg.PromptDir = strings.TrimSpace(cfg.PromptDir)
	cfg.LogLevel = strings.ToLower(strings.TrimSpace(cfg.LogLevel))
	cfg.LogFile = strings.TrimSpace(cfg.LogFile)

	if cfg.FeishuAppID == "" {
		return Config{}, errors.New("feishu_app_id is required")
	}
	if cfg.FeishuAppSecret == "" {
		return Config{}, errors.New("feishu_app_secret is required")
	}
	if cfg.FeishuBaseURL == "" {
		cfg.FeishuBaseURL = "https://open.feishu.cn"
	}
	if cfg.LLMProvider == "" {
		cfg.LLMProvider = DefaultLLMProvider
	}
	if cfg.TriggerMode == "" {
		cfg.TriggerMode = TriggerModeAt
	}
	if cfg.ImmediateFeedbackMode == "" {
		cfg.ImmediateFeedbackMode = ImmediateFeedbackModeReply
	}
	if cfg.ImmediateFeedbackReaction == "" {
		cfg.ImmediateFeedbackReaction = DefaultImmediateFeedbackReaction
	}
	if cfg.CodexCommand == "" {
		cfg.CodexCommand = "codex"
	}
	if cfg.ClaudeCommand == "" {
		cfg.ClaudeCommand = "claude"
	}
	if cfg.KimiCommand == "" {
		cfg.KimiCommand = "kimi"
	}
	if cfg.CodexMCPServerName == "" {
		cfg.CodexMCPServerName = "alice-feishu"
	}
	if cfg.RuntimeHTTPAddr == "" {
		cfg.RuntimeHTTPAddr = "127.0.0.1:7331"
	}
	if cfg.WorkspaceDir == "" {
		cfg.WorkspaceDir = "."
	}
	if cfg.MemoryDir == "" {
		cfg.MemoryDir = ".memory"
	}
	if cfg.PromptDir == "" {
		cfg.PromptDir = "prompts"
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}
	if cfg.FailureMessage == "" {
		cfg.FailureMessage = "Codex 暂时不可用，请稍后重试。"
	}
	if cfg.ThinkingMessage == "" {
		cfg.ThinkingMessage = "正在思考中..."
	}
	switch cfg.LLMProvider {
	case DefaultLLMProvider, LLMProviderClaude, LLMProviderKimi:
	default:
		return Config{}, fmt.Errorf("unsupported llm_provider %q", cfg.LLMProvider)
	}
	switch cfg.TriggerMode {
	case TriggerModeAt, TriggerModeActive, TriggerModePrefix:
	default:
		return Config{}, fmt.Errorf("unsupported trigger_mode %q", cfg.TriggerMode)
	}
	switch cfg.ImmediateFeedbackMode {
	case ImmediateFeedbackModeReply, ImmediateFeedbackModeReaction:
	default:
		return Config{}, fmt.Errorf("unsupported immediate_feedback_mode %q", cfg.ImmediateFeedbackMode)
	}
	if cfg.TriggerMode == TriggerModePrefix && cfg.TriggerPrefix == "" {
		return Config{}, errors.New("trigger_prefix is required when trigger_mode is prefix")
	}

	if cfg.CodexTimeoutSecs <= 0 {
		if cfg.LLMProvider == DefaultLLMProvider {
			return Config{}, errors.New("codex_timeout_secs must be > 0")
		}
		cfg.CodexTimeoutSecs = 120
	}
	if cfg.ClaudeTimeoutSecs <= 0 {
		if cfg.LLMProvider == LLMProviderClaude {
			return Config{}, errors.New("claude_timeout_secs must be > 0")
		}
		cfg.ClaudeTimeoutSecs = 120
	}
	if cfg.KimiTimeoutSecs <= 0 {
		if cfg.LLMProvider == LLMProviderKimi {
			return Config{}, errors.New("kimi_timeout_secs must be > 0")
		}
		cfg.KimiTimeoutSecs = 120
	}
	for key := range cfg.CodexEnv {
		if key == "" {
			return Config{}, errors.New("env key must not be empty")
		}
		if strings.ContainsRune(key, '=') {
			return Config{}, fmt.Errorf("env key %q must not contain '='", key)
		}
	}
	if cfg.QueueCapacity <= 0 {
		return Config{}, errors.New("queue_capacity must be > 0")
	}
	if cfg.WorkerConcurrency <= 0 {
		return Config{}, errors.New("worker_concurrency must be > 0")
	}
	if cfg.AutomationTaskTimeoutSecs <= 0 {
		return Config{}, errors.New("automation_task_timeout_secs must be > 0")
	}
	if cfg.IdleSummaryHours <= 0 {
		return Config{}, errors.New("idle_summary_hours must be > 0")
	}
	if cfg.GroupContextWindowMinutes <= 0 {
		return Config{}, errors.New("group_context_window_minutes must be > 0")
	}
	if cfg.LogMaxSizeMB <= 0 {
		cfg.LogMaxSizeMB = 20
	}
	if cfg.LogMaxBackups <= 0 {
		cfg.LogMaxBackups = 5
	}
	if cfg.LogMaxAgeDays <= 0 {
		cfg.LogMaxAgeDays = 7
	}
	cfg.CodexTimeout = time.Duration(cfg.CodexTimeoutSecs) * time.Second
	cfg.ClaudeTimeout = time.Duration(cfg.ClaudeTimeoutSecs) * time.Second
	cfg.KimiTimeout = time.Duration(cfg.KimiTimeoutSecs) * time.Second
	cfg.AutomationTaskTimeout = time.Duration(cfg.AutomationTaskTimeoutSecs) * time.Second
	cfg.IdleSummaryIdle = time.Duration(cfg.IdleSummaryHours) * time.Hour
	cfg.GroupContextWindowTTL = time.Duration(cfg.GroupContextWindowMinutes) * time.Minute

	return cfg, nil
}

func normalizeEnvMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}

	out := make(map[string]string, len(in))
	for key, value := range in {
		out[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return out
}

func loadEnvFromYAML(path string) (map[string]string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file %q failed: %w", path, err)
	}

	var raw struct {
		Env map[string]string `yaml:"env"`
	}
	if err := yaml.Unmarshal(content, &raw); err != nil {
		return nil, fmt.Errorf("parse config yaml failed: %w", err)
	}
	return raw.Env, nil
}
