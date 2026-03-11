package platform

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config holds all application configuration.
type Config struct {
	HTTP      HTTPConfig      `mapstructure:"http"`
	Storage   StorageConfig   `mapstructure:"storage"`
	Runtime   RuntimeConfig   `mapstructure:"runtime"`
	Promotion PromotionConfig `mapstructure:"promotion"`
	Workflow  WorkflowConfig  `mapstructure:"workflow"`
	MCP       MCPConfig       `mapstructure:"mcp"`
	Scheduler SchedulerConfig `mapstructure:"scheduler"`
	Ops       OpsConfig       `mapstructure:"ops"`
	Auth      AuthConfig      `mapstructure:"auth"`
	Agent     AgentConfig     `mapstructure:"agent"`
	Logging   LoggingConfig   `mapstructure:"logging"`
}

type HTTPConfig struct {
	ListenAddr string `mapstructure:"listen_addr"`
}

type StorageConfig struct {
	RootDir          string `mapstructure:"root_dir"`
	SnapshotInterval int    `mapstructure:"snapshot_interval"`
}

type RuntimeConfig struct {
	ShardCount    int `mapstructure:"shard_count"`
	OutboxWorkers int `mapstructure:"outbox_workers"`
}

type PromotionConfig struct {
	MinConfidence float64 `mapstructure:"min_confidence"`
}

type WorkflowConfig struct {
	ManifestRoots []string `mapstructure:"manifest_roots"`
}

type MCPConfig struct {
	Domains map[string]MCPDomainConfig `mapstructure:"domains"`
}

type MCPDomainConfig struct {
	BaseURL string `mapstructure:"base_url"`
}

type SchedulerConfig struct {
	PollInterval string `mapstructure:"poll_interval"`
}

type OpsConfig struct {
	MetricsEnabled                 bool `mapstructure:"metrics_enabled"`
	AdminEventInjectionEnabled     bool `mapstructure:"admin_event_injection_enabled"`
	AdminScheduleFireReplayEnabled bool `mapstructure:"admin_schedule_fire_replay_enabled"`
}

type AuthConfig struct {
	AdminToken             string `mapstructure:"admin_token"`
	HumanActionSecret      string `mapstructure:"human_action_secret"`
	GitHubWebhookSecret    string `mapstructure:"github_webhook_secret"`
	GitLabWebhookSecret    string `mapstructure:"gitlab_webhook_secret"`
	SchedulerIngressSecret string `mapstructure:"scheduler_ingress_secret"`
}

type AgentConfig struct {
	KimiExecutable     string `mapstructure:"kimi_executable"`
	WorkDir            string `mapstructure:"work_dir"`
	Timeout            string `mapstructure:"timeout"`
	MaxSteps           int    `mapstructure:"max_steps"`
	SkillsDir          string `mapstructure:"skills_dir"`
	EnableDirectAnswer bool   `mapstructure:"enable_direct_answer"`
}

type FileLogConfig struct {
	Path       string `mapstructure:"path"`
	MaxSizeMB  int    `mapstructure:"max_size_mb"`
	MaxBackups int    `mapstructure:"max_backups"`
	MaxAgeDays int    `mapstructure:"max_age_days"`
	Compress   bool   `mapstructure:"compress"`
}

// LoadConfig loads configuration from file and environment variables.
func LoadConfig(path string) (*Config, error) {
	v := viper.New()

	// Set defaults
	setDefaults(v)

	// Set config file
	if path != "" {
		v.SetConfigFile(path)
		if err := v.ReadInConfig(); err != nil {
			// Config file is optional, but log if there's a real error
			if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
				return nil, fmt.Errorf("read config %s: %w", path, err)
			}
		}
	}

	// Environment variable binding
	bindEnvVars(v)

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	// Post-processing
	applyPostProcessing(&cfg)

	if err := validateConfig(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("http.listen_addr", ":8080")
	v.SetDefault("storage.root_dir", "data")
	v.SetDefault("storage.snapshot_interval", 100)
	v.SetDefault("runtime.shard_count", 16)
	v.SetDefault("runtime.outbox_workers", 4)
	v.SetDefault("promotion.min_confidence", 0.6)
	v.SetDefault("workflow.manifest_roots", []string{"configs/workflows"})
	v.SetDefault("scheduler.poll_interval", "30s")
	v.SetDefault("agent.kimi_executable", "kimi")
	v.SetDefault("agent.work_dir", ".")
	v.SetDefault("agent.timeout", "120s")
	v.SetDefault("agent.max_steps", 10)
	v.SetDefault("agent.skills_dir", "skills")
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "json")
	v.SetDefault("logging.console", true)
}

