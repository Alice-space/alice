package bootstrap

import (
	"fmt"
	"sort"
	"time"

	"github.com/Alice-space/alice/internal/config"
	"github.com/Alice-space/alice/internal/llm"
	"github.com/Alice-space/alice/internal/logging"
	"github.com/Alice-space/alice/internal/prompting"
)

type ConfigReloadReport struct {
	AppliedFields         []string
	RestartRequiredFields []string
}

func (r *ConnectorRuntime) ApplyConfigReload(next config.Config) (ConfigReloadReport, error) {
	if r == nil {
		return ConfigReloadReport{}, fmt.Errorf("connector runtime is nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	current := r.Config
	report := ConfigReloadReport{
		RestartRequiredFields: diffRestartRequiredFields(current, next),
	}

	merged := current
	applied := make(map[string]struct{})
	applyReloadableFields(&merged, next, applied)
	if len(applied) == 0 && len(report.RestartRequiredFields) == 0 {
		return report, nil
	}

	llmChanged := llmRuntimeConfigChanged(current, merged)
	promptLoader := r.promptLoaderForReload(merged)

	var backend llm.Backend
	var err error
	if llmChanged {
		backend, err = llm.NewBackend(buildFactoryConfig(merged, promptLoader))
		if err != nil {
			return report, fmt.Errorf("rebuild llm backend failed: %w", err)
		}
	}

	if loggingConfigChanged(current, merged) {
		if err := logging.Configure(logging.Options{
			Level:      merged.LogLevel,
			FilePath:   merged.LogFile,
			MaxSizeMB:  merged.LogMaxSizeMB,
			MaxBackups: merged.LogMaxBackups,
			MaxAgeDays: merged.LogMaxAgeDays,
			Compress:   merged.LogCompress,
		}); err != nil {
			return report, fmt.Errorf("reconfigure logging failed: %w", err)
		}
	}

	if r.App != nil {
		r.App.UpdateRuntimeConfig(merged)
	}
	if r.Processor != nil {
		if llmChanged && backend != nil {
			r.Processor.SetLLMBackend(backend)
		}
		r.Processor.SetReplyMessages(merged.FailureMessage, merged.ThinkingMessage)
		r.Processor.SetImmediateFeedback(merged.ImmediateFeedbackMode, merged.ImmediateFeedbackReaction)
	}
	if r.AutomationEngine != nil {
		if llmChanged && backend != nil {
			r.AutomationEngine.SetLLMRunner(backend)
		}
		r.AutomationEngine.SetUserTaskTimeout(merged.AutomationTaskTimeout)
	}

	r.Config = merged
	report.AppliedFields = fieldSetToSortedSlice(applied)
	return report, nil
}

func (r *ConnectorRuntime) promptLoaderForReload(cfg config.Config) *prompting.Loader {
	if r != nil && r.PromptLoader != nil {
		return r.PromptLoader
	}
	promptDir := ResolvePromptDir(cfg.WorkspaceDir, cfg.PromptDir)
	return prompting.NewLoader(promptDir)
}

func diffRestartRequiredFields(current, next config.Config) []string {
	changed := make([]string, 0, 10)
	if current.FeishuAppID != next.FeishuAppID {
		changed = append(changed, "feishu_app_id")
	}
	if current.FeishuAppSecret != next.FeishuAppSecret {
		changed = append(changed, "feishu_app_secret")
	}
	if current.FeishuBaseURL != next.FeishuBaseURL {
		changed = append(changed, "feishu_base_url")
	}
	if current.RuntimeHTTPAddr != next.RuntimeHTTPAddr {
		changed = append(changed, "runtime_http_addr")
	}
	if current.RuntimeHTTPToken != next.RuntimeHTTPToken {
		changed = append(changed, "runtime_http_token")
	}
	if current.WorkspaceDir != next.WorkspaceDir {
		changed = append(changed, "workspace_dir")
	}
	if current.MemoryDir != next.MemoryDir {
		changed = append(changed, "memory_dir")
	}
	if current.PromptDir != next.PromptDir {
		changed = append(changed, "prompt_dir")
	}
	if current.QueueCapacity != next.QueueCapacity {
		changed = append(changed, "queue_capacity")
	}
	if current.WorkerConcurrency != next.WorkerConcurrency {
		changed = append(changed, "worker_concurrency")
	}
	sort.Strings(changed)
	return changed
}

func applyReloadableFields(dst *config.Config, src config.Config, changed map[string]struct{}) {
	if dst == nil {
		return
	}
	applyStringField(&dst.TriggerMode, src.TriggerMode, "trigger_mode", changed)
	applyStringField(&dst.TriggerPrefix, src.TriggerPrefix, "trigger_prefix", changed)
	applyStringField(&dst.FeishuBotOpenID, src.FeishuBotOpenID, "feishu_bot_open_id", changed)
	applyStringField(&dst.FeishuBotUserID, src.FeishuBotUserID, "feishu_bot_user_id", changed)
	applyStringField(&dst.FailureMessage, src.FailureMessage, "failure_message", changed)
	applyStringField(&dst.ThinkingMessage, src.ThinkingMessage, "thinking_message", changed)
	applyStringField(&dst.ImmediateFeedbackMode, src.ImmediateFeedbackMode, "immediate_feedback_mode", changed)
	applyStringField(&dst.ImmediateFeedbackReaction, src.ImmediateFeedbackReaction, "immediate_feedback_reaction", changed)

	applyStringField(&dst.LLMProvider, src.LLMProvider, "llm_provider", changed)
	if !llmProfileMapEqual(dst.LLMProfiles, src.LLMProfiles) {
		dst.LLMProfiles = cloneLLMProfileMap(src.LLMProfiles)
		changed["llm_profiles"] = struct{}{}
	}
	if dst.GroupScenes != src.GroupScenes {
		dst.GroupScenes = src.GroupScenes
		changed["group_scenes"] = struct{}{}
	}
	applyStringField(&dst.CodexCommand, src.CodexCommand, "codex_command", changed)
	applyIntField(&dst.CodexTimeoutSecs, src.CodexTimeoutSecs, "codex_timeout_secs", changed)
	applyDurationField(&dst.CodexTimeout, src.CodexTimeout, "codex_timeout", changed)
	applyStringField(&dst.CodexModel, src.CodexModel, "codex_model", changed)
	applyStringField(&dst.CodexReasoningEffort, src.CodexReasoningEffort, "codex_model_reasoning_effort", changed)
	if !stringMapEqual(dst.CodexEnv, src.CodexEnv) {
		dst.CodexEnv = cloneStringMap(src.CodexEnv)
		changed["env"] = struct{}{}
	}
	applyStringField(&dst.CodexPromptPrefix, src.CodexPromptPrefix, "codex_prompt_prefix", changed)

	applyStringField(&dst.ClaudeCommand, src.ClaudeCommand, "claude_command", changed)
	applyIntField(&dst.ClaudeTimeoutSecs, src.ClaudeTimeoutSecs, "claude_timeout_secs", changed)
	applyDurationField(&dst.ClaudeTimeout, src.ClaudeTimeout, "claude_timeout", changed)
	applyStringField(&dst.ClaudePromptPrefix, src.ClaudePromptPrefix, "claude_prompt_prefix", changed)

	applyStringField(&dst.KimiCommand, src.KimiCommand, "kimi_command", changed)
	applyIntField(&dst.KimiTimeoutSecs, src.KimiTimeoutSecs, "kimi_timeout_secs", changed)
	applyDurationField(&dst.KimiTimeout, src.KimiTimeout, "kimi_timeout", changed)
	applyStringField(&dst.KimiPromptPrefix, src.KimiPromptPrefix, "kimi_prompt_prefix", changed)

	applyIntField(&dst.AutomationTaskTimeoutSecs, src.AutomationTaskTimeoutSecs, "automation_task_timeout_secs", changed)
	applyDurationField(&dst.AutomationTaskTimeout, src.AutomationTaskTimeout, "automation_task_timeout", changed)
	applyIntField(&dst.IdleSummaryHours, src.IdleSummaryHours, "idle_summary_hours", changed)
	applyDurationField(&dst.IdleSummaryIdle, src.IdleSummaryIdle, "idle_summary_idle", changed)

	applyStringField(&dst.LogLevel, src.LogLevel, "log_level", changed)
	applyStringField(&dst.LogFile, src.LogFile, "log_file", changed)
	applyIntField(&dst.LogMaxSizeMB, src.LogMaxSizeMB, "log_max_size_mb", changed)
	applyIntField(&dst.LogMaxBackups, src.LogMaxBackups, "log_max_backups", changed)
	applyIntField(&dst.LogMaxAgeDays, src.LogMaxAgeDays, "log_max_age_days", changed)
	applyBoolField(&dst.LogCompress, src.LogCompress, "log_compress", changed)
}

func llmRuntimeConfigChanged(current, next config.Config) bool {
	return current.LLMProvider != next.LLMProvider ||
		!llmProfileMapEqual(current.LLMProfiles, next.LLMProfiles) ||
		current.GroupScenes != next.GroupScenes ||
		current.CodexCommand != next.CodexCommand ||
		current.CodexTimeout != next.CodexTimeout ||
		current.CodexModel != next.CodexModel ||
		current.CodexReasoningEffort != next.CodexReasoningEffort ||
		!stringMapEqual(current.CodexEnv, next.CodexEnv) ||
		current.CodexPromptPrefix != next.CodexPromptPrefix ||
		current.ClaudeCommand != next.ClaudeCommand ||
		current.ClaudeTimeout != next.ClaudeTimeout ||
		current.ClaudePromptPrefix != next.ClaudePromptPrefix ||
		current.KimiCommand != next.KimiCommand ||
		current.KimiTimeout != next.KimiTimeout ||
		current.KimiPromptPrefix != next.KimiPromptPrefix
}

func loggingConfigChanged(current, next config.Config) bool {
	return current.LogLevel != next.LogLevel ||
		current.LogFile != next.LogFile ||
		current.LogMaxSizeMB != next.LogMaxSizeMB ||
		current.LogMaxBackups != next.LogMaxBackups ||
		current.LogMaxAgeDays != next.LogMaxAgeDays ||
		current.LogCompress != next.LogCompress
}

func fieldSetToSortedSlice(fields map[string]struct{}) []string {
	out := make([]string, 0, len(fields))
	for field := range fields {
		out = append(out, field)
	}
	sort.Strings(out)
	return out
}

func applyStringField(dst *string, src string, field string, changed map[string]struct{}) {
	if dst == nil || *dst == src {
		return
	}
	*dst = src
	changed[field] = struct{}{}
}

func applyIntField(dst *int, src int, field string, changed map[string]struct{}) {
	if dst == nil || *dst == src {
		return
	}
	*dst = src
	changed[field] = struct{}{}
}

func applyDurationField(dst *time.Duration, src time.Duration, field string, changed map[string]struct{}) {
	if dst == nil || *dst == src {
		return
	}
	*dst = src
	changed[field] = struct{}{}
}

func applyBoolField(dst *bool, src bool, field string, changed map[string]struct{}) {
	if dst == nil || *dst == src {
		return
	}
	*dst = src
	changed[field] = struct{}{}
}

func stringMapEqual(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func llmProfileMapEqual(left, right map[string]config.LLMProfileConfig) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if rightValue, ok := right[key]; !ok || rightValue != value {
			return false
		}
	}
	return true
}

func cloneLLMProfileMap(in map[string]config.LLMProfileConfig) map[string]config.LLMProfileConfig {
	if len(in) == 0 {
		return map[string]config.LLMProfileConfig{}
	}
	out := make(map[string]config.LLMProfileConfig, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
