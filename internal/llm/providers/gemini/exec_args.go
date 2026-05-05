package gemini

import "strings"

func buildExecArgs(threadID string, prompt string, model string) []string {
	args := []string{
		"-p",
		strings.TrimSpace(prompt),
		"--output-format",
		"json",
	}
	if model = strings.TrimSpace(model); model != "" {
		args = append(args, "--model", model)
	}
	if threadID = strings.TrimSpace(threadID); threadID != "" {
		args = append(args, "--resume", threadID)
	}
	return args
}
