package campaignrepo

import "strings"

func resolveExecutorRole(repo Repository, task TaskDocument) RoleConfig {
	return resolveRoleConfig(defaultExecutorRoleConfig(), repo.Campaign.Frontmatter.DefaultExecutor, task.Frontmatter.Executor, "executor")
}

func resolveReviewerRole(repo Repository, task TaskDocument) RoleConfig {
	return resolveRoleConfig(defaultReviewerRoleConfig(), repo.Campaign.Frontmatter.DefaultReviewer, task.Frontmatter.Reviewer, "reviewer")
}

func resolveRoleConfig(base RoleConfig, campaignDefault RoleConfig, taskOverride RoleConfig, kind string) RoleConfig {
	cfg := mergeRoleConfig(base, campaignDefault)
	cfg = mergeRoleConfig(cfg, taskOverride)
	if cfg.Role == "" {
		cfg.Role = kind
	}
	if cfg.Provider == "" {
		cfg.Provider = providerFromRole(cfg.Role)
	}
	if cfg.Workflow == "" {
		cfg.Workflow = "code_army"
	}
	if cfg.ReasoningEffort == "" {
		cfg.ReasoningEffort = "high"
	}
	if cfg.Personality == "" {
		if kind == "reviewer" {
			cfg.Personality = "analytical"
		} else {
			cfg.Personality = "pragmatic"
		}
	}
	return normalizeRoleConfig(cfg)
}

func mergeRoleConfig(base RoleConfig, overlay RoleConfig) RoleConfig {
	overlay = normalizeRoleConfig(overlay)
	if overlay.Role != "" {
		base.Role = overlay.Role
	}
	if overlay.Provider != "" {
		base.Provider = overlay.Provider
	}
	if overlay.Model != "" {
		base.Model = overlay.Model
	}
	if overlay.Profile != "" {
		base.Profile = overlay.Profile
	}
	if overlay.Workflow != "" {
		base.Workflow = overlay.Workflow
	}
	if overlay.ReasoningEffort != "" {
		base.ReasoningEffort = overlay.ReasoningEffort
	}
	if overlay.Personality != "" {
		base.Personality = overlay.Personality
	}
	return normalizeRoleConfig(base)
}

func defaultExecutorRoleConfig() RoleConfig {
	return normalizeRoleConfig(RoleConfig{
		Role:            "executor.codex",
		Provider:        "codex",
		Workflow:        "code_army",
		ReasoningEffort: "high",
		Personality:     "pragmatic",
	})
}

func defaultReviewerRoleConfig() RoleConfig {
	return normalizeRoleConfig(RoleConfig{
		Role:            "reviewer.claude",
		Provider:        "claude",
		Workflow:        "code_army",
		ReasoningEffort: "high",
		Personality:     "analytical",
	})
}

func providerFromRole(role string) string {
	role = strings.ToLower(strings.TrimSpace(role))
	switch {
	case strings.Contains(role, "codex"):
		return "codex"
	case strings.Contains(role, "claude"):
		return "claude"
	case strings.Contains(role, "gemini"):
		return "gemini"
	case strings.Contains(role, "kimi"):
		return "kimi"
	default:
		return ""
	}
}

func roleLabel(role RoleConfig) string {
	if value := strings.TrimSpace(role.Role); value != "" {
		return value
	}
	if value := strings.TrimSpace(role.Provider); value != "" {
		return value
	}
	return "agent"
}
