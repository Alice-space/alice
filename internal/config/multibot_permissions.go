package config

import "fmt"

func normalizeBotPermissions(in BotPermissionsConfig) BotPermissionsConfig {
	if in.RuntimeMessage == nil {
		in.RuntimeMessage = boolPtr(true)
	}
	if in.RuntimeAutomation == nil {
		in.RuntimeAutomation = boolPtr(true)
	}
	if in.RuntimeCampaigns == nil {
		in.RuntimeCampaigns = boolPtr(true)
	}
	in.AllowedSkills = normalizeStringSlice(in.AllowedSkills)
	return in
}

func normalizeCodexExecPolicy(in CodexExecPolicyConfig) CodexExecPolicyConfig {
	in.Sandbox = normalizeLLMProvider(in.Sandbox)
	in.AskForApproval = normalizeLLMProvider(in.AskForApproval)
	in.AddDirs = normalizePathSlice(in.AddDirs)
	return in
}

func validateBotPermissions(_ BotPermissionsConfig) error {
	return nil
}

func validateCodexExecPolicy(policy CodexExecPolicyConfig, field string) error {
	switch policy.Sandbox {
	case "", CodexSandboxReadOnly, CodexSandboxWorkspaceWrite, CodexSandboxDangerFullAccess:
	default:
		return fmt.Errorf("%s.sandbox %q is unsupported", field, policy.Sandbox)
	}
	switch policy.AskForApproval {
	case "", CodexApprovalUntrusted, CodexApprovalOnRequest, CodexApprovalNever:
	default:
		return fmt.Errorf("%s.ask_for_approval %q is unsupported", field, policy.AskForApproval)
	}
	return nil
}

func mergeBotPermissions(base BotPermissionsConfig, override *BotPermissionsConfig) BotPermissionsConfig {
	merged := normalizeBotPermissions(base)
	if override == nil {
		return merged
	}
	if override.RuntimeMessage != nil {
		merged.RuntimeMessage = boolPtr(*override.RuntimeMessage)
	}
	if override.RuntimeAutomation != nil {
		merged.RuntimeAutomation = boolPtr(*override.RuntimeAutomation)
	}
	if override.RuntimeCampaigns != nil {
		merged.RuntimeCampaigns = boolPtr(*override.RuntimeCampaigns)
	}
	if len(override.AllowedSkills) > 0 {
		merged.AllowedSkills = normalizeStringSlice(override.AllowedSkills)
	}
	return normalizeBotPermissions(merged)
}
