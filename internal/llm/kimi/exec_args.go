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
		args = append(args, "-S", threadID, "-C")
	}
	args = append(args, "-p", strings.TrimSpace(prompt))
	return args
}

func buildPrompt(threadID string, promptPrefix string, userText string) string {
	trimmedThreadID := strings.TrimSpace(threadID)
	trimmedPrefix := strings.TrimSpace(promptPrefix)
	trimmedUserText := strings.TrimSpace(userText)
	if trimmedThreadID != "" {
		return trimmedUserText
	}
	if trimmedPrefix == "" {
		return trimmedUserText
	}
	return trimmedPrefix + "\n\n" + trimmedUserText
}
