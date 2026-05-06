package opencode

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/Alice-space/alice/internal/llm/internal/shared"
)

func buildRunArgs(threadID, prompt, model, variant string) []string {
	args := []string{"run"}
	if model = strings.TrimSpace(model); model != "" {
		args = append(args, "--model", model)
	}
	if variant = strings.TrimSpace(variant); variant != "" {
		args = append(args, "--variant", variant)
	}
	args = append(args, "--format", "json")
	if threadID = strings.TrimSpace(threadID); threadID != "" {
		args = append(args, "--session", threadID)
	}
	args = append(args, "--", prompt)
	return args
}

type LoginReport struct {
	Command string
	Version string
	Ready   bool
	Error   string
}

func CheckLogin(command string, timeout time.Duration) (LoginReport, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		command = "opencode"
	}
	if timeout <= 0 {
		timeout = shared.AuthCheckTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, command, "--version")
	output, err := cmd.Output()
	if err != nil {
		report := LoginReport{
			Command: command,
			Ready:   false,
			Error:   err.Error(),
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			report.Error = string(exitErr.Stderr)
		}
		return report, nil
	}
	return LoginReport{
		Command: command,
		Version: strings.TrimSpace(string(output)),
		Ready:   true,
	}, nil
}

func formatLoginError(runErr error, stderr string) error {
	lower := strings.ToLower(stderr)
	if strings.Contains(lower, "not authenticated") || strings.Contains(lower, "not logged in") || strings.Contains(lower, "no api key") {
		return fmt.Errorf("%w; opencode is not authenticated for this provider — run 'opencode auth login' first", runErr)
	}
	return runErr
}
