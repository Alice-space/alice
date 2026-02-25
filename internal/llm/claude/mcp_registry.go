package claude

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

const defaultMCPServerName = "alice-feishu"

type MCPRegistration struct {
	ClaudeCommand string
	ServerName    string
	ServerCommand string
	ServerArgs    []string
}

type mcpServerConfig struct {
	TransportType string
	Command       string
	Args          []string
}

func EnsureMCPServerRegistered(ctx context.Context, cfg MCPRegistration) error {
	if strings.TrimSpace(cfg.ClaudeCommand) == "" {
		return errors.New("claude command is empty")
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

	getOutput, getErr := runCommand(ctx, cfg.ClaudeCommand, "mcp", "get", serverName)
	if getErr != nil {
		if isServerNotFoundError(getOutput) {
			return addMCPServer(ctx, cfg.ClaudeCommand, serverName, serverCommand, serverArgs)
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

	removeOutput, removeErr := runCommand(ctx, cfg.ClaudeCommand, "mcp", "remove", serverName)
	if removeErr != nil && !isServerNotFoundError(removeOutput) {
		return fmt.Errorf("remove stale mcp server failed: %w (%s)", removeErr, strings.TrimSpace(removeOutput))
	}
	return addMCPServer(ctx, cfg.ClaudeCommand, serverName, serverCommand, serverArgs)
}

func addMCPServer(ctx context.Context, claudeCommand, serverName, serverCommand string, serverArgs []string) error {
	args := make([]string, 0, 6+len(serverArgs))
	args = append(args, "mcp", "add", serverName, "--", serverCommand)
	args = append(args, serverArgs...)
	output, err := runCommand(ctx, claudeCommand, args...)
	if err != nil {
		return fmt.Errorf("add mcp server failed: %w (%s)", err, strings.TrimSpace(output))
	}
	return nil
}

func parseMCPServerConfig(output string) (mcpServerConfig, error) {
	scanner := bufio.NewScanner(strings.NewReader(output))
	config := mcpServerConfig{}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, "Type:"):
			config.TransportType = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "Type:")))
		case strings.HasPrefix(line, "Command:"):
			config.Command = strings.TrimSpace(strings.TrimPrefix(line, "Command:"))
		case strings.HasPrefix(line, "Args:"):
			rawArgs := strings.TrimSpace(strings.TrimPrefix(line, "Args:"))
			if rawArgs == "" || strings.EqualFold(rawArgs, "(none)") {
				config.Args = nil
				continue
			}
			config.Args = strings.Fields(rawArgs)
		}
	}
	if err := scanner.Err(); err != nil {
		return mcpServerConfig{}, fmt.Errorf("parse mcp config failed: %w", err)
	}
	if strings.TrimSpace(config.TransportType) == "" {
		return mcpServerConfig{}, errors.New("mcp config missing transport type")
	}
	if strings.TrimSpace(config.Command) == "" {
		return mcpServerConfig{}, errors.New("mcp config missing command")
	}
	config.Args = normalizeArgs(config.Args)
	return config, nil
}

func mcpServerMatches(current mcpServerConfig, desiredCommand string, desiredArgs []string) bool {
	if strings.ToLower(strings.TrimSpace(current.TransportType)) != "stdio" {
		return false
	}
	if strings.TrimSpace(current.Command) != strings.TrimSpace(desiredCommand) {
		return false
	}
	currentArgs := normalizeArgs(current.Args)
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
	lowered := strings.ToLower(strings.TrimSpace(output))
	if lowered == "" {
		return false
	}
	return strings.Contains(lowered, "mcp server") &&
		strings.Contains(lowered, "no ") &&
		strings.Contains(lowered, "found") &&
		strings.Contains(lowered, "name")
}

func runCommand(ctx context.Context, command string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	output, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(output)), err
}
