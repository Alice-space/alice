package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	aliceassets "github.com/Alice-space/alice"
	"github.com/Alice-space/alice/internal/bootstrap"
	"github.com/Alice-space/alice/internal/buildinfo"
	"github.com/Alice-space/alice/internal/config"
	"github.com/Alice-space/alice/internal/logging"
)

func newRootCmd() *cobra.Command {
	configPath := config.DefaultConfigPath()
	pidFilePath := config.DefaultPIDFilePath()
	aliceHome := ""
	showVersion := false
	executeConnector := func(cmd *cobra.Command) error {
		if showVersion {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), buildinfo.CurrentVersion())
			return err
		}
		override := strings.TrimSpace(aliceHome)
		if override != "" {
			_ = os.Setenv(config.EnvAliceHome, config.ResolveAliceHomeDir(override))
		}
		effectiveConfigPath := configPath
		if !cmd.Flags().Changed("config") {
			effectiveConfigPath = config.DefaultConfigPath()
		}
		effectivePIDFilePath := pidFilePath
		pidFileExplicit := cmd.Flags().Changed("pid-file")
		if !pidFileExplicit {
			effectivePIDFilePath = config.DefaultPIDFilePath()
		}
		return runConnector(effectiveConfigPath, effectivePIDFilePath, pidFileExplicit)
	}
	root := &cobra.Command{
		Use:           "alice",
		Short:         "Run the Alice Feishu connector",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return executeConnector(cmd)
		},
	}
	root.PersistentFlags().StringVar(
		&aliceHome,
		"alice-home",
		"",
		fmt.Sprintf("alice runtime home dir (default: ~/%s)", config.DefaultAliceHomeName()),
	)
	root.Flags().BoolVar(&showVersion, "version", false, "print Alice version and exit")
	root.PersistentFlags().StringVarP(&configPath, "config", "c", config.DefaultConfigPath(), "path to config yaml")
	root.PersistentFlags().StringVar(&pidFilePath, "pid-file", config.DefaultPIDFilePath(), "path to pid file (empty disables pid lock)")
	root.AddCommand(&cobra.Command{
		Use:   "run",
		Short: "Run the connector process",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return executeConnector(cmd)
		},
	})
	root.AddCommand(newRuntimeCmd())
	return root
}

