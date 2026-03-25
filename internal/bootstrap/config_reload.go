package bootstrap

import (
	"fmt"
	"sort"
	"time"

	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/config"
	"github.com/Alice-space/alice/internal/connector"
	"github.com/Alice-space/alice/internal/llm"
	"github.com/Alice-space/alice/internal/logging"
	"github.com/Alice-space/alice/internal/prompting"
	"github.com/Alice-space/alice/internal/runtimecfg"
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
		backend, err = buildLLMBackend(merged, promptLoader)
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
	if r.RuntimeAPI != nil {
		r.RuntimeAPI.UpdateRuntimeConfig(merged)
	}
	if r.Processor != nil {
		if err := r.Processor.UpdateRuntimeConfig(connector.ProcessorRuntimeUpdate{
			Backend:                backend,
			FailureMessage:         merged.FailureMessage,
			ThinkingMessage:        merged.ThinkingMessage,
			ImmediateFeedbackMode:  merged.ImmediateFeedbackMode,
			ImmediateFeedbackEmoji: merged.ImmediateFeedbackReaction,
			ImageGeneration:        merged.ImageGeneration,
			ImageEnv:               merged.CodexEnv,
		}); err != nil {
			return report, fmt.Errorf("reconfigure image generation failed: %w", err)
		}
	}
	if r.AutomationEngine != nil {
		if llmChanged && backend != nil {
			r.AutomationEngine.SetLLMRunner(backend)
			r.AutomationEngine.SetWorkflowRunner(automation.NewPromptWorkflowRunner(backend))
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
	if dst.ImageGeneration != src.ImageGeneration {
		dst.ImageGeneration = src.ImageGeneration
		changed["image_generation"] = struct{}{}
	}

	applyStringField(&dst.LLMProvider, src.LLMProvider, "llm_provider", changed)
	if !llmProfileMapEqual(dst.LLMProfiles, src.LLMProfiles) {
		dst.LLMProfiles = cloneLLMProfileMap(src.LLMProfiles)
		changed["llm_profiles"] = struct{}{}
	}
	if dst.GroupScenes != src.GroupScenes {
		dst.GroupScenes = src.GroupScenes
		changed["group_scenes"] = struct{}{}
	}
	if !stringMapEqual(dst.CodexEnv, src.CodexEnv) {
		dst.CodexEnv = cloneStringMap(src.CodexEnv)
		changed["env"] = struct{}{}
	}

	applyIntField(&dst.AutomationTaskTimeoutSecs, src.AutomationTaskTimeoutSecs, "automation_task_timeout_secs", changed)
	applyDurationField(&dst.AutomationTaskTimeout, src.AutomationTaskTimeout, "automation_task_timeout", changed)

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
		!stringMapEqual(current.CodexEnv, next.CodexEnv)
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
	return runtimecfg.CloneLLMProfiles(in)
}
