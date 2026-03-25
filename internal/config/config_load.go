package config

import (
	"fmt"

	"github.com/spf13/viper"
)

func LoadFromFile(path string) (Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")
	setRootDefaults(v)

	if err := v.ReadInConfig(); err != nil {
		return Config{}, fmt.Errorf("read config file %q failed: %w", path, err)
	}
	if err := validatePureMultiBotRootConfig(v); err != nil {
		return Config{}, err
	}
	if err := rejectRemovedImageProxyConfig(v); err != nil {
		return Config{}, err
	}
	setBotDefaults(v)

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse config yaml failed: %w", err)
	}

	cfg = normalizeLoadedConfig(cfg, v.GetStringMapString("env"))
	return finalizeConfig(cfg, false)
}

func setRootDefaults(v *viper.Viper) {
	if v == nil {
		return
	}
	setCommonConfigDefaults(v, "", true)
	v.SetDefault("alice_home", AliceHomeDir())
	v.SetDefault("workspace_dir", "")
	v.SetDefault("prompt_dir", "")
	v.SetDefault("codex_home", "")
	v.SetDefault("soul_path", "")
	v.SetDefault("bot_name", "")
	v.SetDefault("log_level", "info")
	v.SetDefault("log_file", "")
	v.SetDefault("log_max_size_mb", 20)
	v.SetDefault("log_max_backups", 5)
	v.SetDefault("log_max_age_days", 7)
	v.SetDefault("log_compress", false)
}

func setBotDefaults(v *viper.Viper) {
	if v == nil {
		return
	}
	for rawBotID := range v.GetStringMap("bots") {
		setCommonConfigDefaults(v, "bots."+rawBotID+".", false)
	}
}

func setCommonConfigDefaults(v *viper.Viper, prefix string, includeRuntimeHTTPAddr bool) {
	if v == nil {
		return
	}

	v.SetDefault(configKey(prefix, "feishu_base_url"), "https://open.feishu.cn")
	v.SetDefault(configKey(prefix, "trigger_mode"), TriggerModeAt)
	v.SetDefault(configKey(prefix, "trigger_prefix"), "")
	v.SetDefault(configKey(prefix, "immediate_feedback_mode"), DefaultImmediateFeedbackMode)
	v.SetDefault(configKey(prefix, "immediate_feedback_reaction"), DefaultImmediateFeedbackReaction)
	if includeRuntimeHTTPAddr {
		v.SetDefault(configKey(prefix, "runtime_http_addr"), DefaultRuntimeHTTPAddr)
	}
	v.SetDefault(configKey(prefix, "runtime_http_token"), "")
	v.SetDefault(configKey(prefix, "failure_message"), "Codex 暂时不可用，请稍后重试。")
	v.SetDefault(configKey(prefix, "thinking_message"), "正在思考中...")
	v.SetDefault(configKey(prefix, "image_generation.enabled"), false)
	v.SetDefault(configKey(prefix, "image_generation.provider"), "openai")
	v.SetDefault(configKey(prefix, "image_generation.model"), "gpt-image-1.5")
	v.SetDefault(configKey(prefix, "image_generation.base_url"), "")
	v.SetDefault(configKey(prefix, "image_generation.timeout_secs"), 120)
	v.SetDefault(configKey(prefix, "image_generation.moderation"), "")
	v.SetDefault(configKey(prefix, "image_generation.n"), 0)
	v.SetDefault(configKey(prefix, "image_generation.output_compression"), -1)
	v.SetDefault(configKey(prefix, "image_generation.response_format"), "")
	v.SetDefault(configKey(prefix, "image_generation.size"), "1024x1536")
	v.SetDefault(configKey(prefix, "image_generation.quality"), "high")
	v.SetDefault(configKey(prefix, "image_generation.background"), "auto")
	v.SetDefault(configKey(prefix, "image_generation.output_format"), "png")
	v.SetDefault(configKey(prefix, "image_generation.partial_images"), -1)
	v.SetDefault(configKey(prefix, "image_generation.stream"), false)
	v.SetDefault(configKey(prefix, "image_generation.style"), "")
	v.SetDefault(configKey(prefix, "image_generation.input_fidelity"), "high")
	v.SetDefault(configKey(prefix, "image_generation.mask_path"), "")
	v.SetDefault(configKey(prefix, "image_generation.use_current_attachments"), true)
	v.SetDefault(configKey(prefix, "env.HTTPS_PROXY"), DefaultHTTPSProxy)
	v.SetDefault(configKey(prefix, "env.ALL_PROXY"), DefaultALLProxy)
	v.SetDefault(configKey(prefix, "permissions.runtime_message"), true)
	v.SetDefault(configKey(prefix, "permissions.runtime_automation"), true)
	v.SetDefault(configKey(prefix, "permissions.runtime_campaigns"), true)
	v.SetDefault(configKey(prefix, "queue_capacity"), 256)
	v.SetDefault(configKey(prefix, "worker_concurrency"), DefaultWorkerConcurrency)
	v.SetDefault(configKey(prefix, "automation_task_timeout_secs"), 6000)
	v.SetDefault(configKey(prefix, "group_scenes.chat.enabled"), false)
	v.SetDefault(configKey(prefix, "group_scenes.chat.session_scope"), GroupSceneSessionPerChat)
	v.SetDefault(configKey(prefix, "group_scenes.chat.llm_profile"), "")
	v.SetDefault(configKey(prefix, "group_scenes.chat.no_reply_token"), "[[NO_REPLY]]")
	v.SetDefault(configKey(prefix, "group_scenes.chat.create_feishu_thread"), false)
	v.SetDefault(configKey(prefix, "group_scenes.work.enabled"), false)
	v.SetDefault(configKey(prefix, "group_scenes.work.trigger_tag"), "#work")
	v.SetDefault(configKey(prefix, "group_scenes.work.session_scope"), GroupSceneSessionPerThread)
	v.SetDefault(configKey(prefix, "group_scenes.work.llm_profile"), "")
	v.SetDefault(configKey(prefix, "group_scenes.work.no_reply_token"), "")
	v.SetDefault(configKey(prefix, "group_scenes.work.create_feishu_thread"), true)
}

func configKey(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + key
}
