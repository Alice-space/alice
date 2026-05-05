package connector

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
const sessionCommandName = "/session"
const cdCommandName = "/cd"
const lsCommandName = "/ls"
const pwdCommandName = "/pwd"
const goalCommandName = "/goal"
const builtinHelpCardTitle = "Alice 帮助"
const builtinStatusCardTitle = "Alice 当前状态"
const builtinWorkThreadCardTitle = "Alice Work Thread"

var backendSessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:@/-]*$`)

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
	if isSessionCommand(job.Text) {
		return true, p.processSessionCommand(ctx, job)
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
	if isGoalCommand(job.Text) {
		return true, p.processGoalCommand(ctx, job)
	}
	return false, JobProcessCompleted
}

func isBuiltinCommandText(text string) bool {
	return isHelpCommand(text) || isStatusCommand(text) || isClearCommand(text) ||
		isStopCommand(text) || isSessionCommand(text) ||
		isCdCommand(text) || isLsCommand(text) || isPwdCommand(text) ||
		isGoalCommand(text)
}

func isContextualBuiltinCommand(text string) bool {
	return isClearCommand(text) || isStopCommand(text) || isSessionCommand(text) ||
		isCdCommand(text) || isLsCommand(text) || isPwdCommand(text)
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

func isSessionCommand(text string) bool {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(fields[0]), sessionCommandName)
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

func isGoalCommand(text string) bool {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(fields[0]), goalCommandName)
}

func (p *Processor) processGoalCommand(ctx context.Context, job Job) JobProcessState {
	reply := p.buildGoalStatusMarkdown(job)
	replyJob := forceDirectReplyJob(job)
	if err := p.replies.respond(ctx, replyJob, reply); err != nil {
		logging.Errorf("send builtin goal reply failed event_id=%s: %v", job.EventID, err)
	}
	return JobProcessCompleted
}

func (p *Processor) buildGoalStatusMarkdown(job Job) string {
	snapshot := p.runtimeSnapshot()
	if snapshot.statusService == nil {
		return "目标状态查询暂不可用。"
	}
	scope := automation.Scope{
		Kind: automation.ScopeKind(strings.ToLower(job.ChatType)),
		ID:   job.ReceiveID,
	}
	if job.ChatType == "group" {
		scope.Kind = automation.ScopeKindChat
	} else {
		scope.Kind = automation.ScopeKindUser
	}
	goal, err := snapshot.statusService.GetGoal(scope)
	if err != nil {
		return "当前会话没有设置目标。\n\n用 `/goal <目标描述>` 来创建一个长期目标，Alice 会自动持续执行直到完成。"
	}
	statusText := map[automation.GoalStatus]string{
		automation.GoalStatusActive:   "执行中",
		automation.GoalStatusPaused:   "已暂停",
		automation.GoalStatusComplete: "已完成",
		automation.GoalStatusTimeout:  "已超时",
	}[goal.Status]
	if statusText == "" {
		statusText = string(goal.Status)
	}
	elapsed := time.Since(goal.CreatedAt)
	remaining := time.Until(goal.DeadlineAt)
	lines := []string{
		"**目标**",
		"",
		"状态: " + statusText,
		"目标: " + goal.Objective,
		"已用时间: " + formatDurationShort(elapsed),
	}
	if !goal.DeadlineAt.IsZero() {
		lines = append(lines, "截止时间: "+goal.DeadlineAt.Format("2006-01-02 15:04"))
		if remaining > 0 {
			lines = append(lines, "剩余时间: "+formatDurationShort(remaining))
		}
	}
	return strings.Join(lines, "  \n")
}

func formatDurationShort(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh%dm", h, m)
	}
	s := int(d.Seconds()) % 60
	if m > 0 {
		return fmt.Sprintf("%dm%ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

type sessionDirective struct {
	SessionID string
	Remainder string
}

func parseSessionDirective(text string) (sessionDirective, error) {
	trimmed := strings.TrimSpace(text)
	fields := strings.Fields(trimmed)
	if len(fields) == 0 || !strings.EqualFold(fields[0], sessionCommandName) {
		return sessionDirective{}, fmt.Errorf("用法：`/session <backend-session-id> [instruction]`")
	}
	if len(fields) < 2 {
		return sessionDirective{}, fmt.Errorf("用法：`/session <backend-session-id> [instruction]`")
	}
	afterCommand := strings.TrimSpace(trimmed[len(fields[0]):])
	remainder := strings.TrimSpace(afterCommand[len(fields[1]):])
	return sessionDirective{
		SessionID: strings.TrimSpace(fields[1]),
		Remainder: remainder,
	}, nil
}

func validateBackendSessionID(sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return fmt.Errorf("用法：`/session <backend-session-id> [instruction]`")
	}
	if !backendSessionIDPattern.MatchString(sessionID) {
		return fmt.Errorf("后端 session id 包含不支持的字符：`%s`", sanitizeInlineCode(sessionID))
	}
	return nil
}

func (p *Processor) processHelpCommand(ctx context.Context, job Job) JobProcessState {
	reply := p.buildBuiltinHelpMarkdown(p.runtimeSnapshot().helpConfig)
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
	if strings.TrimSpace(replyJob.Scene) != jobSceneWork {
		replyJob.Scene = jobSceneChat
		replyJob.CreateFeishuThread = false
	}
	if err := p.replies.respondCardWithTitle(ctx, replyJob, builtinStatusCardTitle, reply); err != nil {
		logging.Errorf("send builtin status reply failed event_id=%s: %v", job.EventID, err)
	}
	return JobProcessCompleted
}

func (p *Processor) processSessionCommand(ctx context.Context, job Job) JobProcessState {
	directive, err := parseSessionDirective(job.Text)
	if err != nil {
		replyJob := forceDirectReplyJob(job)
		if err := p.replies.respond(ctx, replyJob, err.Error()); err != nil {
			logging.Errorf("send builtin session usage reply failed event_id=%s: %v", job.EventID, err)
		}
		return JobProcessCompleted
	}
	if strings.TrimSpace(job.Scene) != jobSceneWork {
		reply := "当前不在 work thread 中。请使用 `@Alice #work /session <backend-session-id>` 新建 work thread 并绑定后端 session。"
		replyJob := forceDirectReplyJob(job)
		if err := p.replies.respond(ctx, replyJob, reply); err != nil {
			logging.Errorf("send builtin session scope reply failed event_id=%s: %v", job.EventID, err)
		}
		return JobProcessCompleted
	}
	if err := validateBackendSessionID(directive.SessionID); err != nil {
		replyJob := forceDirectReplyJob(job)
		if err := p.replies.respond(ctx, replyJob, err.Error()); err != nil {
			logging.Errorf("send builtin session validation reply failed event_id=%s: %v", job.EventID, err)
		}
		return JobProcessCompleted
	}
	sessionKey := sessionKeyForJob(job)
	p.touchSessionMessage(sessionKey, p.now())
	p.recordSessionMetadata(sessionKey, job)
	p.setThreadID(sessionKey, directive.SessionID)

	reply := p.buildWorkThreadStatusMarkdown(job, "已绑定后端 session。")
	if err := p.replies.respondCardWithTitle(ctx, job, builtinWorkThreadCardTitle, reply); err != nil {
		logging.Errorf("send builtin session reply failed event_id=%s: %v", job.EventID, err)
	}
	return JobProcessCompleted
}

