package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	agentbridgeclaude "github.com/Alice-space/agentbridge/claude"
	agentbridgecodex "github.com/Alice-space/agentbridge/codex"
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
	runtimeOnly := false
	feishuWebsocket := false
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
		effectiveRuntimeOnly, err := resolveConnectorStartupMode(
			cmd,
			runtimeOnly,
			feishuWebsocket,
			os.Args,
		)
		if err != nil {
			return err
		}
		return runConnector(effectiveConfigPath, effectivePIDFilePath, pidFileExplicit, effectiveRuntimeOnly)
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
	root.PersistentFlags().BoolVar(&runtimeOnly, "runtime-only", false, "start automation/runtime API without connecting to Feishu websocket")
	root.PersistentFlags().BoolVar(&feishuWebsocket, "feishu-websocket", false, "connect to Feishu websocket and process messages")
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
	root.AddCommand(newSkillsCmd())
	return root
}

func resolveConnectorStartupMode(cmd *cobra.Command, runtimeOnly bool, feishuWebsocket bool, argv []string) (bool, error) {
	runtimeOnlySelected := boolFlagSelected(cmd, "runtime-only", runtimeOnly)
	feishuWebsocketSelected := boolFlagSelected(cmd, "feishu-websocket", feishuWebsocket)
	if runtimeOnlySelected && feishuWebsocketSelected {
		return false, errors.New("choose exactly one connector startup mode: --runtime-only or --feishu-websocket")
	}
	if isHeadlessExecutable(argv) {
		base := pathDisplayBase(firstArg(argv))
		if base == "" {
			base = "headless executable"
		}
		if feishuWebsocketSelected {
			return false, fmt.Errorf("%q only supports --runtime-only", base)
		}
		if !runtimeOnlySelected {
			return false, fmt.Errorf("%q requires --runtime-only", base)
		}
		return true, nil
	}
	switch {
	case runtimeOnlySelected:
		return true, nil
	case feishuWebsocketSelected:
		return false, nil
	default:
		return false, errors.New("select a connector startup mode: pass --runtime-only for local runtime API only, or --feishu-websocket to connect to Feishu")
	}
}

func boolFlagSelected(cmd *cobra.Command, name string, value bool) bool {
	if cmd == nil {
		return value
	}
	return cmd.Flags().Changed(name) && value
}

