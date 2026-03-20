package connector

import (
	"context"
	"fmt"
	"strings"

	"github.com/Alice-space/alice/internal/logging"
)

const helpCommandName = "/help"

func (p *Processor) processBuiltinCommand(ctx context.Context, job Job) (bool, JobProcessState) {
	if isHelpCommand(job.Text) {
		return true, p.processHelpCommand(ctx, job)
	}
	return false, JobProcessCompleted
}

func isBuiltinCommandText(text string) bool {
	return isHelpCommand(text)
}

func isHelpCommand(text string) bool {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(fields[0]), helpCommandName)
}

func (p *Processor) processHelpCommand(ctx context.Context, job Job) JobProcessState {
	reply := buildBuiltinHelpMarkdown(p.runtimeSnapshot().helpConfig)
	replyJob := job
	replyJob.Scene = jobSceneChat
	replyJob.CreateFeishuThread = false
	if err := p.replies.respond(ctx, replyJob, reply); err != nil {
		logging.Errorf("send builtin help reply failed event_id=%s: %v", job.EventID, err)
	}
	return JobProcessCompleted
}

func buildBuiltinHelpMarkdown(helpCfg builtinHelpConfig) string {
	lines := []string{
		"## Alice 内建命令",
		"",
		"- `/help`",
		"  显示内建命令，以及普通模式 / 工作模式的当前说明。",
	}

	if !helpCfg.chatEnabled && !helpCfg.workEnabled {
		lines = append(lines,
			"",
			"## 模式说明",
			"",
			"- 当前未启用 `chat/work` 场景路由，群消息会按 legacy 触发策略处理。",
		)
		return strings.Join(lines, "\n")
	}

	lines = append(lines, "", "## 模式说明", "")
	if helpCfg.chatEnabled {
		lines = append(lines,
			"- `普通模式`",
			"  默认群聊模式，适合闲聊、轻量互动和非任务性交流。",
			"  当前配置：整个群共享一个会话，模型在不需要发言时可以保持静默。",
		)
	}
	if helpCfg.workEnabled {
		lines = append(lines,
			"- `工作模式`",
			"  任务协作模式，适合排查问题、改代码，以及直接给出结论 / 计划 / 风险 / 下一步。",
			fmt.Sprintf("  当前配置：群根消息使用 %s 触发，后续同一 thread 会继续沿用工作上下文。", formatWorkModeTrigger(helpCfg)),
		)
	}
	return strings.Join(lines, "\n")
}

func formatWorkModeTrigger(helpCfg builtinHelpConfig) string {
	parts := make([]string, 0, 2)
	if tag := strings.TrimSpace(helpCfg.workTriggerTag); tag != "" {
		parts = append(parts, "`"+tag+"`")
	}
	if helpCfg.workRequireMention {
		parts = append(parts, "`@机器人`")
	}
	if len(parts) == 0 {
		return "工作模式触发条件"
	}
	return strings.Join(parts, " + ")
}
