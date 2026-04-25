package config

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/spf13/viper"
)

func validatePureMultiBotRootConfig(v *viper.Viper) error {
	if v == nil {
		return nil
	}
	legacyKeys := []string{
		"bot_name",
		"feishu_app_id",
		"feishu_app_secret",
		"feishu_base_url",
		"feishu_bot_open_id",
		"feishu_bot_user_id",
		"trigger_mode",
		"trigger_prefix",
		"immediate_feedback_mode",
		"immediate_feedback_reaction",
		"llm_provider",
		"llm_profiles",
		"group_scenes",
		"codex_command",
		"codex_timeout_secs",
		"codex_model",
		"codex_model_reasoning_effort",
		"codex_prompt_prefix",
		"claude_command",
		"claude_timeout_secs",
		"claude_prompt_prefix",
		"gemini_command",
		"gemini_timeout_secs",
		"gemini_prompt_prefix",
		"kimi_command",
		"kimi_timeout_secs",
		"kimi_prompt_prefix",
		"runtime_http_addr",
		"runtime_http_token",
		"failure_message",
		"thinking_message",
		"alice_home",
		"workspace_dir",
		"prompt_dir",
		"codex_home",
		"soul_path",
		"env",
		"permissions",
		"queue_capacity",
		"worker_concurrency",
		"automation_task_timeout_secs",
		"auth_status_timeout_secs",
		"runtime_api_shutdown_timeout_secs",
		"local_runtime_store_open_timeout_secs",
		"codex_idle_timeout_secs",
		"codex_high_idle_timeout_secs",
		"codex_xhigh_idle_timeout_secs",
	}
	setKeys := make([]string, 0, len(legacyKeys))
	for _, key := range legacyKeys {
		if v.InConfig(key) {
			setKeys = append(setKeys, key)
		}
	}
	if len(setKeys) == 0 {
		return nil
	}
	sort.Strings(setKeys)
	return fmt.Errorf(
		"root bot keys are no longer supported: %s; move them under bots.<id>",
		strings.Join(setKeys, ", "),
	)
}

func isSupportedLLMProvider(provider string) bool {
	switch normalizeLLMProvider(provider) {
	case "", DefaultLLMProvider, LLMProviderClaude, LLMProviderGemini, LLMProviderKimi, LLMProviderOpenCode:
		return true
	default:
		return false
	}
}

func collectResolvedSceneProfileProviders(cfg Config) []string {
	names := make([]string, 0, 2)
	if cfg.GroupScenes.Chat.Enabled {
		names = append(names, strings.TrimSpace(cfg.GroupScenes.Chat.LLMProfile))
	}
	if cfg.GroupScenes.Work.Enabled {
		names = append(names, strings.TrimSpace(cfg.GroupScenes.Work.LLMProfile))
	}
	if len(names) == 0 {
		for name := range cfg.LLMProfiles {
			names = append(names, name)
		}
		sort.Strings(names)
	}

	providers := make([]string, 0, len(names))
	seenProviders := map[string]struct{}{}
	for _, name := range names {
		if name == "" {
			continue
		}
		profile, ok := cfg.LLMProfiles[name]
		if !ok {
			continue
		}
		provider := normalizeLLMProvider(profile.Provider)
		if provider == "" {
			continue
		}
		if _, exists := seenProviders[provider]; exists {
			continue
		}
		seenProviders[provider] = struct{}{}
		providers = append(providers, provider)
	}
	sort.Strings(providers)
	return providers
}

func resolveLLMProvider(cfg Config) (string, error) {
	explicit := normalizeLLMProvider(cfg.LLMProvider)
	if explicit != "" && !isSupportedLLMProvider(explicit) {
		return "", fmt.Errorf("unsupported llm_provider %q", explicit)
	}

	for name, profile := range cfg.LLMProfiles {
		if !isSupportedLLMProvider(profile.Provider) {
			return "", fmt.Errorf("llm_profiles.%s.provider %q is unsupported", name, profile.Provider)
		}
	}

	if explicit != "" {
		return explicit, nil
	}
	providers := collectResolvedSceneProfileProviders(cfg)
	if len(providers) == 1 {
		return providers[0], nil
	}
	return DefaultLLMProvider, nil
}