func bindEnvVars(v *viper.Viper) {
	// Bind environment variables with ALICE_ prefix
	v.SetEnvPrefix("ALICE")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	v.AutomaticEnv()

	// Explicit bindings for nested config
	bindings := map[string]string{
		"http.listen_addr":                       "HTTP_LISTEN_ADDR",
		"storage.root_dir":                       "STORAGE_ROOT_DIR",
		"storage.snapshot_interval":              "STORAGE_SNAPSHOT_INTERVAL",
		"runtime.shard_count":                    "RUNTIME_SHARD_COUNT",
		"runtime.outbox_workers":                 "RUNTIME_OUTBOX_WORKERS",
		"promotion.min_confidence":               "PROMOTION_MIN_CONFIDENCE",
		"workflow.manifest_roots":                "WORKFLOW_MANIFEST_ROOTS",
		"scheduler.poll_interval":                "SCHEDULER_POLL_INTERVAL",
		"auth.admin_token":                       "AUTH_ADMIN_TOKEN",
		"auth.human_action_secret":               "AUTH_HUMAN_ACTION_SECRET",
		"auth.github_webhook_secret":             "AUTH_GITHUB_WEBHOOK_SECRET",
		"auth.gitlab_webhook_secret":             "AUTH_GITLAB_WEBHOOK_SECRET",
		"auth.scheduler_ingress_secret":          "AUTH_SCHEDULER_INGRESS_SECRET",
		"ops.metrics_enabled":                    "OPS_METRICS_ENABLED",
		"ops.admin_event_injection_enabled":      "OPS_ADMIN_EVENT_INJECTION_ENABLED",
		"ops.admin_schedule_fire_replay_enabled": "OPS_ADMIN_SCHEDULE_FIRE_REPLAY_ENABLED",
		"agent.kimi_executable":                  "AGENT_KIMI_EXECUTABLE",
		"agent.work_dir":                         "AGENT_WORK_DIR",
		"agent.timeout":                          "AGENT_TIMEOUT",
		"agent.max_steps":                        "AGENT_MAX_STEPS",
		"agent.skills_dir":                       "AGENT_SKILLS_DIR",
		"agent.enable_direct_answer":             "AGENT_ENABLE_DIRECT_ANSWER",
		"logging.level":                          "LOGGING_LEVEL",
		"logging.format":                         "LOGGING_FORMAT",
		"logging.console":                        "LOGGING_CONSOLE",
		"logging.file.path":                      "LOGGING_FILE_PATH",
		"logging.file.max_size_mb":               "LOGGING_FILE_MAX_SIZE_MB",
		"logging.file.max_backups":               "LOGGING_FILE_MAX_BACKUPS",
		"logging.file.max_age_days":              "LOGGING_FILE_MAX_AGE_DAYS",
		"logging.file.compress":                  "LOGGING_FILE_COMPRESS",
	}

	for key, envVar := range bindings {
		_ = v.BindEnv(key, "ALICE_"+envVar)
	}
}

func applyPostProcessing(cfg *Config) {
	// Ensure manifest roots is not empty
	if len(cfg.Workflow.ManifestRoots) == 0 {
		cfg.Workflow.ManifestRoots = []string{"configs/workflows"}
	}

	// Parse manifest roots from comma-separated env if needed
	if roots := cfg.Workflow.ManifestRoots; len(roots) == 1 && strings.Contains(roots[0], ",") {
		cfg.Workflow.ManifestRoots = strings.Split(roots[0], ",")
	}

	// Ensure logging defaults
	if cfg.Logging.Level == "" {
		cfg.Logging.Level = "info"
	}
	if cfg.Logging.Format == "" {
		cfg.Logging.Format = "json"
	}
	if !cfg.Logging.Console && cfg.Logging.File == nil {
		cfg.Logging.Console = true
	}
}

func validateConfig(cfg *Config) error {
	if len(cfg.Workflow.ManifestRoots) == 0 {
		return fmt.Errorf("workflow.manifest_roots is required")
	}
	if cfg.Scheduler.PollInterval != "" {
		if _, err := time.ParseDuration(cfg.Scheduler.PollInterval); err != nil {
			return fmt.Errorf("invalid scheduler.poll_interval: %w", err)
		}
	}
	if cfg.Agent.Timeout != "" {
		if _, err := time.ParseDuration(cfg.Agent.Timeout); err != nil {
			return fmt.Errorf("invalid agent.timeout: %w", err)
		}
	}
	return nil
}
