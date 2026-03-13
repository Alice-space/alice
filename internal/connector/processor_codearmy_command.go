package connector

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/codearmy"
	"github.com/Alice-space/alice/internal/logging"
)

const (
	codeArmyCommandName         = "/codearmy"
	codeArmyHistoryPreviewLimit = 3
)

var codeArmyStatusLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

type codeArmyTaskStore interface {
	ListTasks(scope automation.Scope, statusFilter string, limit int) ([]automation.Task, error)
}

type codeArmyCommand struct {
	action   string
	stateKey string
}

func (p *Processor) SetCodeArmyCommandDependencies(inspector *codearmy.Inspector, store *automation.Store) {
	if p == nil {
		return
	}
	p.codeArmyStatus = inspector
	p.automationStore = store
}

func (p *Processor) processBuiltinCommand(ctx context.Context, job Job) (bool, JobProcessState) {
	cmd, ok := parseCodeArmyCommand(job.Text)
	if !ok {
		return false, JobProcessCompleted
	}
	if cmd.action != "status" {
		return false, JobProcessCompleted
	}
	return true, p.processCodeArmyStatusCommand(ctx, job, cmd.stateKey)
}

func parseCodeArmyCommand(text string) (codeArmyCommand, bool) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) < 2 {
		return codeArmyCommand{}, false
	}
	command := strings.ToLower(strings.TrimSpace(fields[0]))
	if command != codeArmyCommandName {
		return codeArmyCommand{}, false
	}
	if strings.ToLower(strings.TrimSpace(fields[1])) != "status" {
		return codeArmyCommand{}, false
	}
	switch len(fields) {
	case 2:
		return codeArmyCommand{action: "status"}, true
	case 3:
		return codeArmyCommand{action: "status", stateKey: strings.TrimSpace(fields[2])}, true
	default:
		return codeArmyCommand{}, false
	}
}

func (p *Processor) processCodeArmyStatusCommand(ctx context.Context, job Job, stateKey string) JobProcessState {
	reply := p.buildCodeArmyStatusReply(job, stateKey)
	if strings.TrimSpace(reply) == "" {
		reply = "当前会话暂无 code_army 任务或状态。"
	}

	sendErr := p.replies.respond(ctx, job, reply)
	if sendErr != nil {
		logging.Errorf("send code_army status reply failed event_id=%s: %v", job.EventID, sendErr)
	}
	return JobProcessCompleted
}

func (p *Processor) buildCodeArmyStatusReply(job Job, requestedStateKey string) string {
	requestedStateKey = strings.TrimSpace(requestedStateKey)
	states, stateErr := p.loadCodeArmyStates(job, requestedStateKey)
	tasks, taskErr := p.loadActiveCodeArmyTasks(job, requestedStateKey)

	if stateErr != nil && taskErr != nil {
		return fmt.Sprintf(
			"## Code Army 状态\n\n> 查询失败。\n> 任务错误：`%v`\n> 状态错误：`%v`",
			taskErr,
			stateErr,
		)
	}
	if stateErr != nil {
		return fmt.Sprintf("## Code Army 状态\n\n> 状态读取失败：`%v`", stateErr)
	}
	if taskErr != nil {
		return fmt.Sprintf("## Code Army 状态\n\n> 任务读取失败：`%v`", taskErr)
	}

	if len(tasks) == 0 && len(states) == 0 {
		if requestedStateKey != "" {
			return fmt.Sprintf("## Code Army 状态\n\n> 当前会话没有找到 `state_key=%s` 的 `code_army` 状态。", requestedStateKey)
		}
		return "## Code Army 状态\n\n> 当前会话暂无 `code_army` 任务或状态。"
	}
	return buildCodeArmyStatusMarkdown(requestedStateKey, tasks, states)
}

