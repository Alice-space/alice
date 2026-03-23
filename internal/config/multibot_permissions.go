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
	in.Codex.Chat = normalizeCodexExecPolicy(in.Codex.Chat)
	in.Codex.Work = normalizeCodexExecPolicy(in.Codex.Work)
	if in.Codex.Chat.Sandbox == "" {
		in.Codex.Chat.Sandbox = CodexSandboxWorkspaceWrite
	}
	if in.Codex.Chat.AskForApproval == "" {
		in.Codex.Chat.AskForApproval = CodexApprovalNever
	}
	if in.Codex.Work.Sandbox == "" {
		in.Codex.Work.Sandbox = CodexSandboxDangerFullAccess
	}
	if in.Codex.Work.AskForApproval == "" {
		in.Codex.Work.AskForApproval = CodexApprovalNever
	}
	return in
}

func normalizeCodexExecPolicy(in CodexExecPolicyConfig) CodexExecPolicyConfig {
	in.Sandbox = normalizeLLMProvider(in.Sandbox)
	in.AskForApproval = normalizeLLMProvider(in.AskForApproval)
	in.AddDirs = normalizePathSlice(in.AddDirs)
	return in
}

func validateBotPermissions(cfg BotPermissionsConfig) error {
	if err := validateCodexExecPolicy(cfg.Codex.Chat, "permissions.codex.chat"); err != nil {
		return err
	}
	if err := validateCodexExecPolicy(cfg.Codex.Work, "permissions.codex.work"); err != nil {
		return err
	}
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
	if override.Codex.Chat.Sandbox != "" {
		merged.Codex.Chat.Sandbox = override.Codex.Chat.Sandbox
	}
	if override.Codex.Chat.AskForApproval != "" {
		merged.Codex.Chat.AskForApproval = override.Codex.Chat.AskForApproval
	}
	if len(override.Codex.Chat.AddDirs) > 0 {
		merged.Codex.Chat.AddDirs = normalizePathSlice(override.Codex.Chat.AddDirs)
	}
	if override.Codex.Work.Sandbox != "" {
		merged.Codex.Work.Sandbox = override.Codex.Work.Sandbox
	}
	if override.Codex.Work.AskForApproval != "" {
		merged.Codex.Work.AskForApproval = override.Codex.Work.AskForApproval
	}
	if len(override.Codex.Work.AddDirs) > 0 {
		merged.Codex.Work.AddDirs = normalizePathSlice(override.Codex.Work.AddDirs)
	}
	return normalizeBotPermissions(merged)
}
