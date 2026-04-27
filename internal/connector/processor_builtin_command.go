package connector

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/config"
	"github.com/Alice-space/alice/internal/logging"
	"github.com/Alice-space/alice/internal/statusview"
)

const helpCommandName = "/help"
const statusCommandName = "/status"
const clearCommandName = "/clear"
const stopCommandName = "/stop"
const cdCommandName = "/cd"
const lsCommandName = "/ls"
const pwdCommandName = "/pwd"
const builtinHelpCardTitle = "Alice 帮助"
const builtinStatusCardTitle = "Alice 当前状态"

func (p *Processor) processBuiltinCommand(ctx context.Context, job Job) (bool, JobProcessState) {
	if isHelpCommand(job.Text) {
		return true, p.processHelpCommand(ctx, job)
	}
	if isStatusCommand(job.Text) {
		return true, p.processStatusCommand(ctx, job)
	}
	if isClearCommand(job.Text) {
		return true, p.processClearCommand(ctx, job)
	}
	if isStopCommand(job.Text) {
		return true, p.processStopCommand(ctx, job)
	}
	if isCdCommand(job.Text) {
		return true, p.processCdCommand(ctx, job)
	}
	if isLsCommand(job.Text) {
		return true, p.processLsCommand(ctx, job)
	}
	if isPwdCommand(job.Text) {
		return true, p.processPwdCommand(ctx, job)
	}
	return false, JobProcessCompleted
}

func isBuiltinCommandText(text string) bool {
	return isHelpCommand(text) || isStatusCommand(text) || isClearCommand(text) ||
		isStopCommand(text) || isCdCommand(text) || isLsCommand(text) || isPwdCommand(text)
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

func isStopCommand(text string) bool {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(fields[0]), stopCommandName)
}

func isCdCommand(text string) bool {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(fields[0]), cdCommandName)
}

func isLsCommand(text string) bool {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(fields[0]), lsCommandName)
}

func isPwdCommand(text string) bool {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(fields[0]), pwdCommandName)
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

func (p *Processor) processStopCommand(ctx context.Context, job Job) JobProcessState {
	reply := "已请求停止当前 session 的回复。若当前有正在运行的 Codex 进程，它会被打断；现有 Codex session 会保留，你继续在当前 thread 或会话里发送新指令时会在原 session 上继续。"

	replyJob := job
	replyJob.Scene = jobSceneChat
	replyJob.CreateFeishuThread = false
	if err := p.replies.respond(ctx, replyJob, reply); err != nil {
		logging.Errorf("send builtin stop reply failed event_id=%s: %v", job.EventID, err)
	}
	return JobProcessCompleted
}

func (p *Processor) processCdCommand(ctx context.Context, job Job) JobProcessState {
	fields := strings.Fields(job.Text)
	if len(fields) < 2 {
		reply := "用法：`/cd <path>` 切换工作目录。当前工作目录：\n" + p.getWorkDirText(job)
		replyJob := job
		replyJob.Scene = jobSceneChat
		replyJob.CreateFeishuThread = false
		if err := p.replies.respond(ctx, replyJob, reply); err != nil {
			logging.Errorf("send builtin cd reply failed event_id=%s: %v", job.EventID, err)
		}
		return JobProcessCompleted
	}

	targetPath := strings.TrimSpace(strings.Join(fields[1:], " "))
	workDir := p.resolveWorkDir(job)

	resolvedPath := targetPath
	if !filepath.IsAbs(targetPath) {
		resolvedPath = filepath.Join(workDir, targetPath)
	}
	resolvedPath = filepath.Clean(resolvedPath)

	info, err := os.Stat(resolvedPath)
	if err != nil {
		reply := fmt.Sprintf("路径不存在或无法访问：`%s`", sanitizeInlineCode(resolvedPath))
		replyJob := job
		replyJob.Scene = jobSceneChat
		replyJob.CreateFeishuThread = false
		if err := p.replies.respond(ctx, replyJob, reply); err != nil {
			logging.Errorf("send builtin cd reply failed event_id=%s: %v", job.EventID, err)
		}
		return JobProcessCompleted
	}
	if !info.IsDir() {
		reply := fmt.Sprintf("路径不是目录：`%s`", sanitizeInlineCode(resolvedPath))
		replyJob := job
		replyJob.Scene = jobSceneChat
		replyJob.CreateFeishuThread = false
		if err := p.replies.respond(ctx, replyJob, reply); err != nil {
			logging.Errorf("send builtin cd reply failed event_id=%s: %v", job.EventID, err)
		}
		return JobProcessCompleted
	}

	sessionKey := sessionKeyForJob(job)
	p.setSessionWorkDir(sessionKey, resolvedPath)

	reply := fmt.Sprintf("已切换到：`%s`", sanitizeInlineCode(resolvedPath))
	replyJob := job
	replyJob.Scene = jobSceneChat
	replyJob.CreateFeishuThread = false
	if err := p.replies.respond(ctx, replyJob, reply); err != nil {
		logging.Errorf("send builtin cd reply failed event_id=%s: %v", job.EventID, err)
	}
	return JobProcessCompleted
}

