package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
	"go.yaml.in/yaml/v3"
)

const DefaultConfigPath = "config.yaml"
const DefaultLLMProvider = "codex"

type Config struct {
	FeishuAppID     string `mapstructure:"feishu_app_id"`
	FeishuAppSecret string `mapstructure:"feishu_app_secret"`
	FeishuBaseURL   string `mapstructure:"feishu_base_url"`
	FeishuBotOpenID string `mapstructure:"feishu_bot_open_id"`
	FeishuBotUserID string `mapstructure:"feishu_bot_user_id"`

	LLMProvider string `mapstructure:"llm_provider"`

	CodexCommand           string            `mapstructure:"codex_command"`
	CodexTimeout           time.Duration     `mapstructure:"-"`
	CodexTimeoutSecs       int               `mapstructure:"codex_timeout_secs"`
	CodexEnv               map[string]string `mapstructure:"env"`
	CodexPromptPrefix      string            `mapstructure:"codex_prompt_prefix"`
	CodexMCPAutoRegister   bool              `mapstructure:"codex_mcp_auto_register"`
	CodexMCPRegisterStrict bool              `mapstructure:"codex_mcp_register_strict"`
	CodexMCPServerName     string            `mapstructure:"codex_mcp_server_name"`
	FailureMessage         string            `mapstructure:"failure_message"`
	ThinkingMessage        string            `mapstructure:"thinking_message"`
	WorkspaceDir           string            `mapstructure:"workspace_dir"`
	MemoryDir              string            `mapstructure:"memory_dir"`

	QueueCapacity     int `mapstructure:"queue_capacity"`
	WorkerConcurrency int `mapstructure:"worker_concurrency"`
	IdleSummaryHours  int `mapstructure:"idle_summary_hours"`
	IdleSummaryIdle   time.Duration

	GroupContextWindowMinutes int           `mapstructure:"group_context_window_minutes"`
	GroupContextWindowTTL     time.Duration `mapstructure:"-"`

	LogLevel string `mapstructure:"log_level"`
}

func LoadFromFile(path string) (Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")
	v.SetDefault("feishu_base_url", "https://open.feishu.cn")
	v.SetDefault("llm_provider", DefaultLLMProvider)
	v.SetDefault("codex_command", "codex")
	v.SetDefault("codex_timeout_secs", 120)
	v.SetDefault("codex_mcp_auto_register", true)
	v.SetDefault("codex_mcp_register_strict", false)
	v.SetDefault("codex_mcp_server_name", "alice-feishu")
	v.SetDefault("failure_message", "Codex 暂时不可用，请稍后重试。")
	v.SetDefault("thinking_message", "正在思考中...")
	v.SetDefault("workspace_dir", ".")
	v.SetDefault("memory_dir", ".memory")
	v.SetDefault("queue_capacity", 256)
	v.SetDefault("worker_concurrency", 1)
	v.SetDefault("idle_summary_hours", 8)
	v.SetDefault("group_context_window_minutes", 5)
	v.SetDefault("log_level", "info")

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
	cfg.LLMProvider = strings.ToLower(strings.TrimSpace(cfg.LLMProvider))
	cfg.CodexCommand = strings.TrimSpace(cfg.CodexCommand)
	cfg.CodexEnv = normalizeEnvMap(envMap)
	cfg.CodexPromptPrefix = strings.TrimSpace(cfg.CodexPromptPrefix)
	cfg.CodexMCPServerName = strings.TrimSpace(cfg.CodexMCPServerName)
	cfg.FailureMessage = strings.TrimSpace(cfg.FailureMessage)
	cfg.ThinkingMessage = strings.TrimSpace(cfg.ThinkingMessage)
	cfg.WorkspaceDir = strings.TrimSpace(cfg.WorkspaceDir)
	cfg.MemoryDir = strings.TrimSpace(cfg.MemoryDir)
	cfg.LogLevel = strings.ToLower(strings.TrimSpace(cfg.LogLevel))

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
	if cfg.CodexCommand == "" {
		cfg.CodexCommand = "codex"
	}
	if cfg.CodexMCPServerName == "" {
		cfg.CodexMCPServerName = "alice-feishu"
	}
	if cfg.WorkspaceDir == "" {
		cfg.WorkspaceDir = "."
	}
	if cfg.MemoryDir == "" {
		cfg.MemoryDir = ".memory"
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
	if cfg.LLMProvider != DefaultLLMProvider {
		return Config{}, fmt.Errorf("unsupported llm_provider %q", cfg.LLMProvider)
	}

	if cfg.CodexTimeoutSecs <= 0 {
		return Config{}, errors.New("codex_timeout_secs must be > 0")
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
	if cfg.IdleSummaryHours <= 0 {
		return Config{}, errors.New("idle_summary_hours must be > 0")
	}
	if cfg.GroupContextWindowMinutes <= 0 {
		return Config{}, errors.New("group_context_window_minutes must be > 0")
	}
	cfg.CodexTimeout = time.Duration(cfg.CodexTimeoutSecs) * time.Second
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
