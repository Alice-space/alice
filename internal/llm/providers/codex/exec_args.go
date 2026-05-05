package codex

import (
	"sort"
	"strconv"
	"strings"
)

func buildExecArgs(
	threadID string,
	prompt string,
	model string,
	profile string,
	reasoningEffort string,
	personality string,
	policy ExecPolicyConfig,
) []string {
	threadID = strings.TrimSpace(threadID)
	model = strings.TrimSpace(model)
	profile = strings.TrimSpace(profile)
	reasoningEffort = strings.TrimSpace(reasoningEffort)
	personality = strings.TrimSpace(personality)
	policy.Sandbox = strings.TrimSpace(policy.Sandbox)
	policy.AskForApproval = strings.TrimSpace(policy.AskForApproval)
	policy.AddDirs = uniqueAddDirs(policy.AddDirs)
	usesDangerousBypass := shouldUseDangerousBypass(policy)

	buildRootFlags := func() []string {
		args := make([]string, 0, 10+len(policy.AddDirs)*2)
		if !usesDangerousBypass && policy.AskForApproval != "" {
			args = append(args, "-a", policy.AskForApproval)
		}
		if !usesDangerousBypass && policy.Sandbox != "" {
			args = append(args, "--sandbox", policy.Sandbox)
		}
		for _, dir := range policy.AddDirs {
			args = append(args, "--add-dir", dir)
		}
		if model != "" {
			args = append(args, "-m", model)
		}
		if profile != "" {
			args = append(args, "-p", profile)
		}
		if reasoningEffort != "" {
			args = append(args, "-c", "model_reasoning_effort="+strconv.Quote(reasoningEffort))
		}
		if personality != "" {
			args = append(args, "-c", "personality="+strconv.Quote(personality))
		}
		return args
	}

	buildExecFlags := func() []string {
		args := []string{
			"--json",
			"--skip-git-repo-check",
		}
		if usesDangerousBypass {
			// When Alice wants a full-access, no-approval run, use Codex's explicit
			// bypass mode so the initial work-thread turn sees the same permissions
			// as later resume turns.
			args = append(args, "--dangerously-bypass-approvals-and-sandbox")
		}
		return args
	}

	args := buildRootFlags()
	if threadID != "" {
		args = append(args, []string{
			"exec",
			"resume",
		}...)
		args = append(args, buildExecFlags()...)
		args = append(args, "--", threadID, prompt)
		return args
	}
	args = append(args, []string{
		"exec",
	}...)
	args = append(args, buildExecFlags()...)
	args = append(args, "--", prompt)
	return args
}

func shouldUseDangerousBypass(policy ExecPolicyConfig) bool {
	return policy.Sandbox == "danger-full-access" &&
		policy.AskForApproval == defaultApprovalMode
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
