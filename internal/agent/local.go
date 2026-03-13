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
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"alice/internal/platform"
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

	// Logger writes agent execution logs.
	// If nil, a noop logger is used.
	Logger platform.Logger

	// DebugTranscriptDir writes Markdown execution artifacts when non-empty.
	DebugTranscriptDir string
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
	logger platform.Logger
}

// NewLocalAgent creates a new local agent.
func NewLocalAgent(cfg Config) *LocalAgent {
	if cfg.KimiExecutable == "" {
		cfg.KimiExecutable = "kimi"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 120 * time.Second
	}
	logger := cfg.Logger
	if logger == nil {
		logger = platform.NewNoopLogger()
	}
	return &LocalAgent{
		config: cfg,
		logger: logger.WithComponent("agent"),
	}
}

// ExecuteRequest represents a request to execute with the local agent.
type ExecuteRequest struct {
	// RequestID is the parent Alice request identifier.
	RequestID string `json:"request_id,omitempty"`

	// EventID is the triggering event identifier.
	EventID string `json:"event_id,omitempty"`

	// Stage identifies which caller is executing the agent (for artifacts/logs).
	Stage string `json:"stage,omitempty"`

	// Operation identifies the execution contract the selected skills should follow.
	Operation string `json:"operation,omitempty"`

	// Task is optional free-form user input when the caller does not provide structured input.
	Task string `json:"task,omitempty"`

	// Skill is the skill name to load (e.g., "public-info-query").
	Skill string `json:"skill,omitempty"`

	// Skills loads additional skills in order.
	Skills []string `json:"skills,omitempty"`

	// Input provides structured request data for the loaded skills.
	Input map[string]any `json:"input,omitempty"`

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

	// FinalText is the last user-visible text part extracted from the raw transcript.
	FinalText string `json:"final_text,omitempty"`

	// StructuredOutput is the parsed structured output from MCP tool calls.
	StructuredOutput map[string]interface{} `json:"structured_output,omitempty"`

	// Transcript captures the raw prompt/output exchange for debug logging and auditing.
	Transcript Transcript `json:"transcript,omitempty"`

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
	start := time.Now().UTC()

	// Build the prompt with skill context
	prompt, err := a.buildPrompt(req)
	if err != nil {
		a.logger.Error("agent_prompt_build_failed", "skill", req.Skill, "error", err.Error())
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

	a.logger.Debug("agent_execution_started",
		"request_id", req.RequestID,
		"event_id", req.EventID,
		"stage", req.Stage,
		"skill", req.Skill,
		"skills", requestedSkills(req),
		"operation", req.Operation,
		"work_dir", a.config.WorkDir,
		"max_steps", a.config.MaxSteps,
		"timeout_sec", int64(a.config.Timeout.Seconds()),
		"task", req.Task,
		"input", req.Input,
		"constraints", req.Constraints,
		"rendered_prompt", prompt,
		"command", a.config.KimiExecutable,
		"args", args,
		"mcp_enabled", a.config.MCPServer != nil,
	)

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
	result.Transcript = parseTranscript(prompt, output)
	result.FinalText = result.Transcript.FinalText
	if internalErr := extractExecutionError(result.Transcript.RawConversation); internalErr != "" {
		result.Success = false
		if result.Error == "" {
			result.Error = internalErr
		}
	}

	// Try to extract structured output from MCP tool calls
	if a.config.MCPServer != nil {
		if structured := selectStructuredOutput(req, extractMCPToolOutputs(result.Transcript, output)); structured != nil {
			result.StructuredOutput = structured
		}
	}

	// Extract actions from stderr (kimi logs actions there)
	result.Actions = extractActions(stderr.String())

	a.logger.Debug("agent_execution_finished",
		"request_id", req.RequestID,
		"event_id", req.EventID,
		"stage", req.Stage,
		"skill", req.Skill,
		"success", result.Success,
		"duration_ms", time.Since(start).Milliseconds(),
		"error", result.Error,
		"stdout", output,
		"stderr", stderr.String(),
		"structured_output", result.StructuredOutput,
		"actions", result.Actions,
	)
	a.logger.Debug("agent_execution_transcript",
		"request_id", req.RequestID,
		"event_id", req.EventID,
		"stage", req.Stage,
		"skill", req.Skill,
		"call_prompt", result.Transcript.Prompt,
		"call_raw", result.Transcript.RawConversation,
		"call_final_text", result.Transcript.FinalText,
		"call_tool_calls", result.Transcript.ToolCalls,
	)
	a.writeDebugTranscriptArtifact(start, req, result)

	if result.Error != "" {
		return result, errors.New(result.Error)
	}

	return result, nil
}

func (a *LocalAgent) writeDebugTranscriptArtifact(start time.Time, req ExecuteRequest, result *ExecuteResult) {
	dir := strings.TrimSpace(a.config.DebugTranscriptDir)
	if dir == "" {
		return
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		a.logger.Warn("agent_execution_markdown_failed",
			"skill", req.Skill,
			"request_id", req.RequestID,
			"stage", req.Stage,
			"error", err.Error(),
		)
		return
	}

	stage := sanitizeArtifactPart(req.Stage)
	skill := sanitizeArtifactPart(req.Skill)
	requestID := sanitizeArtifactPart(req.RequestID)
	filename := fmt.Sprintf("%s_%s_%s_%s.md", start.Format("20060102T150405Z"), requestID, stage, skill)
	path := filepath.Join(dir, filename)
	content := renderTranscriptMarkdown(req, result, a.config.MCPServer != nil, time.Since(start))

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		a.logger.Warn("agent_execution_markdown_failed",
			"skill", req.Skill,
			"request_id", req.RequestID,
			"stage", req.Stage,
			"path", path,
			"error", err.Error(),
		)
		return
	}

	a.logger.Debug("agent_execution_markdown_written",
		"skill", req.Skill,
		"request_id", req.RequestID,
		"stage", req.Stage,
		"path", path,
	)
}

