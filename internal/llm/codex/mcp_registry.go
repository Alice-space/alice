package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

const defaultMCPServerName = "alice-feishu"

type MCPRegistration struct {
	CodexCommand  string
	ServerName    string
	ServerCommand string
	ServerArgs    []string
}

type mcpServerConfig struct {
	Name      string
	Transport struct {
		Type    string   `json:"type"`
		Command string   `json:"command"`
		Args    []string `json:"args"`
	} `json:"transport"`
}

func EnsureMCPServerRegistered(ctx context.Context, cfg MCPRegistration) error {
	if strings.TrimSpace(cfg.CodexCommand) == "" {
		return errors.New("codex command is empty")
	}
	serverName := strings.TrimSpace(cfg.ServerName)
	if serverName == "" {
		serverName = defaultMCPServerName
	}
	serverCommand := strings.TrimSpace(cfg.ServerCommand)
	if serverCommand == "" {
		return errors.New("mcp server command is empty")
	}
	serverArgs := normalizeArgs(cfg.ServerArgs)

	getOutput, getErr := runCommand(ctx, cfg.CodexCommand, "mcp", "get", serverName, "--json")
	if getErr != nil {
		if isServerNotFoundError(getOutput) {
			return addMCPServer(ctx, cfg.CodexCommand, serverName, serverCommand, serverArgs)
		}
		return fmt.Errorf("query mcp server failed: %w (%s)", getErr, strings.TrimSpace(getOutput))
	}

	current, parseErr := parseMCPServerConfig(getOutput)
	if parseErr != nil {
		return parseErr
	}
	if mcpServerMatches(current, serverCommand, serverArgs) {
		return nil
	}

	if _, removeErr := runCommand(ctx, cfg.CodexCommand, "mcp", "remove", serverName); removeErr != nil {
		return fmt.Errorf("remove stale mcp server failed: %w", removeErr)
	}
	return addMCPServer(ctx, cfg.CodexCommand, serverName, serverCommand, serverArgs)
}

func addMCPServer(ctx context.Context, codexCommand, serverName, serverCommand string, serverArgs []string) error {
	args := make([]string, 0, 6+len(serverArgs))
	args = append(args, "mcp", "add", serverName, "--", serverCommand)
	args = append(args, serverArgs...)
	output, err := runCommand(ctx, codexCommand, args...)
	if err != nil {
		return fmt.Errorf("add mcp server failed: %w (%s)", err, strings.TrimSpace(output))
	}
	return nil
}

func parseMCPServerConfig(output string) (mcpServerConfig, error) {
	var config mcpServerConfig
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &config); err != nil {
		return mcpServerConfig{}, fmt.Errorf("parse mcp config failed: %w", err)
	}
	if strings.TrimSpace(config.Transport.Type) == "" {
		return mcpServerConfig{}, errors.New("mcp config missing transport type")
	}
	return config, nil
}

func mcpServerMatches(current mcpServerConfig, desiredCommand string, desiredArgs []string) bool {
	if strings.ToLower(strings.TrimSpace(current.Transport.Type)) != "stdio" {
		return false
	}
	if strings.TrimSpace(current.Transport.Command) != strings.TrimSpace(desiredCommand) {
		return false
	}
	currentArgs := normalizeArgs(current.Transport.Args)
	desiredArgs = normalizeArgs(desiredArgs)
	if len(currentArgs) != len(desiredArgs) {
		return false
	}
	for idx := range currentArgs {
		if currentArgs[idx] != desiredArgs[idx] {
			return false
		}
	}
	return true
}

func normalizeArgs(args []string) []string {
	if len(args) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(args))
	for _, arg := range args {
		trimmed := strings.TrimSpace(arg)
		if trimmed == "" {
			continue
		}
		normalized = append(normalized, trimmed)
	}
	return normalized
}

func isServerNotFoundError(output string) bool {
	output = strings.ToLower(strings.TrimSpace(output))
	if output == "" {
		return false
	}
	return strings.Contains(output, "no mcp server named")
}

func runCommand(ctx context.Context, command string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	output, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(output)), err
}
