// Package agent provides local LLM agent integration for Alice.
//
// It wraps the kimi CLI as a local agent that can execute commands and access files.
// This package uses MCP (Model Context Protocol) via HTTP to enable structured
// tool calling instead of parsing JSON from free-text responses.
//
// The agent connects to an embedded MCP HTTP server that provides tools like:
// - submit_promotion_decision: For Reception to submit routing decisions
// - submit_direct_answer: For direct query responses
// - submit_tool_call: For requesting tool/MCP invocations
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"alice/internal/prompts"
)

// MCPServer defines the interface for MCP server that agent needs.
type MCPServer interface {
	// URL returns the HTTP URL for connecting to this server.
	URL() string
	// ConfigJSON returns the MCP configuration JSON for kimi CLI.
	ConfigJSON(serverName string) string
}

// Config holds the configuration for local agent.
type Config struct {
	// KimiExecutable is the path to kimi CLI. Default: "kimi" (from PATH)
	KimiExecutable string

	// WorkDir is the working directory for agent execution.
	WorkDir string

	// Timeout for agent execution.
	Timeout time.Duration

	// MaxSteps limits the number of steps kimi can take.
	MaxSteps int

	// SkillsDir is the path to skills directory.
	SkillsDir string

	// MCPServer provides the MCP HTTP server for tool calling.
	// If nil, agent will not use MCP (fallback to legacy JSON parsing).
	MCPServer MCPServer
}

// DefaultConfig returns default configuration.
func DefaultConfig() Config {
	return Config{
		KimiExecutable: "kimi",
		WorkDir:        ".",
		Timeout:        120 * time.Second,
		MaxSteps:       10,
		SkillsDir:      "skills",
	}
}

// LocalAgent wraps the kimi CLI as a local agent.
type LocalAgent struct {
	config Config
}

// NewLocalAgent creates a new local agent.
func NewLocalAgent(cfg Config) *LocalAgent {
	if cfg.KimiExecutable == "" {
		cfg.KimiExecutable = "kimi"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 120 * time.Second
	}
	return &LocalAgent{config: cfg}
}

// ExecuteRequest represents a request to execute with the local agent.
type ExecuteRequest struct {
	// Task is the user task description.
	Task string `json:"task"`

	// SystemPrompt is the system-level instruction.
	SystemPrompt string `json:"system_prompt,omitempty"`

	// Skill is the skill name to load (e.g., "public-info-query").
	Skill string `json:"skill,omitempty"`

	// Context provides additional context (file paths, previous results, etc.).
	Context map[string]string `json:"context,omitempty"`

	// Constraints limit what the agent can do.
	Constraints ExecuteConstraints `json:"constraints,omitempty"`
}

// ExecuteConstraints defines execution limits.
type ExecuteConstraints struct {
	// ReadOnly prevents any write operations.
	ReadOnly bool `json:"read_only,omitempty"`

	// AllowedPaths limits which paths can be accessed.
	AllowedPaths []string `json:"allowed_paths,omitempty"`

	// DisallowedCommands blocks specific commands.
	DisallowedCommands []string `json:"disallowed_commands,omitempty"`
}

// ExecuteResult represents the result of agent execution.
type ExecuteResult struct {
	// Output is the final text output from the agent.
	Output string `json:"output"`

	// StructuredOutput is the parsed structured output from MCP tool calls.
	StructuredOutput map[string]interface{} `json:"structured_output,omitempty"`

	// Actions lists what actions the agent took.
	Actions []string `json:"actions,omitempty"`

	// Success indicates if the execution succeeded.
	Success bool `json:"success"`

	// Error message if execution failed.
	Error string `json:"error,omitempty"`

	// TokenUsage reports token consumption.
	TokenUsage struct {
		Prompt     int `json:"prompt"`
		Completion int `json:"completion"`
		Total      int `json:"total"`
	} `json:"token_usage,omitempty"`
}

