package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Alice-space/alice/internal/config"
)

type CodexLoginStatusReport struct {
	Command   string
	CodexHome string
	LoggedIn  bool
	Output    string
}

func CheckCodexLoginForCodexHome(command, codexHome string, timeout time.Duration) (CodexLoginStatusReport, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return CodexLoginStatusReport{}, fmt.Errorf("codex command is empty")
	}
	codexHome = config.ResolveCodexHomeDir(codexHome)
	if codexHome == "" {
		return CodexLoginStatusReport{}, fmt.Errorf("codex home is empty")
	}
	if timeout <= 0 {
		timeout = time.Duration(config.DefaultAuthStatusTimeoutSecs) * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, command, "login", "status")
	cmd.Env = envWithOverride(os.Environ(), config.EnvCodexHome, codexHome)
	output, err := cmd.CombinedOutput()

	report := CodexLoginStatusReport{
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

func envWithOverride(base []string, key, value string) []string {
	if strings.TrimSpace(key) == "" {
		return append([]string(nil), base...)
	}
	prefix := key + "="
	env := make([]string, 0, len(base)+1)
	for _, item := range base {
		if strings.HasPrefix(item, prefix) {
			continue
		}
		env = append(env, item)
	}
	env = append(env, prefix+value)
	return env
}