// buildPrompt constructs the full prompt with skill and constraints.
func (a *LocalAgent) buildPrompt(req ExecuteRequest) (string, error) {
	var parts []string

	for _, skillName := range requestedSkills(req) {
		if skillName == "mcp-tool-output" && a.config.MCPServer == nil {
			continue
		}
		skillContent, err := a.loadSkill(skillName)
		if err != nil {
			// Continue without skill when skill file is unavailable.
			a.logger.Warn("agent_skill_load_failed", "skill", skillName, "error", err.Error())
			continue
		}
		parts = append(parts, fmt.Sprintf("<skill name=%q>\n%s\n</skill>", skillName, skillContent))
	}

	payload := map[string]any{
		"operation":   strings.TrimSpace(req.Operation),
		"task":        strings.TrimSpace(req.Task),
		"input":       req.Input,
		"constraints": req.Constraints,
	}

	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal execution payload: %w", err)
	}

	parts = append(parts, "<alice-execution-request>")
	parts = append(parts, string(raw))
	parts = append(parts, "</alice-execution-request>")

	return strings.Join(parts, "\n\n"), nil
}

// loadSkill loads a skill from the skills directory.
func (a *LocalAgent) loadSkill(skillName string) (string, error) {
	skillPath := filepath.Join(a.config.SkillsDir, skillName, "SKILL.md")
	content, err := os.ReadFile(skillPath)
	if err != nil {
		return "", fmt.Errorf("read skill file %s: %w", skillPath, err)
	}
	return stripFrontMatter(string(content)), nil
}

func requestedSkills(req ExecuteRequest) []string {
	var ordered []string
	seen := make(map[string]struct{})
	appendSkill := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		ordered = append(ordered, name)
	}

	appendSkill(req.Skill)
	for _, skill := range req.Skills {
		appendSkill(skill)
	}
	return ordered
}

func stripFrontMatter(content string) string {
	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(trimmed, "---\n") {
		return trimmed
	}
	rest := strings.TrimPrefix(trimmed, "---\n")
	idx := strings.Index(rest, "\n---\n")
	if idx < 0 {
		return trimmed
	}
	return strings.TrimSpace(rest[idx+5:])
}