func buildCodeArmyStatusMarkdown(
	requestedStateKey string,
	tasks []automation.Task,
	states []codearmy.StateSnapshot,
) string {
	lines := []string{
		"## Code Army 状态",
		fmt.Sprintf("**运行中的任务**：`%d`", len(tasks)),
		fmt.Sprintf("**工作流快照**：`%d`", len(states)),
	}
	if requestedStateKey != "" {
		lines = append(lines, fmt.Sprintf("**筛选 state_key**：`%s`", requestedStateKey))
	}

	lines = append(lines, "", "### 运行中的任务")
	if len(tasks) == 0 {
		lines = append(lines, "> 暂无正在运行的 `code_army` 任务。")
	} else {
		for i, task := range tasks {
			lines = append(lines,
				fmt.Sprintf("**%d. `%s`**", i+1, defaultIfEmpty(task.Action.StateKey, "default")),
				fmt.Sprintf("- `task_id`: `%s`", task.ID),
				fmt.Sprintf("- `status`: `%s`", task.Status),
				fmt.Sprintf("- `next_run_at`: `%s`", formatCommandTime(task.NextRunAt)),
			)
		}
	}

	lines = append(lines, "", "### 工作流快照")
	if len(states) == 0 {
		lines = append(lines, "> 暂无可用的 `code_army` 工作流快照。")
	} else {
		for i, state := range states {
			lines = append(lines,
				fmt.Sprintf("**%d. `%s`**", i+1, defaultIfEmpty(state.StateKey, "default")),
				fmt.Sprintf("- `phase`: `%s`", formatCodeArmyPhase(state.Phase)),
				fmt.Sprintf("- `iteration`: `%d`", state.Iteration),
				fmt.Sprintf("- `last_decision`: `%s`", formatCodeArmyDecision(state.LastDecision)),
				fmt.Sprintf("- `updated_at`: `%s`", formatCommandTime(state.UpdatedAt)),
			)
			if objective := compactCodeArmyText(state.Objective, 140); objective != "" {
				lines = append(lines, fmt.Sprintf("- `objective`: %s", objective))
			}
			if historyLines := formatCodeArmyHistory(state.History, codeArmyHistoryPreviewLimit); len(historyLines) > 0 {
				lines = append(lines, "- 最近记录：")
				lines = append(lines, historyLines...)
			}
		}
	}

	return strings.Join(lines, "\n")
}

func (p *Processor) loadCodeArmyStates(job Job, requestedStateKey string) ([]codearmy.StateSnapshot, error) {
	if p == nil || p.codeArmyStatus == nil {
		return nil, nil
	}
	sessionKeys := codeArmySessionKeysForJob(job)
	if len(sessionKeys) == 0 {
		return nil, nil
	}

	merged := make(map[string]codearmy.StateSnapshot)
	for _, sessionKey := range sessionKeys {
		var snapshots []codearmy.StateSnapshot
		if requestedStateKey != "" {
			state, err := p.codeArmyStatus.Get(sessionKey, requestedStateKey)
			if err != nil {
				if errors.Is(err, codearmy.ErrStateNotFound) {
					continue
				}
				return nil, err
			}
			snapshots = []codearmy.StateSnapshot{state}
		} else {
			list, err := p.codeArmyStatus.List(sessionKey)
			if err != nil {
				return nil, err
			}
			snapshots = list
		}
		for _, snapshot := range snapshots {
			key := defaultIfEmpty(snapshot.StateKey, "default")
			current, exists := merged[key]
			if !exists || snapshot.UpdatedAt.After(current.UpdatedAt) {
				merged[key] = snapshot
			}
		}
	}

	out := make([]codearmy.StateSnapshot, 0, len(merged))
	for _, snapshot := range merged {
		out = append(out, snapshot)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
		return out[i].StateKey < out[j].StateKey
	})
	return out, nil
}

