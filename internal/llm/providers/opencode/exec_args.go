package opencode

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/Alice-space/alice/internal/llm/internal/shared"
)

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