func isHeadlessExecutable(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	base := strings.TrimSpace(argv[0])
	base = strings.TrimRight(base, `/\`)
	if idx := strings.LastIndexAny(base, `/\`); idx >= 0 {
		base = base[idx+1:]
	}
	if base == "" {
		return false
	}
	stem := strings.TrimSuffix(strings.ToLower(base), strings.ToLower(filepath.Ext(base)))
	return stem == "alice-headless" || strings.HasSuffix(stem, "-headless")
}

func firstArg(argv []string) string {
	if len(argv) == 0 {
		return ""
	}
	return argv[0]
}

func pathDisplayBase(path string) string {
	path = strings.TrimSpace(path)
	path = strings.TrimRight(path, `/\`)
	if path == "" {
		return ""
	}
	if idx := strings.LastIndexAny(path, `/\`); idx >= 0 {
		path = path[idx+1:]
	}
	return path
}

func runConnector(configPath, pidFilePath string, pidFileExplicit bool, runtimeOnly bool) error {
	configPath = bootstrap.ResolveConfigPath(configPath)
	created, err := ensureConfigFileExists(configPath)
	if err != nil {
		return err
	}
	if created {
		fmt.Printf("[alice] created initial config at %s from embedded template\n", configPath)
		fmt.Printf("[alice] please edit bots.*.feishu_app_id/bots.*.feishu_app_secret, then restart service\n")
		return nil
	}
	if !runtimeOnly {
		ready, err := configHasRequiredCredentials(configPath)
		if err != nil {
			return err
		}
		if !ready {
			fmt.Printf("[alice] config found but bots.*.feishu_app_id/feishu_app_secret are empty: %s\n", configPath)
			fmt.Printf("[alice] please edit config and restart service\n")
			return nil
		}
	}

	cfg, err := config.LoadFromFile(configPath)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.AliceHome) != "" {
		_ = os.Setenv(config.EnvAliceHome, cfg.AliceHome)
	}
	codexHome := ensureCodexHomeEnv(cfg.CodexHome)
	if !pidFileExplicit {
		pidFilePath = config.PIDFilePathForAliceHome(cfg.AliceHome)
	}
	pidCleanup, err := preparePIDFile(pidFilePath)
	if err != nil {
		return err
	}
	defer pidCleanup()
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
	codexAuthChecks := map[string]*codexLoginCheck{}
	claudeAuthChecks := map[string]*claudeLoginCheck{}
	skillPlan := &bundledSkillSyncPlan{
		AliceHome: cfg.AliceHome,
		allowed:   map[string]struct{}{},
	}
	for _, runtimeCfg := range runtimeConfigs {
		if err := ensureWorkspaceDir(runtimeCfg.WorkspaceDir); err != nil {
			return err
		}
		soulReport, soulErr := bootstrap.EnsureBotSoulFile(runtimeCfg.SoulPath)
		if soulErr != nil {
			logging.Warnf("ensure bot soul failed bot=%s path=%s: %v", runtimeCfg.BotID, runtimeCfg.SoulPath, soulErr)
		} else if soulReport.Created {
			logging.Infof("bot soul template created bot=%s path=%s", runtimeCfg.BotID, soulReport.Path)
		}

		if runtimeUsesCodex(runtimeCfg) {
			codexCmd := resolveCodexCommand(runtimeCfg)
			key := codexCmd + "\x00" + runtimeCfg.CodexHome
			check, ok := codexAuthChecks[key]
			if !ok {
				check = &codexLoginCheck{
					Command:   codexCmd,
					CodexHome: runtimeCfg.CodexHome,
					Timeout:   runtimeCfg.AuthStatusTimeout,
				}
				codexAuthChecks[key] = check
			}
			if runtimeCfg.AuthStatusTimeout > check.Timeout {
				check.Timeout = runtimeCfg.AuthStatusTimeout
			}
			check.Bots = append(check.Bots, runtimeCfg.BotID)
		}
		if runtimeUsesClaude(runtimeCfg) {
			claudeCmd := resolveClaudeCommand(runtimeCfg)
			check, ok := claudeAuthChecks[claudeCmd]
			if !ok {
				check = &claudeLoginCheck{
					Command: claudeCmd,
					Timeout: runtimeCfg.AuthStatusTimeout,
				}
				claudeAuthChecks[claudeCmd] = check
			}
			if runtimeCfg.AuthStatusTimeout > check.Timeout {
				check.Timeout = runtimeCfg.AuthStatusTimeout
			}
			check.Bots = append(check.Bots, runtimeCfg.BotID)
		}

		skillPlan.Bots = append(skillPlan.Bots, runtimeCfg.BotID)
		for _, skill := range runtimeCfg.AllowedBundledSkills() {
			skill = strings.TrimSpace(skill)
			if skill == "" {
				continue
			}
			skillPlan.allowed[skill] = struct{}{}
		}
	}

	authKeys := make([]string, 0, len(codexAuthChecks))
	for key := range codexAuthChecks {
		authKeys = append(authKeys, key)
	}
	sort.Strings(authKeys)
	for _, key := range authKeys {
		check := codexAuthChecks[key]
		report, authErr := agentbridgecodex.CheckLogin(check.Command, check.CodexHome, check.Timeout)
		if authErr != nil {
			return fmt.Errorf("check Codex login failed for bots %s: %w", check.botList(), authErr)
		}
		if !report.LoggedIn {
			return fmt.Errorf(
				"Codex login required for bots %s (command=%q, CODEX_HOME=%s): %s",
				check.botList(),
				report.Command,
				report.CodexHome,
				formatCodexLoginOutput(report.Command, report.Output),
			)
		}
		logging.Infof("codex login verified bots=%s codex_home=%s command=%s", check.botList(), report.CodexHome, report.Command)
	}
	claudeKeys := make([]string, 0, len(claudeAuthChecks))
	for key := range claudeAuthChecks {
		claudeKeys = append(claudeKeys, key)
	}
	sort.Strings(claudeKeys)
	for _, key := range claudeKeys {
		check := claudeAuthChecks[key]
		report, authErr := agentbridgeclaude.CheckLogin(check.Command, check.Timeout)
		if authErr != nil {
			return fmt.Errorf("check Claude login failed for bots %s: %w", check.botList(), authErr)
		}
		if !report.LoggedIn {
			return fmt.Errorf(
				"Claude login required for bots %s (command=%q): %s",
				check.botList(),
				report.Command,
				formatClaudeLoginOutput(report.Command, report.Output),
			)
		}
		logging.Infof(
			"claude login verified bots=%s command=%s auth_method=%s api_provider=%s",
			check.botList(),
			report.Command,
			report.AuthMethod,
			report.APIProvider,
		)
	}

	skillReport, skillErr := bootstrap.EnsureBundledSkillsLinkedForAliceHome(skillPlan.AliceHome, skillPlan.allowedSkills())
	if skillErr != nil {
		logging.Warnf("sync bundled skills failed bots=%s alice_home=%s: %v", skillPlan.botList(), skillPlan.AliceHome, skillErr)
	} else if skillReport.Discovered > 0 {
		logging.Infof(
			"bundled skills synced bots=%s alice_home=%s source_root=%s agents_skills=%s claude_skills=%s discovered=%d linked=%d updated=%d unchanged=%d failed=%d",
			skillPlan.botList(),
			skillReport.AliceHome,
			skillReport.SourceRoot,
			skillReport.AgentsSkillsDir,
			skillReport.ClaudeSkillsDir,
			skillReport.Discovered,
			skillReport.Linked,
			skillReport.Updated,
			skillReport.Unchanged,
			skillReport.Failed,
		)
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

	if runtimeOnly {
		logging.Infof("runtime-only mode enabled; Feishu websocket connector disabled")
	} else {
		logging.Infof("feishu-codex connector started (long connection mode)")
	}
	for _, built := range manager.Runtimes {
		if built == nil {
			continue
		}
		logging.Infof("automation engine enabled bot=%s state_file=%s", built.Config.BotID, built.AutomationStatePath)
		if built.RuntimeAPI != nil {
			logging.Infof("runtime http api enabled bot=%s addr=%s", built.Config.BotID, built.RuntimeAPIBaseURL)
		}
	}
	if runtimeOnly {
		if err := manager.RunRuntimeOnly(ctx); err != nil {
			return err
		}
		return nil
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
	next, err := cfg.RuntimeConfigForBot(runtime.Config.BotID)
	if err != nil {
		logging.Warnf("config hot reload skipped: resolve bot runtime failed path=%s bot=%s err=%v", configPath, runtime.Config.BotID, err)
		return
	}
	report, err := runtime.ApplyConfigReload(next)
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

func ensureCodexHomeEnv(codexHome string) string {
	target := config.ResolveCodexHomeDir(codexHome)
	_ = os.Setenv(config.EnvCodexHome, target)
	return target
}

type codexLoginCheck struct {
	Command   string
	CodexHome string
	Timeout   time.Duration
	Bots      []string
}

func (c *codexLoginCheck) botList() string {
	bots := append([]string(nil), c.Bots...)
	sort.Strings(bots)
	return strings.Join(bots, ",")
}

type bundledSkillSyncPlan struct {
	AliceHome string
	Bots      []string
	allowed   map[string]struct{}
}

type claudeLoginCheck struct {
	Command string
	Timeout time.Duration
	Bots    []string
}

func (p *bundledSkillSyncPlan) allowedSkills() []string {
	skills := make([]string, 0, len(p.allowed))
	for skill := range p.allowed {
		skills = append(skills, skill)
	}
	sort.Strings(skills)
	return skills
}

func (p *bundledSkillSyncPlan) botList() string {
	bots := append([]string(nil), p.Bots...)
	sort.Strings(bots)
	return strings.Join(bots, ",")
}

func runtimeUsesCodex(cfg config.Config) bool {
	for _, provider := range cfg.ResolvedLLMProviders() {
		if provider == config.DefaultLLMProvider {
			return true
		}
	}
	return false
}

func runtimeUsesClaude(cfg config.Config) bool {
	for _, provider := range cfg.ResolvedLLMProviders() {
		if provider == config.LLMProviderClaude {
			return true
		}
	}
	return false
}

// resolveCodexCommand returns the codex command from the first codex profile
// (alphabetically), falling back to "codex".
func resolveCodexCommand(cfg config.Config) string {
	names := make([]string, 0, len(cfg.LLMProfiles))
	for name, profile := range cfg.LLMProfiles {
		if strings.ToLower(strings.TrimSpace(profile.Provider)) == config.DefaultLLMProvider ||
			profile.Provider == "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if cmd := strings.TrimSpace(cfg.LLMProfiles[name].Command); cmd != "" {
			return cmd
		}
	}
	return "codex"
}

func resolveClaudeCommand(cfg config.Config) string {
	names := make([]string, 0, len(cfg.LLMProfiles))
	for name, profile := range cfg.LLMProfiles {
		if strings.ToLower(strings.TrimSpace(profile.Provider)) == config.LLMProviderClaude {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if cmd := strings.TrimSpace(cfg.LLMProfiles[name].Command); cmd != "" {
			return cmd
		}
	}
	return "claude"
}

func formatCodexLoginOutput(command, output string) string {
	output = strings.Join(strings.Fields(strings.TrimSpace(output)), " ")
	if output == "" {
		command = strings.TrimSpace(command)
		if command == "" {
			command = "codex"
		}
		return fmt.Sprintf("run `%s login` (or `CODEX_HOME=<path> %s login`) first", command, command)
	}
	return output
}

func (c *claudeLoginCheck) botList() string {
	bots := append([]string(nil), c.Bots...)
	sort.Strings(bots)
	return strings.Join(bots, ",")
}

func formatClaudeLoginOutput(command, output string) string {
	output = strings.Join(strings.Fields(strings.TrimSpace(output)), " ")
	if output == "" {
		command = strings.TrimSpace(command)
		if command == "" {
			command = "claude"
		}
		return fmt.Sprintf("run `%s auth login` first", command)
	}
	return output
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
