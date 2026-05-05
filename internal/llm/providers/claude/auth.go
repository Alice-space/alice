package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// LoginStatusReport holds the result of a Claude CLI login status check.
type LoginStatusReport struct {
	Command     string
	LoggedIn    bool
	AuthMethod  string
	APIProvider string
	Output      string
}

// CheckLogin runs "claude auth status" and returns the login state.
// If timeout is zero or negative, a 15-second default is used.
func CheckLogin(command string, timeout time.Duration) (LoginStatusReport, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return LoginStatusReport{}, fmt.Errorf("claude command is empty")
	}
	if timeout <= 0 {
		timeout = 15 * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, command, "auth", "status")
	output, err := cmd.CombinedOutput()

	report := LoginStatusReport{
		Command: command,
		Output:  strings.TrimSpace(string(output)),
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return report, fmt.Errorf("check claude login status timed out: %w", ctx.Err())
	}

	var parsed struct {
		LoggedIn    bool   `json:"loggedIn"`
		AuthMethod  string `json:"authMethod"`
		APIProvider string `json:"apiProvider"`
	}
	if decodeErr := json.Unmarshal(output, &parsed); decodeErr == nil {
		report.LoggedIn = parsed.LoggedIn
		report.AuthMethod = strings.TrimSpace(parsed.AuthMethod)
		report.APIProvider = strings.TrimSpace(parsed.APIProvider)
	}

	if err == nil {
		return report, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return report, nil
	}
	return report, fmt.Errorf("run %q auth status failed: %w", command, err)
}