// Execute runs the kimi CLI with the given request using MCP tool calling.
func (a *LocalAgent) Execute(ctx context.Context, req ExecuteRequest) (*ExecuteResult, error) {
	// Build the prompt with skill context
	prompt, err := a.buildPrompt(req)
	if err != nil {
		return nil, fmt.Errorf("build prompt: %w", err)
	}

	// Build kimi arguments
	args := []string{
		"--print",                      // Non-interactive mode
		"--yolo",                       // Auto-approve actions
		"--work-dir", a.config.WorkDir, // Working directory
		"--max-steps-per-turn", fmt.Sprintf("%d", a.config.MaxSteps),
		"--prompt", prompt,
	}

	// Add MCP configuration if available
	if a.config.MCPServer != nil {
		mcpConfig := a.config.MCPServer.ConfigJSON("alice-tools")
		args = append(args, "--mcp-config", mcpConfig)
	}

	// Add skills directory if specified
	if a.config.SkillsDir != "" {
		args = append(args, "--skills-dir", a.config.SkillsDir)
	}

	// Create command with timeout context
	ctx, cancel := context.WithTimeout(ctx, a.config.Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, a.config.KimiExecutable, args...)

	// Capture stdout and stderr
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Set environment
	cmd.Env = os.Environ()

	// Execute
	err = cmd.Run()

	result := &ExecuteResult{
		Success: err == nil,
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			result.Error = "execution timeout"
		} else {
			result.Error = fmt.Sprintf("execution failed: %v, stderr: %s", err, stderr.String())
		}
	}

	// Parse output
	output := stdout.String()
	result.Output = output

	// Try to extract structured output from MCP tool calls
	if a.config.MCPServer != nil {
		if structured := extractMCPToolOutput(output); structured != nil {
			result.StructuredOutput = structured
		}
	}

	// Extract actions from stderr (kimi logs actions there)
	result.Actions = extractActions(stderr.String())

	return result, nil
}

// buildPrompt constructs the full prompt with skill and constraints.
func (a *LocalAgent) buildPrompt(req ExecuteRequest) (string, error) {
	var parts []string

	// Add skill instruction if specified
	if req.Skill != "" {
		skillContent, err := a.loadSkill(req.Skill)
		if err != nil {
			// Log warning but continue without skill
			fmt.Printf("Warning: failed to load skill %s: %v\n", req.Skill, err)
		} else {
			parts = append(parts, skillContent)
		}
	}

	// Add system prompt
	if req.SystemPrompt != "" {
		parts = append(parts, "# System Instructions\n\n"+req.SystemPrompt)
	}

	// Add constraints
	if req.Constraints.ReadOnly {
		parts = append(parts, "# Constraints\n\nYou are in READ-ONLY mode. Do not modify any files or execute write operations.")
	}

	// Add context
	if len(req.Context) > 0 {
		parts = append(parts, "# Context\n")
		for key, value := range req.Context {
			parts = append(parts, fmt.Sprintf("- **%s**: %s", key, value))
		}
	}

	// Add the main task
	parts = append(parts, "# Task\n\n"+req.Task)

	// Add output format instruction from embedded template
	parts = append(parts, prompts.Get(prompts.LocalAgentOutputFormat))

	return strings.Join(parts, "\n\n"), nil
}

// loadSkill loads a skill from the skills directory.
func (a *LocalAgent) loadSkill(skillName string) (string, error) {
	skillPath := filepath.Join(a.config.SkillsDir, skillName, "SKILL.md")
	content, err := os.ReadFile(skillPath)
	if err != nil {
		return "", fmt.Errorf("read skill file %s: %w", skillPath, err)
	}
	return string(content), nil
}

// MCPToolOutput represents the wrapper format from MCP server
type MCPToolOutput struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// extractMCPToolOutput extracts structured data from MCP tool call outputs.
// The MCP server wraps tool outputs in a JSON format with "type" and "payload" fields.
func extractMCPToolOutput(text string) map[string]interface{} {
	// Look for MCP tool output markers
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Try to parse as MCP output wrapper
		var wrapper MCPToolOutput
		if err := json.Unmarshal([]byte(line), &wrapper); err != nil {
			continue
		}

		// Validate known types
		switch wrapper.Type {
		case "promotion_decision", "direct_answer", "tool_call":
			// Parse the payload into a generic map
			var result map[string]interface{}
			if err := json.Unmarshal(wrapper.Payload, &result); err != nil {
				continue
			}
			// Add the type to the result for context
			result["_output_type"] = wrapper.Type
			return result
		}
	}

	return nil
}

// extractActions extracts action descriptions from kimi stderr logs.
func extractActions(stderr string) []string {
	var actions []string
	lines := strings.Split(stderr, "\n")
	for _, line := range lines {
		// Look for action patterns in kimi logs
		if strings.Contains(line, "Action:") || strings.Contains(line, "Running:") {
			actions = append(actions, strings.TrimSpace(line))
		}
	}
	return actions
}

// Health checks if the local agent is available.
func (a *LocalAgent) Health(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, a.config.KimiExecutable, "--version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("kimi not available: %w, output: %s", err, out)
	}
	return nil
}