func forceDirectReplyJob(job Job) Job {
	job.Scene = jobSceneChat
	job.CreateFeishuThread = false
	return job
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
			lines = append(lines, fmt.Sprintf("- %s`%s`", prefix, sanitizeInlineCode(entry.Name())))
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

type builtinHelpTemplateData struct {
	ChatEnabled     bool
	WorkEnabled     bool
	WorkModeTrigger string
}

func (p *Processor) buildBuiltinHelpMarkdown(helpCfg builtinHelpConfig) string {
	rendered, err := p.renderPromptFile(connectorPromptBuiltinHelp, builtinHelpTemplateData{
		ChatEnabled:     helpCfg.chatEnabled,
		WorkEnabled:     helpCfg.workEnabled,
		WorkModeTrigger: formatWorkModeTrigger(helpCfg),
	})
	if err != nil {
		logging.Warnf("render builtin help failed template=%s err=%v", connectorPromptBuiltinHelp, err)
		return fmt.Sprintf("内建帮助模板加载失败：`%s`", sanitizeInlineCode(connectorPromptBuiltinHelp))
	}
	return rendered
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
	lines = append(lines, "", "### 当前 Session", "")
	lines = append(lines, p.formatCurrentSessionStatusLines(job)...)
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

func (p *Processor) buildWorkThreadStatusMarkdown(job Job, headline string) string {
	lines := []string{}
	if headline = strings.TrimSpace(headline); headline != "" {
		lines = append(lines, headline, "")
	}
	lines = append(lines, "### 当前 Session", "")
	lines = append(lines, p.formatCurrentSessionStatusLines(job)...)
	return strings.Join(lines, "\n")
}

func (p *Processor) formatCurrentSessionStatusLines(job Job) []string {
	sessionKey := sessionKeyForJob(job)
	canonicalKey, state, ok := p.snapshotSessionState(sessionKey)
	if canonicalKey == "" {
		canonicalKey = sessionKey
	}

	scene := strings.TrimSpace(job.Scene)
	if scene == "" {
		scene = detectSceneFromSessionKey(canonicalKey)
	}
	if scene == "" {
		scene = "legacy"
	}

	backendProvider := firstNonEmptyString(job.LLMProvider, state.BackendProvider)
	if backendProvider == "" {
		backendProvider = "default"
	}
	backendModel := firstNonEmptyString(job.LLMModel, state.BackendModel)
	backendProfile := firstNonEmptyString(job.LLMProfile, state.BackendProfile)
	backendSessionID := strings.TrimSpace(state.ThreadID)
	feishuThreadID := firstNonEmptyString(job.ThreadID, state.WorkThreadID)
	workDir := p.resolveWorkDir(job)

	lines := []string{
		fmt.Sprintf("- scene: `%s`", sanitizeInlineCode(scene)),
		fmt.Sprintf("- Alice session key: `%s`", sanitizeInlineCode(canonicalKey)),
		fmt.Sprintf("- Feishu thread id: `%s`", sanitizeInlineCode(defaultStatusValue(feishuThreadID, "未记录"))),
		fmt.Sprintf("- backend: `%s`", sanitizeInlineCode(backendProvider)),
	}
	if backendProfile != "" {
		lines = append(lines, fmt.Sprintf("- backend profile: `%s`", sanitizeInlineCode(backendProfile)))
	}
	if backendModel != "" {
		lines = append(lines, fmt.Sprintf("- backend model: `%s`", sanitizeInlineCode(backendModel)))
	}
	lines = append(lines,
		fmt.Sprintf("- backend session id: `%s`", sanitizeInlineCode(defaultStatusValue(backendSessionID, "未开始"))),
		fmt.Sprintf("- cwd: `%s`", sanitizeInlineCode(workDir)),
	)
	if command := backendResumeCommand(backendProvider, backendSessionID, workDir); command != "" {
		lines = append(lines, fmt.Sprintf("- CLI resume: `%s`", sanitizeInlineCode(command)))
	}
	if !ok {
		lines = append(lines, "- session state: `未持久化`")
	}
	return lines
}

func detectSceneFromSessionKey(sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	switch {
	case strings.Contains(sessionKey, workSceneToken):
		return jobSceneWork
	case strings.Contains(sessionKey, chatSceneToken):
		return jobSceneChat
	default:
		return ""
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func defaultStatusValue(value, fallback string) string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return trimmed
	}
	return fallback
}

func backendResumeCommand(provider, sessionID, workDir string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	sessionID = strings.TrimSpace(sessionID)
	workDir = strings.TrimSpace(workDir)
	if sessionID == "" {
		return ""
	}
	switch provider {
	case "codex", "":
		if workDir != "" {
			return "codex resume -C " + shellQuote(workDir) + " " + shellQuote(sessionID)
		}
		return "codex resume " + shellQuote(sessionID)
	case "claude":
		command := "claude --resume " + shellQuote(sessionID)
		if workDir != "" {
			return "cd " + shellQuote(workDir) + " && " + command
		}
		return command
	default:
		return ""
	}
}

func shellQuote(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "''"
	}
	if strings.IndexFunc(value, func(r rune) bool {
		return !(r >= 'A' && r <= 'Z') &&
			!(r >= 'a' && r <= 'z') &&
			!(r >= '0' && r <= '9') &&
			!strings.ContainsRune("_+-=.,/:@", r)
	}) < 0 {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func formatBuiltinStatusTaskLine(task automation.Task) string {
	parts := []string{fmt.Sprintf("- `%s`", sanitizeInlineCode(task.ID))}
	if title := strings.TrimSpace(task.Title); title != "" {
		parts = append(parts, title)
	}
	parts = append(parts, "`run_llm`")
	if !task.NextRunAt.IsZero() {
		parts = append(parts, fmt.Sprintf("next `%s`", task.NextRunAt.Local().Format("2006-01-02 15:04:05")))
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

func formatBuiltinStatusTime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.Local().Format("2006-01-02 15:04:05")
}

func sanitizeInlineCode(value string) string {
	return strings.ReplaceAll(strings.TrimSpace(value), "`", "'")
}
