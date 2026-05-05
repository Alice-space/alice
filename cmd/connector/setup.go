package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Alice-space/alice/internal/bootstrap"
	"github.com/Alice-space/alice/internal/config"
)

const opencodePluginJS = `export const AliceDelegate = async ({ $, directory }) => ({
  tool: {
    codex: {
      description:
        "Delegate a task to OpenAI Codex CLI agent. " +
        "Use for code editing, sandbox execution, or repository-wide changes.",
      args: {
        prompt: { type: "string", description: "Task for Codex" },
        model: { type: "string", description: "Model override (e.g. gpt-5.1)" },
		workspaceDir: { type: "string", description: "Working directory override" },
      },
      async execute(args) {
        const cmd = ["alice", "delegate", "--provider", "codex"]
        if (args.workspaceDir) cmd.push("--workspace-dir", args.workspaceDir)
        else cmd.push("--workspace-dir", directory)
        if (args.model) cmd.push("--model", args.model)
        cmd.push("--prompt", args.prompt)
        return await $` + "`${cmd}`" + `.text()
      },
    },
    claude: {
      description:
        "Delegate a task to Anthropic Claude CLI agent. " +
        "Use for analysis, review, explanation, documentation, or debugging.",
      args: {
        prompt: { type: "string", description: "Task for Claude" },
        model: { type: "string", description: "Model override (e.g. claude-sonnet-4-20250514)" },
		workspaceDir: { type: "string", description: "Working directory override" },
      },
      async execute(args) {
        const cmd = ["alice", "delegate", "--provider", "claude"]
        if (args.workspaceDir) cmd.push("--workspace-dir", args.workspaceDir)
        else cmd.push("--workspace-dir", directory)
        if (args.model) cmd.push("--model", args.model)
        cmd.push("--prompt", args.prompt)
        return await $` + "`${cmd}`" + `.text()
      },
    },
  },
})
`

func newSetupCmd() *cobra.Command {
	aliceHome := ""
	serviceName := "alice.service"

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Initialize Alice runtime directories, config, and OpenCode plugin",
		Long: `Create ALICE_HOME, write initial config, sync bundled skills,
write systemd user unit (Linux), and install the OpenCode delegate plugin.

After setup:
  systemctl --user start alice.service     # Linux
  alice --feishu-websocket                 # manual start`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			aliceHome = strings.TrimSpace(aliceHome)
			if aliceHome == "" {
				aliceHome = config.AliceHomeDir()
			} else {
				aliceHome = config.ResolveAliceHomeDir(aliceHome)
			}
			_ = os.Setenv(config.EnvAliceHome, aliceHome)

			configPath := config.ConfigPathForAliceHome(aliceHome)
			skillsSourceDir := filepath.Join(aliceHome, "skills")

			newline := func() { fmt.Fprintln(cmd.OutOrStdout()) }
			info := func(format string, args ...any) {
				fmt.Fprintf(cmd.OutOrStdout(), "[alice setup] "+format+"\n", args...)
			}

			info("alice home: %s", aliceHome)

			// 1. Create directory structure
			for _, dir := range []string{
				filepath.Join(aliceHome, "bin"),
				filepath.Join(aliceHome, "log"),
				filepath.Join(aliceHome, "run"),
				filepath.Join(aliceHome, "prompts"),
			} {
				if err := os.MkdirAll(dir, 0o750); err != nil {
					return fmt.Errorf("create directory %s: %w", dir, err)
				}
			}
			info("directories created")

			// 2. Write config template
			configCreated, err := ensureConfigFileExists(configPath)
			if err != nil {
				return fmt.Errorf("write config: %w", err)
			}
			if configCreated {
				info("wrote initial config: %s", configPath)
			} else {
				info("config already exists: %s", configPath)
			}

			// 3. Sync skills
			skillReport, err := bootstrap.EnsureBundledSkillsLinkedForAliceHome(aliceHome, nil)
			if err != nil {
				info("skill sync failed (Alice will retry on startup): %v", err)
			} else {
				info("skills synced: source=%s linked=%d unchanged=%d",
					skillReport.SourceRoot, skillReport.Linked, skillReport.Unchanged)
			}
			_ = skillsSourceDir

			// 4. Write systemd user unit (Linux only)
			if runtime.GOOS == "linux" {
				configHome := os.Getenv("XDG_CONFIG_HOME")
				if configHome == "" {
					configHome = filepath.Join(os.Getenv("HOME"), ".config")
				}
				serviceDir := filepath.Join(configHome, "systemd", "user")
				serviceFile := filepath.Join(serviceDir, serviceName)
				if err := os.MkdirAll(serviceDir, 0o750); err != nil {
					return fmt.Errorf("create systemd dir: %w", err)
				}
				binPath, _ := os.Executable()
				unit := fmt.Sprintf(`[Unit]
Description=Alice Connector
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
Environment=ALICE_HOME=%s
WorkingDirectory=%s
ExecStart=%s --feishu-websocket
Restart=on-failure
RestartSec=3
NoNewPrivileges=yes

[Install]
WantedBy=default.target
`, aliceHome, aliceHome, binPath)
				if err := os.WriteFile(serviceFile, []byte(unit), 0o644); err != nil {
					return fmt.Errorf("write systemd unit: %w", err)
				}
				info("wrote systemd unit: %s", serviceFile)

				// enable-linger
				if lingerOutput, lingerErr := exec.Command("loginctl", "enable-linger").CombinedOutput(); lingerErr == nil {
					info("linger enabled")
				} else {
					info("linger enable skipped (may need sudo): %s", strings.TrimSpace(string(lingerOutput)))
				}

				// daemon-reload
				if reloadOut, reloadErr := exec.Command("systemctl", "--user", "daemon-reload").CombinedOutput(); reloadErr != nil {
					info("systemctl daemon-reload: %s", strings.TrimSpace(string(reloadOut)))
				} else {
					info("systemd daemon-reload done")
				}
			}

			// 5. Write OpenCode plugin
			pluginDir := filepath.Join(os.Getenv("HOME"), ".config", "opencode", "plugins")
			if err := os.MkdirAll(pluginDir, 0o750); err != nil {
				info("plugin dir create failed: %v", err)
			} else {
				pluginPath := filepath.Join(pluginDir, "alice-delegate.js")
				if err := os.WriteFile(pluginPath, []byte(opencodePluginJS), 0o644); err != nil {
					info("plugin write failed: %v", err)
				} else {
					info("wrote OpenCode plugin: %s", pluginPath)
				}
			}

			newline()
			info("setup complete")

			if runtime.GOOS == "linux" {
				fmt.Fprintln(cmd.OutOrStdout(), "Next steps:")
				fmt.Fprintf(cmd.OutOrStdout(), "  1. Edit config: %s\n", configPath)
				fmt.Fprintf(cmd.OutOrStdout(), "  2. Start service: systemctl --user start %s\n", serviceName)
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "Next steps:")
				fmt.Fprintf(cmd.OutOrStdout(), "  1. Edit config: %s\n", configPath)
				fmt.Fprintf(cmd.OutOrStdout(), "  2. Start: alice --feishu-websocket\n")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&aliceHome, "alice-home", "", "ALICE_HOME directory (default: ~/.alice)")
	cmd.Flags().StringVar(&serviceName, "service", "alice.service", "systemd service name")
	return cmd
}
