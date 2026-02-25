package codex

import (
	"sort"
	"strings"
)

func buildExecArgs(threadID string, prompt string, model string, profile string) []string {
	threadID = strings.TrimSpace(threadID)
	model = strings.TrimSpace(model)
	profile = strings.TrimSpace(profile)

	buildFlags := func() []string {
		args := []string{
			"--json",
			"--skip-git-repo-check",
			"--dangerously-bypass-approvals-and-sandbox",
		}
		if model != "" {
			args = append(args, "-m", model)
		}
		if profile != "" {
			args = append(args, "-p", profile)
		}
		return args
	}

	if threadID != "" {
		args := []string{
			"exec",
			"resume",
		}
		args = append(args, buildFlags()...)
		args = append(args, "--", threadID, prompt)
		return args
	}
	args := []string{
		"exec",
	}
	args = append(args, buildFlags()...)
	args = append(args, "--", prompt)
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

func mergeEnv(base []string, overrides map[string]string) []string {
	if len(overrides) == 0 {
		return base
	}

	env := make([]string, len(base))
	copy(env, base)

	indexByKey := make(map[string]int, len(env))
	for i, item := range env {
		key := envKey(item)
		if key == "" {
			continue
		}
		indexByKey[key] = i
	}

	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		pair := key + "=" + overrides[key]
		if idx, ok := indexByKey[key]; ok {
			env[idx] = pair
			continue
		}
		env = append(env, pair)
	}
	return env
}

func envKey(item string) string {
	idx := strings.Index(item, "=")
	if idx <= 0 {
		return ""
	}
	return item[:idx]
}
