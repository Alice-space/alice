package kimi

import "strings"

func buildExecArgs(threadID string, prompt string, model string) []string {
	args := []string{
		"--print",
		"--output-format",
		"stream-json",
	}
	if model = strings.TrimSpace(model); model != "" {
		args = append(args, "-m", model)
	}
	if threadID = strings.TrimSpace(threadID); threadID != "" {
		args = append(args, "-S", threadID)
	}
	args = append(args, "-p", strings.TrimSpace(prompt))
	return args
}