func runConnector(configPath, pidFilePath string, pidFileExplicit bool) error {
	configPath = bootstrap.ResolveConfigPath(configPath)
	created, err := ensureConfigFileExists(configPath)
	if err != nil {
		return err
	}
	if created {
		fmt.Printf("[alice] created initial config at %s from embedded template\n", configPath)
		fmt.Printf("[alice] please edit feishu_app_id/feishu_app_secret, then restart service\n")
		return nil
	}
	ready, err := configHasRequiredCredentials(configPath)
	if err != nil {
		return err
	}
	if !ready {
		fmt.Printf("[alice] config found but feishu_app_id/feishu_app_secret are empty: %s\n", configPath)
		fmt.Printf("[alice] please edit config and restart service\n")
		return nil
	}

	cfg, err := config.LoadFromFile(configPath)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.AliceHome) != "" {
		_ = os.Setenv(config.EnvAliceHome, cfg.AliceHome)
	}
	codexHome := ensureIsolatedCodexHomeEnv(cfg.CodexHome)
	if !pidFileExplicit {
		pidFilePath = config.PIDFilePathForAliceHome(cfg.AliceHome)
	}
	pidCleanup, err := preparePIDFile(pidFilePath)
	if err != nil {
		return err
	}
	defer pidCleanup()
	if err := ensureWorkspaceDir(cfg.WorkspaceDir); err != nil {
		return err
	}
	if err := logging.Configure(logging.Options{
		Level:      cfg.LogLevel,
		FilePath:   cfg.LogFile,
		MaxSizeMB:  cfg.LogMaxSizeMB,
		MaxBackups: cfg.LogMaxBackups,
		MaxAgeDays: cfg.LogMaxAgeDays,
		Compress:   cfg.LogCompress,
	}); err != nil {
		return err
	}
	logging.Debugf("debug logging enabled log_level=%s config=%s", cfg.LogLevel, configPath)
	logging.Infof("runtime timezone=%s", time.Now().Location().String())
	logging.Infof("runtime CODEX_HOME=%s", codexHome)
	if strings.TrimSpace(pidFilePath) != "" {
		logging.Infof("pid file enabled path=%s", pidFilePath)
	}

	runtimeConfigs, err := cfg.RuntimeConfigs()
	if err != nil {
		return err
	}
	for _, runtimeCfg := range runtimeConfigs {
		if err := ensureWorkspaceDir(runtimeCfg.WorkspaceDir); err != nil {
			return err
		}
		skillReport, skillErr := bootstrap.EnsureBundledSkillsLinkedForCodexHome(runtimeCfg.CodexHome, runtimeCfg.AllowedBundledSkills())
		if skillErr != nil {
			logging.Warnf("sync bundled skills failed bot=%s: %v", runtimeCfg.BotID, skillErr)
			continue
		}
		if skillReport.Discovered > 0 {
			logging.Infof(
				"bundled skills synced bot=%s codex_home=%s discovered=%d linked=%d updated=%d unchanged=%d failed=%d",
				runtimeCfg.BotID,
				skillReport.CodexHome,
				skillReport.Discovered,
				skillReport.Linked,
				skillReport.Updated,
				skillReport.Unchanged,
				skillReport.Failed,
			)
		}
	}

	manager, err := bootstrap.BuildRuntimeManager(cfg)
	if err != nil {
		return err
	}
	if len(manager.Runtimes) == 0 {
		return fmt.Errorf("no connector runtime built")
	}
	var runtime *bootstrap.ConnectorRuntime
	if len(manager.Runtimes) == 1 {
		runtime = manager.Runtimes[0]
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if runtime != nil {
		if err := startConfigHotReload(ctx, configPath, runtime); err != nil {
			logging.Warnf("config hot reload disabled: %v", err)
		}
	} else {
		logging.Warnf("config hot reload disabled for multi-bot mode; restart service after config changes")
	}

	logging.Infof("feishu-codex connector started (long connection mode)")
	for _, built := range manager.Runtimes {
		if built == nil {
			continue
		}
		logging.Infof("automation engine enabled bot=%s state_file=%s", built.Config.BotID, built.AutomationStatePath)
		if built.RuntimeAPI != nil {
			logging.Infof("runtime http api enabled bot=%s addr=%s", built.Config.BotID, built.RuntimeAPIBaseURL)
		}
	}
	if err := manager.Run(ctx); err != nil {
		return err
	}

	logging.Infof("connector stopped")
	return nil
}

func ensureConfigFileExists(configPath string) (bool, error) {
	if _, err := os.Stat(configPath); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("check config path failed: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return false, fmt.Errorf("create config directory failed: %w", err)
	}
	if err := os.WriteFile(configPath, aliceassets.ConfigExampleYAML, 0o600); err != nil {
		return false, fmt.Errorf("write initial config failed: %w", err)
	}
	return true, nil
}

func configHasRequiredCredentials(configPath string) (bool, error) {
	v := viper.New()
	v.SetConfigFile(configPath)
	v.SetConfigType("yaml")
	if err := v.ReadInConfig(); err != nil {
		return false, fmt.Errorf("read config yaml failed: %w", err)
	}
	appID := strings.TrimSpace(v.GetString("feishu_app_id"))
	appSecret := strings.TrimSpace(v.GetString("feishu_app_secret"))
	if appID != "" && appSecret != "" {
		return true, nil
	}
	for _, rawBot := range v.GetStringMap("bots") {
		botMap, ok := rawBot.(map[string]any)
		if !ok {
			continue
		}
		botAppID := strings.TrimSpace(stringValue(botMap["feishu_app_id"]))
		botSecret := strings.TrimSpace(stringValue(botMap["feishu_app_secret"]))
		if botAppID != "" && botSecret != "" {
			return true, nil
		}
	}
	return false, nil
}

