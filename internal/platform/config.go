package platform

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	HTTP struct {
		ListenAddr string `yaml:"listen_addr"`
	} `yaml:"http"`
	Storage struct {
		RootDir          string `yaml:"root_dir"`
		SnapshotInterval int    `yaml:"snapshot_interval"`
	} `yaml:"storage"`
	Runtime struct {
		ShardCount    int `yaml:"shard_count"`
		OutboxWorkers int `yaml:"outbox_workers"`
	} `yaml:"runtime"`
	Promotion struct {
		MinConfidence float64 `yaml:"min_confidence"`
	} `yaml:"promotion"`
	Workflow struct {
		ManifestRoots []string `yaml:"manifest_roots"`
	} `yaml:"workflow"`
	MCP struct {
		Domains map[string]struct {
			BaseURL string `yaml:"base_url"`
		} `yaml:"domains"`
	} `yaml:"mcp"`
	Scheduler struct {
		PollInterval string `yaml:"poll_interval"`
	} `yaml:"scheduler"`
	Ops struct {
		MetricsEnabled                 bool `yaml:"metrics_enabled"`
		AdminEventInjectionEnabled     bool `yaml:"admin_event_injection_enabled"`
		AdminScheduleFireReplayEnabled bool `yaml:"admin_schedule_fire_replay_enabled"`
	} `yaml:"ops"`
	Auth struct {
		AdminToken             string `yaml:"admin_token"`
		HumanActionSecret      string `yaml:"human_action_secret"`
		GitHubWebhookSecret    string `yaml:"github_webhook_secret"`
		GitLabWebhookSecret    string `yaml:"gitlab_webhook_secret"`
		SchedulerIngressSecret string `yaml:"scheduler_ingress_secret"`
	} `yaml:"auth"`
}

func LoadConfig(path string) (*Config, error) {
	var cfg Config
	raw, err := os.ReadFile(path)
	if err == nil {
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			return nil, fmt.Errorf("parse config %s: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	overrideFromEnv(&cfg)
	applyDefaults(&cfg)
	if err := validateConfig(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func overrideFromEnv(cfg *Config) {
	if v := os.Getenv("ALICE_HTTP_LISTEN_ADDR"); v != "" {
		cfg.HTTP.ListenAddr = v
	}
	if v := os.Getenv("ALICE_STORAGE_ROOT_DIR"); v != "" {
		cfg.Storage.RootDir = v
	}
	if v := os.Getenv("ALICE_RUNTIME_SHARD_COUNT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Runtime.ShardCount = n
		}
	}
	if v := os.Getenv("ALICE_PROMOTION_MIN_CONFIDENCE"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.Promotion.MinConfidence = f
		}
	}
	if v := os.Getenv("ALICE_AUTH_ADMIN_TOKEN"); v != "" {
		cfg.Auth.AdminToken = v
	}
	if v := os.Getenv("ALICE_AUTH_HUMAN_ACTION_SECRET"); v != "" {
		cfg.Auth.HumanActionSecret = v
	}
	if v := os.Getenv("ALICE_AUTH_GITHUB_WEBHOOK_SECRET"); v != "" {
		cfg.Auth.GitHubWebhookSecret = v
	}
	if v := os.Getenv("ALICE_AUTH_GITLAB_WEBHOOK_SECRET"); v != "" {
		cfg.Auth.GitLabWebhookSecret = v
	}
	if v := os.Getenv("ALICE_AUTH_SCHEDULER_INGRESS_SECRET"); v != "" {
		cfg.Auth.SchedulerIngressSecret = v
	}
	if v := os.Getenv("ALICE_WORKFLOW_MANIFEST_ROOTS"); v != "" {
		cfg.Workflow.ManifestRoots = strings.Split(v, ",")
	}
	if v := os.Getenv("ALICE_OPS_ADMIN_EVENT_INJECTION_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Ops.AdminEventInjectionEnabled = b
		}
	}
	if v := os.Getenv("ALICE_OPS_ADMIN_SCHEDULE_FIRE_REPLAY_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Ops.AdminScheduleFireReplayEnabled = b
		}
	}
}

func applyDefaults(cfg *Config) {
	if cfg.HTTP.ListenAddr == "" {
		cfg.HTTP.ListenAddr = ":8080"
	}
	if cfg.Storage.RootDir == "" {
		cfg.Storage.RootDir = "data"
	}
	if cfg.Storage.SnapshotInterval <= 0 {
		cfg.Storage.SnapshotInterval = 100
	}
	if cfg.Runtime.ShardCount <= 0 {
		cfg.Runtime.ShardCount = 16
	}
	if cfg.Runtime.OutboxWorkers <= 0 {
		cfg.Runtime.OutboxWorkers = 4
	}
	if cfg.Promotion.MinConfidence <= 0 {
		cfg.Promotion.MinConfidence = 0.6
	}
	if cfg.Scheduler.PollInterval == "" {
		cfg.Scheduler.PollInterval = "30s"
	}
	if len(cfg.Workflow.ManifestRoots) == 0 {
		cfg.Workflow.ManifestRoots = []string{"configs/workflows"}
	}
}

func validateConfig(cfg *Config) error {
	if len(cfg.Workflow.ManifestRoots) == 0 {
		return fmt.Errorf("workflow.manifest_roots is required")
	}
	return nil
}
