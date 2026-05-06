package claude

import (
	"strings"
)

func buildExecArgs(threadID string, prompt string, model string) []string {
	threadID = strings.TrimSpace(threadID)
	model = strings.TrimSpace(model)

	args := []string{
		"-p",
		"--output-format",
		"stream-json",
		"--verbose",
		"--permission-mode",
		"bypassPermissions",
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	if threadID != "" {
		args = append(args, "--resume", threadID)
	}
	args = append(args, "--", prompt)
	return args
}
