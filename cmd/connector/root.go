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

	"github.com/Alice-space/alice/internal/bootstrap"
	"github.com/Alice-space/alice/internal/config"
	"github.com/Alice-space/alice/internal/logging"
)

func newRootCmd() *cobra.Command {
	configPath := config.DefaultConfigPath()
	pidFilePath := config.DefaultPIDFilePath()
	aliceHome := ""
	executeConnector := func(cmd *cobra.Command) error {
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
	cfg, err := config.LoadFromFile(configPath)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.AliceHome) != "" {
		_ = os.Setenv(config.EnvAliceHome, cfg.AliceHome)
	}
	codexHome := ensureIsolatedCodexHomeEnv(cfg.AliceHome)
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

	llmProvider, err := bootstrap.NewLLMProvider(cfg)
	if err != nil {
		return err
	}

	skillReport, err := bootstrap.EnsureBundledSkillsLinked(cfg.WorkspaceDir)
	if err != nil {
		logging.Warnf("sync bundled skills failed: %v", err)
	} else if skillReport.Discovered > 0 {
		logging.Infof(
			"bundled skills synced codex_home=%s discovered=%d linked=%d updated=%d unchanged=%d failed=%d",
			skillReport.CodexHome,
			skillReport.Discovered,
			skillReport.Linked,
			skillReport.Updated,
			skillReport.Unchanged,
			skillReport.Failed,
		)
	}

	runtime, err := bootstrap.BuildConnectorRuntime(cfg, llmProvider)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := startConfigHotReload(ctx, configPath, runtime); err != nil {
		logging.Warnf("config hot reload disabled: %v", err)
	}

	logging.Infof("feishu-codex connector started (long connection mode)")
	logging.Infof("memory module enabled dir=%s", runtime.MemoryDir)
	logging.Infof("automation engine enabled state_file=%s", runtime.AutomationStatePath)
	if runtime.RuntimeAPI != nil {
		logging.Infof("runtime http api enabled addr=%s", runtime.RuntimeAPIBaseURL)
	}
	if err := runtime.Run(ctx); err != nil {
		return err
	}

	logging.Infof("connector stopped")
	return nil
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

func ensureIsolatedCodexHomeEnv(aliceHome string) string {
	target := config.CodexHomeForAliceHome(aliceHome)
	_ = os.Setenv(config.EnvCodexHome, target)
	return target
}
