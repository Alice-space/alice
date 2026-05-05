package codex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// LoginStatusReport holds the result of a Codex CLI login status check.
type LoginStatusReport struct {
	Command   string
	CodexHome string
	LoggedIn  bool
	Output    string
}

// CheckLogin runs "codex login status" for the given codexHome directory.
// If codexHome is empty, it falls back to $CODEX_HOME or ~/.codex.
// If timeout is zero or negative, a 15-second default is used.
func CheckLogin(command, codexHome string, timeout time.Duration) (LoginStatusReport, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return LoginStatusReport{}, fmt.Errorf("codex command is empty")
	}
	codexHome = resolveCodexHome(codexHome)
	if codexHome == "" {
		return LoginStatusReport{}, fmt.Errorf("codex home is empty")
	}
	if timeout <= 0 {
		timeout = 15 * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, command, "login", "status")
	cmd.Env = envWithCodexHome(os.Environ(), codexHome)
	output, err := cmd.CombinedOutput()

	report := LoginStatusReport{
		Command:   command,
		CodexHome: codexHome,
		Output:    strings.TrimSpace(string(output)),
	}
	if err == nil {
		report.LoggedIn = true
		return report, nil
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return report, fmt.Errorf("check codex login status timed out: %w", ctx.Err())
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return report, nil
	}
	return report, fmt.Errorf("run %q login status failed: %w", command, err)
}

func resolveCodexHome(override string) string {
	override = strings.TrimSpace(override)
	if override != "" {
		return override
	}
	if v := strings.TrimSpace(os.Getenv("CODEX_HOME")); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err == nil && strings.TrimSpace(home) != "" {
		return home + "/.codex"
	}
	return ".codex"
}

func envWithCodexHome(base []string, codexHome string) []string {
	const key = "CODEX_HOME"
	prefix := key + "="
	env := make([]string, 0, len(base)+1)
	for _, item := range base {
		if strings.HasPrefix(item, prefix) {
			continue
		}
		env = append(env, item)
	}
	env = append(env, prefix+codexHome)
	return env
}
