package connector

import (
	"context"
	"strings"

	"github.com/Alice-space/alice/internal/logging"
)

const helpCommandName = "/help"

func (p *Processor) processBuiltinCommand(ctx context.Context, job Job) (bool, JobProcessState) {
	if isHelpCommand(job.Text) {
		return true, p.processHelpCommand(ctx, job)
	}

	cmd, ok := parseCodeArmyCommand(job.Text)
	if !ok {
		return false, JobProcessCompleted
	}
	if cmd.action != "status" {
		return false, JobProcessCompleted
	}
	return true, p.processCodeArmyStatusCommand(ctx, job, cmd.stateKey)
}

func isBuiltinCommandText(text string) bool {
	if isHelpCommand(text) {
		return true
	}
	_, ok := parseCodeArmyCommand(text)
	return ok
}

func isHelpCommand(text string) bool {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(fields[0]), helpCommandName)
}

func (p *Processor) processHelpCommand(ctx context.Context, job Job) JobProcessState {
	reply := buildBuiltinHelpMarkdown()
	if err := p.replies.respond(ctx, job, reply); err != nil {
		logging.Errorf("send builtin help reply failed event_id=%s: %v", job.EventID, err)
	}
	return JobProcessCompleted
}

func buildBuiltinHelpMarkdown() string {
	return strings.Join([]string{
		"## Alice 内建命令",
		"",
		"- `/help`",
		"  显示当前可用的所有内建命令。",
		"- `/codearmy status [state_key]`",
		"  查看当前会话的 `code_army` 任务和工作流状态；可选按 `state_key` 过滤。",
	}, "\n")
}
