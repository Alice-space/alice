package connector

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/campaign"
	"github.com/Alice-space/alice/internal/config"
	"github.com/Alice-space/alice/internal/logging"
	"github.com/Alice-space/alice/internal/statusview"
)

const helpCommandName = "/help"
const statusCommandName = "/status"
const codeArmyCommandName = "/codearmy"
const codeArmyStatusSubcommand = "status"
const clearCommandName = "/clear"
const builtinHelpCardTitle = "Alice 帮助"
const builtinStatusCardTitle = "Alice 当前状态"

func (p *Processor) processBuiltinCommand(ctx context.Context, job Job) (bool, JobProcessState) {
	if isHelpCommand(job.Text) {
		return true, p.processHelpCommand(ctx, job)
	}
	if isStatusCommand(job.Text) || isCodeArmyStatusCommand(job.Text) {
		return true, p.processStatusCommand(ctx, job)
	}
	if isClearCommand(job.Text) {
		return true, p.processClearCommand(ctx, job)
	}
	return false, JobProcessCompleted
}

func isBuiltinCommandText(text string) bool {
	return isHelpCommand(text) || isStatusCommand(text) || isCodeArmyStatusCommand(text) || isClearCommand(text)
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

func isStatusCommand(text string) bool {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(fields[0]), statusCommandName)
}

func isCodeArmyStatusCommand(text string) bool {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) < 2 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(fields[0]), codeArmyCommandName) &&
		strings.EqualFold(strings.TrimSpace(fields[1]), codeArmyStatusSubcommand)
}

func (p *Processor) processHelpCommand(ctx context.Context, job Job) JobProcessState {
	reply := buildBuiltinHelpMarkdown(p.runtimeSnapshot().helpConfig)
	replyJob := job
	replyJob.Scene = jobSceneChat
	replyJob.CreateFeishuThread = false
	if err := p.replies.respondCardWithTitle(ctx, replyJob, builtinHelpCardTitle, reply); err != nil {
		logging.Errorf("send builtin help reply failed event_id=%s: %v", job.EventID, err)
	}
	return JobProcessCompleted
}

