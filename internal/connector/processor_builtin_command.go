package connector

import (
	"context"
	"fmt"
	"strings"

	"github.com/Alice-space/alice/internal/config"
	"github.com/Alice-space/alice/internal/logging"
)

const helpCommandName = "/help"
const clearCommandName = "/clear"

func (p *Processor) processBuiltinCommand(ctx context.Context, job Job) (bool, JobProcessState) {
	if isHelpCommand(job.Text) {
		return true, p.processHelpCommand(ctx, job)
	}
	if isClearCommand(job.Text) {
		return true, p.processClearCommand(ctx, job)
	}
	return false, JobProcessCompleted
}

func isBuiltinCommandText(text string) bool {
	return isHelpCommand(text) || isClearCommand(text)
}

func isHelpCommand(text string) bool {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(fields[0]), helpCommandName)
}

func isClearCommand(text string) bool {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(fields[0]), clearCommandName)
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

func (p *Processor) processClearCommand(ctx context.Context, job Job) JobProcessState {
	reply := "当前只支持在群聊的 `chat` 模式下使用 `/clear`。"
	helpCfg := p.runtimeSnapshot().helpConfig
	switch {
	case !isGroupChatType(job.ChatType):
		reply = "当前不是群聊会话，`/clear` 仅用于群聊 `chat` 模式。"
	case !helpCfg.chatEnabled:
		reply = "当前群未启用 `chat` 模式，`/clear` 不会切换上下文。"
	default:
		_, _ = p.resetChatSceneSession(job.ReceiveIDType, job.ReceiveID)
		reply = "当前群聊的 `chat` 上下文已经清空。后续普通消息会进入新的 Codex session。"
	}

	replyJob := job
	replyJob.Scene = jobSceneChat
	replyJob.CreateFeishuThread = false
	if err := p.replies.respond(ctx, replyJob, reply); err != nil {
		logging.Errorf("send builtin clear reply failed event_id=%s: %v", job.EventID, err)
	}
	return JobProcessCompleted
}

func buildBuiltinHelpMarkdown(helpCfg builtinHelpConfig) string {
	lines := []string{
		"## Alice 内建命令",
		"",
		"- `/help`",
		"  显示内建命令，以及普通模式 / 工作模式的当前说明。",
		"- `/clear`",
		"  仅在群聊 `chat` 模式下可用；切换到新的群聊会话，相当于清空当前上下文。",
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
			"  当前配置：整个群默认共享一个会话；发送 `/clear` 后会切到新的会话。模型在不需要发言时可以保持静默。",
		)
	}
	if helpCfg.workEnabled {
		lines = append(lines,
			"- `工作模式`",
			"  任务协作模式，适合排查问题、改代码，以及直接给出结论 / 计划 / 风险 / 下一步。",
			fmt.Sprintf("  当前配置：群根消息需要同时满足 %s 才会进入工作模式；进入后，同一 thread 里继续满足触发条件的新消息会沿用工作上下文。", formatWorkModeTrigger(helpCfg)),
		)
	}
	return strings.Join(lines, "\n")
}

func formatWorkModeTrigger(helpCfg builtinHelpConfig) string {
	trigger := "`@机器人`"
	if strings.EqualFold(strings.TrimSpace(helpCfg.workTriggerMode), config.TriggerModePrefix) {
		prefix := strings.TrimSpace(helpCfg.workTriggerPrefix)
		if prefix == "" {
			trigger = "`前缀`"
		} else {
			trigger = "`" + prefix + "` 前缀"
		}
	}

	tag := strings.TrimSpace(helpCfg.workTriggerTag)
	if tag == "" {
		return trigger
	}
	return trigger + " + `" + tag + "`"
}