func (p *Processor) processLsCommand(ctx context.Context, job Job) JobProcessState {
	fields := strings.Fields(job.Text)
	workDir := p.resolveWorkDir(job)

	targetDir := workDir
	if len(fields) >= 2 {
		targetPath := strings.TrimSpace(strings.Join(fields[1:], " "))
		if filepath.IsAbs(targetPath) {
			targetDir = filepath.Clean(targetPath)
		} else {
			targetDir = filepath.Join(workDir, targetPath)
		}
	}
	targetDir = filepath.Clean(targetDir)

	entries, err := os.ReadDir(targetDir)
	if err != nil {
		reply := fmt.Sprintf("读取目录失败：`%s`\n%v", sanitizeInlineCode(targetDir), err)
		replyJob := job
		replyJob.Scene = jobSceneChat
		replyJob.CreateFeishuThread = false
		if err := p.replies.respond(ctx, replyJob, reply); err != nil {
			logging.Errorf("send builtin ls reply failed event_id=%s: %v", job.EventID, err)
		}
		return JobProcessCompleted
	}

	lines := []string{fmt.Sprintf("### `%s`", sanitizeInlineCode(targetDir)), ""}
	if len(entries) == 0 {
		lines = append(lines, "- (empty)")
	} else {
		for _, entry := range entries {
			prefix := ""
			if entry.IsDir() {
				prefix = "[DIR]  "
			}
			lines = append(lines, fmt.Sprintf("- %s%s", prefix, sanitizeInlineCode(entry.Name())))
		}
	}
	reply := strings.Join(lines, "\n")

	replyJob := job
	replyJob.Scene = jobSceneChat
	replyJob.CreateFeishuThread = false
	if err := p.replies.respond(ctx, replyJob, reply); err != nil {
		logging.Errorf("send builtin ls reply failed event_id=%s: %v", job.EventID, err)
	}
	return JobProcessCompleted
}

func (p *Processor) processPwdCommand(ctx context.Context, job Job) JobProcessState {
	reply := p.getWorkDirText(job)
	replyJob := job
	replyJob.Scene = jobSceneChat
	replyJob.CreateFeishuThread = false
	if err := p.replies.respond(ctx, replyJob, reply); err != nil {
		logging.Errorf("send builtin pwd reply failed event_id=%s: %v", job.EventID, err)
	}
	return JobProcessCompleted
}

func (p *Processor) getWorkDirText(job Job) string {
	workDir := p.resolveWorkDir(job)
	return fmt.Sprintf("当前工作目录：`%s`", sanitizeInlineCode(workDir))
}

func (p *Processor) resolveWorkDir(job Job) string {
	sessionKey := sessionKeyForJob(job)
	if dir := strings.TrimSpace(p.getSessionWorkDir(sessionKey)); dir != "" {
		return dir
	}
	snapshot := p.runtimeSnapshot()
	if snapshot.workspaceDir != "" {
		return snapshot.workspaceDir
	}
	return "."
}

func buildBuiltinHelpMarkdown(helpCfg builtinHelpConfig) string {
	lines := []string{
		"## Alice 内建命令",
		"",
		"- `/help`",
		"  显示内建命令，以及普通模式 / 工作模式的当前说明。",
		"- `/status`",
		"  显示当前会话 scope 下的 token 统计，以及活跃自动化任务。",
		"- `/clear`",
		"  仅在群聊 `chat` 模式下可用；切换到新的群聊会话，相当于清空当前上下文。",
		"- `/stop`",
		"  停止当前 session 正在运行的回复，但保留现有 session；后续新指令会在当前 session 上继续。",
		"- `/cd <path>`",
		"  切换 agent 工作目录，仅在 work 模式下有效；后续 agent 运行将使用该目录。",
		"- `/ls [path]`",
		"  列出工作目录内容。不指定路径时显示当前工作目录，可指定绝对或相对路径。",
		"- `/pwd`",
		"  显示当前 agent 工作目录。",
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
		return "当前还没有挂载 automation 状态存储，暂时无法执行 `/status`。"
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
	}
	if updatedAt := formatBuiltinStatusTime(statusview.NewestUsageUpdate(result.BotUsages)); updatedAt != "" {
		lines = append(lines, fmt.Sprintf("- token 统计更新：`%s`", updatedAt))
	}
	if result.TaskError != nil || result.UsageError != nil {
		lines = append(lines, "")
		if result.TaskError != nil {
			lines = append(lines, fmt.Sprintf("- 自动化任务查询失败：`%s`", sanitizeInlineCode(result.TaskError.Error())))
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
	return strings.Join(lines, "\n")
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
	return label
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
