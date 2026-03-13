package logging

import (
	"log"
	"strings"
	"sync/atomic"
)

const (
	levelDebug int32 = iota
	levelInfo
	levelWarn
	levelError
)

var currentLevel atomic.Int32

func init() {
	currentLevel.Store(levelInfo)
}

func SetLevel(level string) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		currentLevel.Store(levelDebug)
	case "warn", "warning":
		currentLevel.Store(levelWarn)
	case "error":
		currentLevel.Store(levelError)
	default:
		currentLevel.Store(levelInfo)
	}
}

func IsDebugEnabled() bool {
	return currentLevel.Load() <= levelDebug
}

func Debugf(format string, args ...any) {
	if !IsDebugEnabled() {
		return
	}
	log.Printf("[DEBUG] "+format, args...)
}

type AgentTrace struct {
	Provider  string
	Agent     string
	ThreadID  string
	Model     string
	Profile   string
	Input     string
	Output    string
	ToolCalls []string
	Error     string
}

func DebugAgentTrace(trace AgentTrace) {
	if !IsDebugEnabled() {
		return
	}

	sections := []string{
		"# Agent Trace",
		"",
		"- provider: `" + defaultString(trace.Provider, "unknown") + "`",
		"- agent: `" + defaultString(trace.Agent, "assistant") + "`",
		"- thread_id: `" + defaultString(trace.ThreadID, "-") + "`",
		"- model: `" + defaultString(trace.Model, "-") + "`",
		"- profile: `" + defaultString(trace.Profile, "-") + "`",
	}
	if strings.TrimSpace(trace.Error) != "" {
		sections = append(sections,
			"- error: `"+strings.TrimSpace(trace.Error)+"`",
		)
	}
	sections = append(sections,
		"",
		"## Input",
		codeBlock(trace.Input),
		"",
		"## Tool Calls",
	)
	if len(trace.ToolCalls) == 0 {
		sections = append(sections, "- none")
	} else {
		for _, item := range trace.ToolCalls {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			sections = append(sections, "- "+item)
		}
	}
	sections = append(sections,
		"",
		"## Output",
		codeBlock(trace.Output),
	)
	log.Printf("[DEBUG][agent-trace]\n%s", strings.Join(sections, "\n"))
}

func codeBlock(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		trimmed = "(empty)"
	}
	return "```md\n" + trimmed + "\n```"
}

func defaultString(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