func (p *Processor) loadActiveCodeArmyTasks(job Job, requestedStateKey string) ([]automation.Task, error) {
	if p == nil || p.automationStore == nil {
		return nil, nil
	}
	scope, ok := automationScopeForJob(job)
	if !ok {
		return nil, nil
	}
	list, err := p.automationStore.ListTasks(scope, string(automation.TaskStatusActive), 200)
	if err != nil {
		return nil, err
	}

	out := make([]automation.Task, 0, len(list))
	for _, task := range list {
		if task.Action.Type != automation.ActionTypeRunWorkflow {
			continue
		}
		if task.Action.Workflow != automation.WorkflowCodeArmy {
			continue
		}
		if requestedStateKey != "" && strings.TrimSpace(task.Action.StateKey) != strings.TrimSpace(requestedStateKey) {
			continue
		}
		out = append(out, task)
	}
	sort.Slice(out, func(i, j int) bool {
		left := out[i]
		right := out[j]
		if !left.NextRunAt.Equal(right.NextRunAt) {
			if left.NextRunAt.IsZero() {
				return false
			}
			if right.NextRunAt.IsZero() {
				return true
			}
			return left.NextRunAt.Before(right.NextRunAt)
		}
		return left.ID < right.ID
	})
	return out, nil
}

func automationScopeForJob(job Job) (automation.Scope, bool) {
	if isGroupChatType(job.ChatType) {
		if strings.TrimSpace(job.ReceiveID) == "" {
			return automation.Scope{}, false
		}
		return automation.Scope{Kind: automation.ScopeKindChat, ID: strings.TrimSpace(job.ReceiveID)}, true
	}

	actorID := preferredID(job.SenderOpenID, job.SenderUserID, job.SenderUnionID)
	if actorID == "" {
		return automation.Scope{}, false
	}
	return automation.Scope{Kind: automation.ScopeKindUser, ID: actorID}, true
}

func codeArmySessionKeysForJob(job Job) []string {
	candidates := []string{
		strings.TrimSpace(memoryScopeKeyForJob(job)),
		strings.TrimSpace(sessionKeyForJob(job)),
	}
	keys := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		keys = append(keys, candidate)
	}
	return keys
}

func formatCodeArmyPhase(phase string) string {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "manager":
		return "manager · 规划"
	case "worker":
		return "worker · 执行"
	case "reviewer":
		return "reviewer · 评审"
	case "gate":
		return "gate · 决策"
	default:
		return defaultIfEmpty(strings.TrimSpace(phase), "unknown")
	}
}

func formatCodeArmyDecision(decision string) string {
	switch strings.ToLower(strings.TrimSpace(decision)) {
	case "pass":
		return "pass · 通过"
	case "fail":
		return "fail · 返工"
	default:
		return defaultIfEmpty(strings.TrimSpace(decision), "n/a")
	}
}

func formatCodeArmyHistory(history []codearmy.HistoryRecord, limit int) []string {
	if len(history) == 0 || limit <= 0 {
		return nil
	}
	start := 0
	if len(history) > limit {
		start = len(history) - limit
	}
	lines := make([]string, 0, len(history)-start)
	for _, item := range history[start:] {
		line := fmt.Sprintf("  - `%s` · `%s`", formatCommandTime(item.At), formatCodeArmyPhase(item.Phase))
		if decision := strings.TrimSpace(item.Decision); decision != "" {
			line += fmt.Sprintf(" · `%s`", formatCodeArmyDecision(decision))
		}
		if summary := compactCodeArmyText(item.Summary, 80); summary != "" {
			line += " · " + summary
		}
		lines = append(lines, line)
	}
	return lines
}

func compactCodeArmyText(text string, maxRunes int) string {
	normalized := strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if normalized == "" {
		return ""
	}
	return clipText(normalized, maxRunes)
}

func formatCommandTime(value time.Time) string {
	if value.IsZero() {
		return "n/a"
	}
	return value.In(codeArmyStatusLocation).Format("2006-01-02 15:04:05") + " Asia/Shanghai"
}