// MCPToolOutput represents the wrapper format from MCP server
type MCPToolOutput struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// extractMCPToolOutput extracts structured data from MCP tool call outputs.
// The MCP server wraps tool outputs in a JSON format with "type" and "payload" fields.
func extractMCPToolOutputs(transcript Transcript, text string) []map[string]interface{} {
	var outputs []map[string]interface{}

	for _, call := range transcript.ToolCalls {
		if structured := parseMCPToolOutput(call.ResultText); structured != nil {
			structured["_tool_name"] = call.Name
			outputs = append(outputs, structured)
		}
	}
	if len(outputs) > 0 {
		return outputs
	}

	if structured := parseMCPToolOutput(text); structured != nil {
		outputs = append(outputs, structured)
	}

	return outputs
}

func selectStructuredOutput(req ExecuteRequest, outputs []map[string]interface{}) map[string]interface{} {
	if len(outputs) == 0 {
		return nil
	}

	if expectedType := expectedOutputType(req); expectedType != "" {
		for _, output := range outputs {
			if outputType(output) == expectedType {
				return output
			}
		}
	}

	return outputs[len(outputs)-1]
}

func expectedOutputType(req ExecuteRequest) string {
	switch {
	case req.Stage == "reception", req.Skill == "reception-assessment", req.Operation == "reception_assessment":
		return "promotion_decision"
	case req.Stage == "direct_answer", req.Operation == "direct_answer":
		return "direct_answer"
	default:
		return ""
	}
}

func outputType(m map[string]interface{}) string {
	if v, ok := m["_output_type"].(string); ok {
		return v
	}
	return ""
}

func parseMCPToolOutput(text string) map[string]interface{} {
	var last map[string]interface{}
	for _, candidate := range jsonTextCandidates(text) {
		for _, normalized := range normalizeJSONCandidates(candidate) {
			var wrapper MCPToolOutput
			if err := json.Unmarshal([]byte(normalized), &wrapper); err != nil {
				continue
			}

			switch wrapper.Type {
			case "promotion_decision", "direct_answer", "tool_call":
				var result map[string]interface{}
				if err := json.Unmarshal(wrapper.Payload, &result); err != nil {
					continue
				}
				result["_output_type"] = wrapper.Type
				last = result
			}
		}
	}
	return last
}

func extractMCPToolOutput(text string) map[string]interface{} {
	var last map[string]interface{}
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		for _, candidate := range jsonLineCandidates(line) {
			if parsed := parseMCPToolOutput(candidate); parsed != nil {
				last = parsed
			}
		}
	}

	return last
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

func extractExecutionError(output string) string {
	idx := strings.Index(output, "Error code:")
	if idx < 0 {
		return ""
	}
	line := output[idx:]
	if newline := strings.Index(line, "\n"); newline >= 0 {
		line = line[:newline]
	}
	return strings.TrimSpace(line)
}

func jsonLineCandidates(line string) []string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return nil
	}

	candidates := []string{trimmed}
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start >= 0 && end > start {
		snippet := strings.TrimSpace(trimmed[start : end+1])
		if snippet != trimmed {
			candidates = append(candidates, snippet)
		}
	}
	return candidates
}

func normalizeJSONCandidates(candidate string) []string {
	normalized := []string{candidate}
	if strings.Contains(candidate, `\"`) {
		if unquoted, err := strconv.Unquote(`"` + candidate + `"`); err == nil {
			normalized = append(normalized, unquoted)
		}
	}
	compacted := strings.NewReplacer("\r", "", "\n", "", "\t", "").Replace(candidate)
	if compacted != candidate {
		normalized = append(normalized, compacted)
		if strings.Contains(compacted, `\"`) {
			if unquoted, err := strconv.Unquote(`"` + compacted + `"`); err == nil {
				normalized = append(normalized, unquoted)
			}
		}
	}
	return normalized
}

func jsonTextCandidates(text string) []string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}

	candidates := []string{trimmed}
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start >= 0 && end > start {
		snippet := strings.TrimSpace(trimmed[start : end+1])
		if snippet != trimmed {
			candidates = append(candidates, snippet)
		}
	}
	return candidates
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