func (p *Processor) processStatusCommand(ctx context.Context, job Job) JobProcessState {
	reply := p.buildBuiltinStatusMarkdown(job)
	replyJob := job
	replyJob.Scene = jobSceneChat
	replyJob.CreateFeishuThread = false
	if err := p.replies.respondCardWithTitle(ctx, replyJob, builtinStatusCardTitle, reply); err != nil {
		logging.Errorf("send builtin status reply failed event_id=%s: %v", job.EventID, err)
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
		"- `/status`",
		"  显示当前会话 scope 下的 token 统计、活跃自动化任务，以及非终态的 code-army campaigns。",
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

func (p *Processor) buildBuiltinStatusMarkdown(job Job) string {
	snapshot := p.runtimeSnapshot()
	if snapshot.statusService == nil || !snapshot.statusService.IsAvailable() {
		return "当前还没有挂载 automation / code-army 状态存储，暂时无法执行 `/status`。"
	}

	result := snapshot.statusService.Query(job)

	lines := []string{
		"## Alice 当前状态",
		"",
		fmt.Sprintf("- scope: `%s`", result.ScopeLabel),
		fmt.Sprintf("- 总 token：`%s`", formatBuiltinStatusTokenCount(result.TotalUsage.TotalTokens())),
		fmt.Sprintf(
			"- token 明细：input `%s` | cached `%s` | output `%s` | turns `%s`",
			formatBuiltinStatusTokenCount(result.TotalUsage.InputTokens),
			formatBuiltinStatusTokenCount(result.TotalUsage.CachedInputTokens),
			formatBuiltinStatusTokenCount(result.TotalUsage.OutputTokens),
			formatBuiltinStatusTokenCount(result.TotalUsage.Turns),
		),
		fmt.Sprintf("- 活跃自动化任务：`%d`", len(result.Tasks)),
		fmt.Sprintf("- 活跃 Code Army：`%d`", len(result.Campaigns)),
	}
	if updatedAt := formatBuiltinStatusTime(statusview.NewestUsageUpdate(result.BotUsages)); updatedAt != "" {
		lines = append(lines, fmt.Sprintf("- token 统计更新：`%s`", updatedAt))
	}
	if result.TaskError != nil || result.CampaignError != nil || result.UsageError != nil {
		lines = append(lines, "")
		if result.TaskError != nil {
			lines = append(lines, fmt.Sprintf("- 自动化任务查询失败：`%s`", sanitizeInlineCode(result.TaskError.Error())))
		}
		if result.CampaignError != nil {
			lines = append(lines, fmt.Sprintf("- Code Army 查询失败：`%s`", sanitizeInlineCode(result.CampaignError.Error())))
		}
		if result.UsageError != nil {
			lines = append(lines, fmt.Sprintf("- token 统计查询失败：`%s`", sanitizeInlineCode(result.UsageError.Error())))
		}
		return strings.Join(lines, "\n")
	}

	lines = append(lines, "", "### Bot Token 统计", "")
	if len(result.BotUsages) == 0 {
		lines = append(lines, "- none")
	} else {
		for _, item := range result.BotUsages {
			lines = append(lines, formatBuiltinStatusUsageLine(item))
		}
	}

	lines = append(lines, "", "### 活跃自动化任务", "")
	if len(result.Tasks) == 0 {
		lines = append(lines, "- none")
	} else {
		for _, task := range result.Tasks {
			lines = append(lines, formatBuiltinStatusTaskLine(task))
		}
	}

	lines = append(lines, "", "### 活跃 Code Army", "")
	if len(result.Campaigns) == 0 {
		lines = append(lines, "- none")
	} else {
		for _, item := range result.Campaigns {
			lines = append(lines, formatBuiltinStatusCampaignLine(item))
		}
	}
	return strings.Join(lines, "\n")
}

func isBuiltinStatusActiveTrial(status campaign.TrialStatus) bool {
	switch status {
	case campaign.TrialStatusPlanned, campaign.TrialStatusRunning, campaign.TrialStatusCandidate, campaign.TrialStatusHold:
		return true
	default:
		return false
	}
}

func formatBuiltinStatusTaskLine(task automation.Task) string {
	parts := []string{fmt.Sprintf("- `%s`", sanitizeInlineCode(task.ID))}
	if title := strings.TrimSpace(task.Title); title != "" {
		parts = append(parts, title)
	}
	parts = append(parts, fmt.Sprintf("`%s`", sanitizeInlineCode(formatBuiltinStatusTaskAction(task.Action))))
	if !task.NextRunAt.IsZero() {
		parts = append(parts, fmt.Sprintf("next `%s`", task.NextRunAt.Local().Format("2006-01-02 15:04:05")))
	}
	if stateKey := strings.TrimSpace(task.Action.StateKey); stateKey != "" {
		parts = append(parts, fmt.Sprintf("state_key `%s`", sanitizeInlineCode(stateKey)))
	}
	if task.MaxRuns > 0 {
		parts = append(parts, fmt.Sprintf("runs `%d/%d`", task.RunCount, task.MaxRuns))
	} else if task.RunCount > 0 {
		parts = append(parts, fmt.Sprintf("runs `%d`", task.RunCount))
	}
	if task.Running {
		parts = append(parts, "`running-now`")
	}
	return strings.Join(parts, " | ")
}

func formatBuiltinStatusTaskAction(action automation.Action) string {
	label := strings.TrimSpace(string(action.Type))
	if label == "" {
		label = "unknown"
	}
	if action.Type == automation.ActionTypeRunWorkflow && strings.TrimSpace(action.Workflow) != "" {
		return label + "/" + strings.TrimSpace(action.Workflow)
	}
	return label
}

func formatBuiltinStatusCampaignLine(item campaign.Campaign) string {
	parts := []string{fmt.Sprintf("- `%s`", sanitizeInlineCode(item.ID))}
	if title := strings.TrimSpace(item.Title); title != "" {
		parts = append(parts, title)
	}
	parts = append(parts, fmt.Sprintf("status `%s`", sanitizeInlineCode(string(item.Status))))
	if repo := strings.TrimSpace(item.Repo); repo != "" {
		parts = append(parts, fmt.Sprintf("repo `%s`", sanitizeInlineCode(repo)))
	}
	if issueIID := strings.TrimSpace(item.IssueIID); issueIID != "" {
		parts = append(parts, fmt.Sprintf("issue `#%s`", sanitizeInlineCode(issueIID)))
	}
	if winner := strings.TrimSpace(item.CurrentWinnerTrialID); winner != "" {
		parts = append(parts, fmt.Sprintf("winner `%s`", sanitizeInlineCode(winner)))
	}
	if activeTrials := builtinStatusActiveTrialIDs(item.Trials); len(activeTrials) > 0 {
		parts = append(parts, fmt.Sprintf("active trials `%s`", sanitizeInlineCode(strings.Join(activeTrials, ", "))))
	}
	if updatedAt := formatBuiltinStatusTime(item.UpdatedAt); updatedAt != "" {
		parts = append(parts, fmt.Sprintf("updated `%s`", updatedAt))
	}
	return strings.Join(parts, " | ")
}

func builtinStatusActiveTrialIDs(trials []campaign.Trial) []string {
	if len(trials) == 0 {
		return nil
	}
	active := make([]string, 0, len(trials))
	for _, trial := range trials {
		if !isBuiltinStatusActiveTrial(trial.Status) {
			continue
		}
		if id := strings.TrimSpace(trial.ID); id != "" {
			active = append(active, id)
		}
	}
	return active
}

func formatBuiltinStatusTime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.Local().Format("2006-01-02 15:04:05")
}

func sanitizeInlineCode(value string) string {
	return strings.ReplaceAll(strings.TrimSpace(value), "`", "'")
}
