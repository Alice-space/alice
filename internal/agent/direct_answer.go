package agent

import (
	"context"
	"fmt"
	"time"

	"alice/internal/platform"
)

// DirectAnswerExecutor handles direct answer path execution using local agent.
type DirectAnswerExecutor struct {
	agent  *LocalAgent
	logger Logger
}

// Logger is an alias to platform.Logger.
type Logger = platform.Logger

// NewDirectAnswerExecutor creates a new direct answer executor.
func NewDirectAnswerExecutor(agent *LocalAgent, logger Logger) *DirectAnswerExecutor {
	return &DirectAnswerExecutor{
		agent:  agent,
		logger: logger.WithComponent("direct_answer"),
	}
}

// ExecuteRequest represents a direct answer request.
type DirectAnswerRequest struct {
	RequestID  string
	EventID    string
	TraceID    string
	UserInput  string
	IntentKind string
	Context    map[string]string
	Skill      string
}

// ExecuteResult represents the result of direct answer execution.
type DirectAnswerResult struct {
	Answer     string
	Confidence float64
	Sources    []string
	DurationMS int64
	TokenUsage struct {
		Prompt     int
		Completion int
		Total      int
	}
}

// Execute performs the direct answer execution.
func (e *DirectAnswerExecutor) Execute(ctx context.Context, req DirectAnswerRequest) (*DirectAnswerResult, error) {
	start := time.Now().UTC()

	e.logger.Info("direct_answer_started",
		"request_id", req.RequestID,
		"event_id", req.EventID,
		"intent_kind", req.IntentKind,
		"user_input", truncate(req.UserInput, 100),
	)

	// Execute with local agent
	agentReq := ExecuteRequest{
		RequestID: req.RequestID,
		EventID:   req.EventID,
		Stage:     "direct_answer",
		Operation: "direct_answer",
		Skills:    directAnswerSkills(req.Skill),
		Input:     e.buildAgentInput(req),
		Constraints: ExecuteConstraints{
			ReadOnly: true,
		},
	}

	agentResult, err := e.agent.Execute(ctx, agentReq)
	if err != nil {
		e.logger.Error("direct_answer_failed",
			"request_id", req.RequestID,
			"error", err.Error(),
			"duration_ms", time.Since(start).Milliseconds(),
		)
		return nil, fmt.Errorf("agent execution failed: %w", err)
	}

	duration := time.Since(start)
	result := &DirectAnswerResult{
		DurationMS: duration.Milliseconds(),
	}

	if agentResult.StructuredOutput != nil {
		if answer := getString(agentResult.StructuredOutput, "answer"); answer != "" {
			result.Answer = answer
		}
		if conf, ok := agentResult.StructuredOutput["confidence"].(float64); ok {
			result.Confidence = conf
		}
		if citations := getStringSlice(agentResult.StructuredOutput, "citations"); len(citations) > 0 {
			result.Sources = append(result.Sources, citations...)
		}
		if sources := getStringSlice(agentResult.StructuredOutput, "sources"); len(sources) > 0 {
			result.Sources = append(result.Sources, sources...)
		}
	}

	if result.Answer == "" && agentResult.FinalText != "" {
		result.Answer = agentResult.FinalText
	}
	if result.Answer == "" {
		result.Answer = agentResult.Output
	}

	// Default confidence if not extracted
	if result.Confidence == 0 {
		result.Confidence = 0.85
	}

	e.logger.Info("direct_answer_completed",
		"request_id", req.RequestID,
		"duration_ms", result.DurationMS,
		"confidence", result.Confidence,
		"token_usage_total", result.TokenUsage.Total,
	)

	return result, nil
}

func (e *DirectAnswerExecutor) buildAgentInput(req DirectAnswerRequest) map[string]any {
	input := map[string]any{
		"request_id":  req.RequestID,
		"event_id":    req.EventID,
		"trace_id":    req.TraceID,
		"intent_kind": req.IntentKind,
		"user_input":  req.UserInput,
	}
	if len(req.Context) > 0 {
		input["context"] = stringMapToAny(req.Context)
	}
	return input
}

func directAnswerSkills(extraSkill string) []string {
	skills := []string{"mcp-tool-output", "direct-answer"}
	if extraSkill != "" {
		skills = append(skills, extraSkill)
	}
	return skills
}

func stringMapToAny(values map[string]string) map[string]any {
	if len(values) == 0 {
		return nil
	}
	result := make(map[string]any, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getStringSlice(m map[string]interface{}, key string) []string {
	if arr, ok := m[key].([]interface{}); ok {
		result := make([]string, 0, len(arr))
		for _, v := range arr {
			if s, ok := v.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	return nil
}