func startConfigHotReload(ctx context.Context, configPath string, runtime *bootstrap.ConnectorRuntime) error {
	if runtime == nil {
		return nil
	}
	absConfigPath, err := filepath.Abs(strings.TrimSpace(configPath))
	if err != nil {
		return err
	}
	watcher := viper.New()
	watcher.SetConfigFile(absConfigPath)
	watcher.SetConfigType("yaml")
	if err := watcher.ReadInConfig(); err != nil {
		return err
	}
	logging.Infof("config hot reload enabled path=%s", absConfigPath)

	var timerMu sync.Mutex
	var timer *time.Timer
	scheduleReload := func() {
		timerMu.Lock()
		defer timerMu.Unlock()
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(300*time.Millisecond, func() {
			reloadConfigFromDisk(absConfigPath, runtime)
		})
	}
	watcher.OnConfigChange(func(event fsnotify.Event) {
		changedPath := filepath.Clean(strings.TrimSpace(event.Name))
		if changedPath != filepath.Clean(absConfigPath) {
			return
		}
		logging.Infof("config change detected path=%s op=%s", absConfigPath, event.Op.String())
		scheduleReload()
	})
	watcher.WatchConfig()

	go func() {
		<-ctx.Done()
		timerMu.Lock()
		if timer != nil {
			timer.Stop()
		}
		timerMu.Unlock()
	}()
	return nil
}

func reloadConfigFromDisk(configPath string, runtime *bootstrap.ConnectorRuntime) {
	cfg, err := config.LoadFromFile(configPath)
	if err != nil {
		logging.Warnf("config hot reload skipped: reload config failed path=%s err=%v", configPath, err)
		return
	}
	report, err := runtime.ApplyConfigReload(cfg)
	if err != nil {
		logging.Warnf("config hot reload failed path=%s err=%v", configPath, err)
		return
	}
	if len(report.AppliedFields) > 0 {
		logging.Infof(
			"config hot reload applied path=%s fields=%s",
			configPath,
			strings.Join(report.AppliedFields, ","),
		)
	}
	if len(report.RestartRequiredFields) > 0 {
		logging.Warnf(
			"config hot reload requires restart path=%s fields=%s",
			configPath,
			strings.Join(report.RestartRequiredFields, ","),
		)
	}
}

func preparePIDFile(path string) (func(), error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return func() {}, nil
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve pid file path failed: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return nil, fmt.Errorf("create pid file directory failed: %w", err)
	}

	existingPID, err := readPIDFile(absPath)
	if err == nil && existingPID > 0 && existingPID != os.Getpid() && isProcessRunning(existingPID) {
		return nil, fmt.Errorf("alice is already running pid=%d pid_file=%s", existingPID, absPath)
	}

	selfPID := os.Getpid()
	if err := os.WriteFile(absPath, []byte(strconv.Itoa(selfPID)+"\n"), 0o644); err != nil {
		return nil, fmt.Errorf("write pid file failed: %w", err)
	}
	return func() {
		currentPID, err := readPIDFile(absPath)
		if err != nil {
			return
		}
		if currentPID == selfPID {
			_ = os.Remove(absPath)
		}
	}, nil
}

func readPIDFile(path string) (int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("invalid pid content")
	}
	return pid, nil
}

func isProcessRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	var errno syscall.Errno
	return errors.As(err, &errno) && errno == syscall.EPERM
}

func ensureWorkspaceDir(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	info, err := os.Stat(path)
	if err == nil {
		if info.IsDir() {
			return nil
		}
		return fmt.Errorf("workspace_dir is not a directory: %s", path)
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf("check workspace_dir failed: %w", err)
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return fmt.Errorf("create workspace_dir failed: %w", err)
	}
	return nil
}

func ensureIsolatedCodexHomeEnv(codexHome string) string {
	target := strings.TrimSpace(codexHome)
	if target == "" {
		target = config.DefaultCodexHome()
	}
	_ = os.Setenv(config.EnvCodexHome, target)
	return target
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		return fmt.Sprintf("%v", value)
	}
}