func (cfg Config) ResolvedLLMProviders() []string {
	defaultProvider := normalizeLLMProvider(cfg.LLMProvider)
	if defaultProvider == "" {
		defaultProvider = DefaultLLMProvider
	}
	set := map[string]struct{}{
		defaultProvider: {},
	}
	for _, profile := range cfg.LLMProfiles {
		provider := normalizeLLMProvider(profile.Provider)
		if provider == "" {
			continue
		}
		set[provider] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for provider := range set {
		out = append(out, provider)
	}
	sort.Strings(out)
	return out
}

type baseConfigValidation struct {
	QueueCapacity                    int `validate:"gt=0"`
	WorkerConcurrency                int `validate:"gt=0"`
	AutomationTaskTimeoutSecs        int `validate:"gt=0"`
	AuthStatusTimeoutSecs            int `validate:"gt=0"`
	RuntimeAPIShutdownTimeoutSecs    int `validate:"gt=0"`
	LocalRuntimeStoreOpenTimeoutSecs int `validate:"gt=0"`
	CodexIdleTimeoutSecs             int `validate:"gt=0"`
	CodexHighIdleTimeoutSecs         int `validate:"gt=0"`
	CodexXHighIdleTimeoutSecs        int `validate:"gt=0"`
}

func validateBaseConfig(cfg Config, requireCredentials bool) error {
	base := baseConfigValidation{
		QueueCapacity:                    cfg.QueueCapacity,
		WorkerConcurrency:                cfg.WorkerConcurrency,
		AutomationTaskTimeoutSecs:        cfg.AutomationTaskTimeoutSecs,
		AuthStatusTimeoutSecs:            cfg.AuthStatusTimeoutSecs,
		RuntimeAPIShutdownTimeoutSecs:    cfg.RuntimeAPIShutdownTimeoutSecs,
		LocalRuntimeStoreOpenTimeoutSecs: cfg.LocalRuntimeStoreOpenTimeoutSecs,
		CodexIdleTimeoutSecs:             cfg.CodexIdleTimeoutSecs,
		CodexHighIdleTimeoutSecs:         cfg.CodexHighIdleTimeoutSecs,
		CodexXHighIdleTimeoutSecs:        cfg.CodexXHighIdleTimeoutSecs,
	}
	if requireCredentials {
		if strings.TrimSpace(cfg.FeishuAppID) == "" {
			return errors.New("feishu_app_id is required")
		}
		if strings.TrimSpace(cfg.FeishuAppSecret) == "" {
			return errors.New("feishu_app_secret is required")
		}
	}
	if err := configValidator.Struct(base); err != nil {
		var validationErrs validator.ValidationErrors
		if !errors.As(err, &validationErrs) {
			return fmt.Errorf("validate config failed: %w", err)
		}
		for _, validationErr := range validationErrs {
			switch validationErr.Field() {
			case "QueueCapacity":
				return errors.New("queue_capacity must be > 0")
			case "WorkerConcurrency":
				return errors.New("worker_concurrency must be > 0")
			case "AutomationTaskTimeoutSecs":
				return errors.New("automation_task_timeout_secs must be > 0")
			case "AuthStatusTimeoutSecs":
				return errors.New("auth_status_timeout_secs must be > 0")
			case "RuntimeAPIShutdownTimeoutSecs":
				return errors.New("runtime_api_shutdown_timeout_secs must be > 0")
			case "LocalRuntimeStoreOpenTimeoutSecs":
				return errors.New("local_runtime_store_open_timeout_secs must be > 0")
			case "CodexIdleTimeoutSecs":
				return errors.New("codex_idle_timeout_secs must be > 0")
			case "CodexHighIdleTimeoutSecs":
				return errors.New("codex_high_idle_timeout_secs must be > 0")
			case "CodexXHighIdleTimeoutSecs":
				return errors.New("codex_xhigh_idle_timeout_secs must be > 0")
			}
		}
		return err
	}
	for name, profile := range cfg.LLMProfiles {
		switch profile.Provider {
		case "", DefaultLLMProvider, LLMProviderClaude, LLMProviderGemini, LLMProviderKimi, LLMProviderOpenCode:
		default:
			return fmt.Errorf("llm_profiles.%s.provider %q is unsupported", name, profile.Provider)
		}
		if profile.Permissions != nil {
			if err := validateCodexExecPolicy(*profile.Permissions, fmt.Sprintf("llm_profiles.%s.permissions", name)); err != nil {
				return err
			}
		}
	}
	if cfg.GroupScenes.Chat.Enabled {
		if cfg.GroupScenes.Chat.LLMProfile == "" {
			return errors.New("group_scenes.chat.llm_profile is required when chat scene is enabled")
		}
		if _, ok := cfg.LLMProfiles[cfg.GroupScenes.Chat.LLMProfile]; !ok {
			return fmt.Errorf("group_scenes.chat.llm_profile %q is undefined", cfg.GroupScenes.Chat.LLMProfile)
		}
		if cfg.GroupScenes.Chat.SessionScope != GroupSceneSessionPerChat {
			return fmt.Errorf("group_scenes.chat.session_scope must be %q", GroupSceneSessionPerChat)
		}
	}
	if cfg.GroupScenes.Work.Enabled {
		if cfg.GroupScenes.Work.LLMProfile == "" {
			return errors.New("group_scenes.work.llm_profile is required when work scene is enabled")
		}
		if cfg.GroupScenes.Work.TriggerTag == "" {
			return errors.New("group_scenes.work.trigger_tag is required when work scene is enabled")
		}
		if _, ok := cfg.LLMProfiles[cfg.GroupScenes.Work.LLMProfile]; !ok {
			return fmt.Errorf("group_scenes.work.llm_profile %q is undefined", cfg.GroupScenes.Work.LLMProfile)
		}
		if cfg.GroupScenes.Work.SessionScope != GroupSceneSessionPerThread {
			return fmt.Errorf("group_scenes.work.session_scope must be %q", GroupSceneSessionPerThread)
		}
	}
	return nil
}

func validateSceneConfig(cfg Config) error {
	for name, profile := range cfg.LLMProfiles {
		switch profile.Provider {
		case "", DefaultLLMProvider, LLMProviderClaude, LLMProviderGemini, LLMProviderKimi, LLMProviderOpenCode:
		default:
			return fmt.Errorf("llm_profiles.%s.provider %q is unsupported", name, profile.Provider)
		}
		if profile.Permissions != nil {
			if err := validateCodexExecPolicy(*profile.Permissions, fmt.Sprintf("llm_profiles.%s.permissions", name)); err != nil {
				return err
			}
		}
	}
	if cfg.GroupScenes.Chat.Enabled {
		if cfg.GroupScenes.Chat.LLMProfile == "" {
			return errors.New("group_scenes.chat.llm_profile is required when chat scene is enabled")
		}
		if _, ok := cfg.LLMProfiles[cfg.GroupScenes.Chat.LLMProfile]; !ok {
			return fmt.Errorf("group_scenes.chat.llm_profile %q is undefined", cfg.GroupScenes.Chat.LLMProfile)
		}
		if cfg.GroupScenes.Chat.SessionScope != GroupSceneSessionPerChat {
			return fmt.Errorf("group_scenes.chat.session_scope must be %q", GroupSceneSessionPerChat)
		}
	}
	if cfg.GroupScenes.Work.Enabled {
		if cfg.GroupScenes.Work.LLMProfile == "" {
			return errors.New("group_scenes.work.llm_profile is required when work scene is enabled")
		}
		if cfg.GroupScenes.Work.TriggerTag == "" {
			return errors.New("group_scenes.work.trigger_tag is required when work scene is enabled")
		}
		if _, ok := cfg.LLMProfiles[cfg.GroupScenes.Work.LLMProfile]; !ok {
			return fmt.Errorf("group_scenes.work.llm_profile %q is undefined", cfg.GroupScenes.Work.LLMProfile)
		}
		if cfg.GroupScenes.Work.SessionScope != GroupSceneSessionPerThread {
			return fmt.Errorf("group_scenes.work.session_scope must be %q", GroupSceneSessionPerThread)
		}
	}
	return nil
}
