package codearmy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"gitee.com/alicespace/alice/internal/automation"
	"gitee.com/alicespace/alice/internal/llm"
)

const (
	stateVersion = 1

	phaseManager  = "manager"
	phaseWorker   = "worker"
	phaseReviewer = "reviewer"
	phaseGate     = "gate"

	decisionPass = "pass"
	decisionFail = "fail"
)

var decisionPattern = regexp.MustCompile(`(?im)^\s*decision\s*:\s*(pass|fail)\b`)
var invalidStateKeyPattern = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

type Runner struct {
	stateDir string
	backend  llm.Backend
	now      func() time.Time
	mu       sync.Mutex
}

type workflowState struct {
	Version          int              `json:"version"`
	Workflow         string           `json:"workflow"`
	Key              string           `json:"key"`
	TaskID           string           `json:"task_id"`
	Phase            string           `json:"phase"`
	Iteration        int              `json:"iteration"`
	Objective        string           `json:"objective"`
	ManagerPlan      string           `json:"manager_plan,omitempty"`
	WorkerOutput     string           `json:"worker_output,omitempty"`
	ReviewerReport   string           `json:"reviewer_report,omitempty"`
	LastDecision     string           `json:"last_decision,omitempty"`
	ManagerThreadID  string           `json:"manager_thread_id,omitempty"`
	WorkerThreadID   string           `json:"worker_thread_id,omitempty"`
	ReviewerThreadID string           `json:"reviewer_thread_id,omitempty"`
	UpdatedAt        time.Time        `json:"updated_at"`
	History          []workflowRecord `json:"history,omitempty"`
}

type workflowRecord struct {
	At       time.Time `json:"at"`
	Phase    string    `json:"phase"`
	Summary  string    `json:"summary"`
	Decision string    `json:"decision,omitempty"`
}

func NewRunner(stateDir string, backend llm.Backend) *Runner {
	return &Runner{
		stateDir: strings.TrimSpace(stateDir),
		backend:  backend,
		now:      time.Now,
	}
}

func (r *Runner) Run(ctx context.Context, req automation.WorkflowRunRequest) (automation.WorkflowRunResult, error) {
	if r == nil {
		return automation.WorkflowRunResult{}, errors.New("code_army runner is nil")
	}
	if r.backend == nil {
		return automation.WorkflowRunResult{}, errors.New("code_army llm backend is nil")
	}

	workflow := strings.ToLower(strings.TrimSpace(req.Workflow))
	if workflow != automation.WorkflowCodeArmy {
		return automation.WorkflowRunResult{}, fmt.Errorf("unsupported workflow %q", req.Workflow)
	}

	taskID := strings.TrimSpace(req.TaskID)
	if taskID == "" {
		return automation.WorkflowRunResult{}, errors.New("workflow task id is empty")
	}

	stateKey := sanitizeStateKey(req.StateKey)
	if stateKey == "" {
		stateKey = sanitizeStateKey(taskID)
	}
	if stateKey == "" {
		return automation.WorkflowRunResult{}, errors.New("workflow state key is empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	statePath := r.stateFilePath(stateKey)
	state, err := r.loadState(statePath, workflow, stateKey, taskID)
	if err != nil {
		return automation.WorkflowRunResult{}, err
	}
	state.Objective = defaultIfEmpty(state.Objective, strings.TrimSpace(req.Prompt))
	if strings.TrimSpace(state.Objective) == "" {
		return automation.WorkflowRunResult{}, errors.New("workflow objective is empty")
	}

	message, err := r.advance(ctx, &state, req)
	if err != nil {
		return automation.WorkflowRunResult{}, err
	}
	if err := r.saveState(statePath, state); err != nil {
		return automation.WorkflowRunResult{}, err
	}
	return automation.WorkflowRunResult{Message: message}, nil
}

func (r *Runner) advance(ctx context.Context, state *workflowState, req automation.WorkflowRunRequest) (string, error) {
	if state == nil {
		return "", errors.New("workflow state is nil")
	}

	now := r.nowUTC()
	switch state.Phase {
	case phaseManager:
		reply, nextThreadID, err := r.runLLM(ctx, state.ManagerThreadID, buildManagerPrompt(*state), req)
		if err != nil {
			return "", err
		}
		state.ManagerPlan = strings.TrimSpace(reply)
		state.ManagerThreadID = strings.TrimSpace(nextThreadID)
		state.Phase = phaseWorker
		state.UpdatedAt = now
		state.appendHistory(now, phaseManager, clipText(state.ManagerPlan, 120), "")
		return fmt.Sprintf(
			"code-army manager 已完成第%d轮规划，下一步进入 worker。\n规划摘要：%s",
			state.Iteration,
			clipText(state.ManagerPlan, 160),
		), nil
	case phaseWorker:
		reply, nextThreadID, err := r.runLLM(ctx, state.WorkerThreadID, buildWorkerPrompt(*state), req)
		if err != nil {
			return "", err
		}
		state.WorkerOutput = strings.TrimSpace(reply)
		state.WorkerThreadID = strings.TrimSpace(nextThreadID)
		state.Phase = phaseReviewer
		state.UpdatedAt = now
		state.appendHistory(now, phaseWorker, clipText(state.WorkerOutput, 120), "")
		return "code-army worker 已产出实现方案，下一步进入 reviewer 审核。", nil
	case phaseReviewer:
		reply, nextThreadID, err := r.runLLM(ctx, state.ReviewerThreadID, buildReviewerPrompt(*state), req)
		if err != nil {
			return "", err
		}
		state.ReviewerReport = strings.TrimSpace(reply)
		state.ReviewerThreadID = strings.TrimSpace(nextThreadID)
		state.LastDecision = parseDecision(state.ReviewerReport)
		state.Phase = phaseGate
		state.UpdatedAt = now
		state.appendHistory(now, phaseReviewer, clipText(state.ReviewerReport, 120), state.LastDecision)
		return fmt.Sprintf(
			"code-army reviewer 已给出结论：%s。下一步进入 gate。",
			strings.ToUpper(state.LastDecision),
		), nil
	case phaseGate:
		state.UpdatedAt = now
		if state.LastDecision == decisionPass {
			state.Iteration++
			state.Phase = phaseManager
			state.appendHistory(now, phaseGate, "gate passed", decisionPass)
			return fmt.Sprintf(
				"code-army gate 通过，进入第%d轮迭代。",
				state.Iteration,
			), nil
		}
		state.Phase = phaseWorker
		state.appendHistory(now, phaseGate, "gate rejected, back to worker", decisionFail)
		return "code-army gate 未通过，已回退到 worker 进行整改。", nil
	default:
		state.Phase = phaseManager
		state.UpdatedAt = now
		state.appendHistory(now, phaseManager, "phase reset to manager", "")
		return "code-army 状态异常已自动修复，已重置到 manager。", nil
	}
}

func (r *Runner) runLLM(
	ctx context.Context,
	threadID string,
	prompt string,
	req automation.WorkflowRunRequest,
) (reply string, nextThreadID string, err error) {
	result, err := r.backend.Run(ctx, llm.RunRequest{
		ThreadID: strings.TrimSpace(threadID),
		UserText: strings.TrimSpace(prompt),
		Model:    strings.TrimSpace(req.Model),
		Profile:  strings.TrimSpace(req.Profile),
		Env:      req.Env,
	})
	if err != nil {
		return "", "", err
	}
	reply = strings.TrimSpace(result.Reply)
	if reply == "" {
		return "", "", errors.New("code_army llm reply is empty")
	}
	nextThreadID = strings.TrimSpace(result.NextThreadID)
	if nextThreadID == "" {
		nextThreadID = strings.TrimSpace(threadID)
	}
	return reply, nextThreadID, nil
}

func buildManagerPrompt(state workflowState) string {
	return "你是代码军队的 manager。\n" +
		"目标：\n" + state.Objective + "\n\n" +
		fmt.Sprintf("当前迭代：第 %d 轮。\n", state.Iteration) +
		"请输出：\n" +
		"1) 本轮目标\n" +
		"2) 最多3条可执行开发任务\n" +
		"3) 验收标准\n" +
		"要求：简洁、可执行、避免空话。"
}

func buildWorkerPrompt(state workflowState) string {
	base := "你是代码军队的 worker。\n" +
		"总体目标：\n" + state.Objective + "\n\n" +
		"manager 规划如下：\n" + defaultIfEmpty(state.ManagerPlan, "（无）") + "\n\n"
	if strings.TrimSpace(state.LastDecision) == decisionFail {
		base += "上轮 gate 未通过，请优先修复 reviewer 指出的问题：\n" +
			defaultIfEmpty(state.ReviewerReport, "（无）") + "\n\n"
	}
	base += "请输出：\n" +
		"1) 具体实现步骤\n" +
		"2) 关键改动点\n" +
		"3) 自测检查项"
	return base
}

func buildReviewerPrompt(state workflowState) string {
	return "你是代码军队的 reviewer。\n" +
		"请基于以下内容做严格评审：\n\n" +
		"目标：\n" + state.Objective + "\n\n" +
		"manager 规划：\n" + defaultIfEmpty(state.ManagerPlan, "（无）") + "\n\n" +
		"worker 产出：\n" + defaultIfEmpty(state.WorkerOutput, "（无）") + "\n\n" +
		"请输出：\n" +
		"- 主要风险\n" +
		"- 改进建议\n" +
		"- 最后一行必须是 `DECISION: PASS` 或 `DECISION: FAIL`"
}

func parseDecision(reply string) string {
	normalized := strings.TrimSpace(reply)
	if normalized == "" {
		return decisionFail
	}
	matches := decisionPattern.FindStringSubmatch(normalized)
	if len(matches) >= 2 {
		return strings.ToLower(strings.TrimSpace(matches[1]))
	}

	lower := strings.ToLower(normalized)
	switch {
	case strings.Contains(lower, "decision: pass"), strings.Contains(lower, "通过"):
		return decisionPass
	case strings.Contains(lower, "decision: fail"), strings.Contains(lower, "不通过"):
		return decisionFail
	default:
		return decisionFail
	}
}

func (r *Runner) loadState(path, workflow, stateKey, taskID string) (workflowState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			now := r.nowUTC()
			return workflowState{
				Version:   stateVersion,
				Workflow:  workflow,
				Key:       stateKey,
				TaskID:    taskID,
				Phase:     phaseManager,
				Iteration: 1,
				UpdatedAt: now,
			}, nil
		}
		return workflowState{}, fmt.Errorf("read code_army state failed: %w", err)
	}

	var state workflowState
	if err := json.Unmarshal(data, &state); err != nil {
		return workflowState{}, fmt.Errorf("parse code_army state failed: %w", err)
	}
	state = normalizeState(state)
	state.Workflow = workflow
	state.Key = stateKey
	if state.TaskID == "" {
		state.TaskID = taskID
	}
	return state, nil
}

func (r *Runner) saveState(path string, state workflowState) error {
	state = normalizeState(state)
	if strings.TrimSpace(path) == "" {
		return errors.New("code_army state path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create code_army state dir failed: %w", err)
	}

	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal code_army state failed: %w", err)
	}
	tmpFile, err := os.CreateTemp(filepath.Dir(path), ".code_army_state.*.tmp")
	if err != nil {
		return fmt.Errorf("create temp code_army state failed: %w", err)
	}
	tmpPath := tmpFile.Name()
	if _, err := tmpFile.Write(raw); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write temp code_army state failed: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp code_army state failed: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("replace code_army state failed: %w", err)
	}
	return nil
}

func normalizeState(state workflowState) workflowState {
	state.Workflow = strings.ToLower(strings.TrimSpace(state.Workflow))
	state.Key = sanitizeStateKey(state.Key)
	state.TaskID = strings.TrimSpace(state.TaskID)
	state.Phase = normalizePhase(state.Phase)
	state.Objective = strings.TrimSpace(state.Objective)
	state.ManagerPlan = strings.TrimSpace(state.ManagerPlan)
	state.WorkerOutput = strings.TrimSpace(state.WorkerOutput)
	state.ReviewerReport = strings.TrimSpace(state.ReviewerReport)
	state.LastDecision = strings.ToLower(strings.TrimSpace(state.LastDecision))
	state.ManagerThreadID = strings.TrimSpace(state.ManagerThreadID)
	state.WorkerThreadID = strings.TrimSpace(state.WorkerThreadID)
	state.ReviewerThreadID = strings.TrimSpace(state.ReviewerThreadID)
	if state.Version <= 0 {
		state.Version = stateVersion
	}
	if state.Iteration <= 0 {
		state.Iteration = 1
	}
	return state
}

func normalizePhase(phase string) string {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case phaseManager, phaseWorker, phaseReviewer, phaseGate:
		return strings.ToLower(strings.TrimSpace(phase))
	default:
		return phaseManager
	}
}

func (s *workflowState) appendHistory(at time.Time, phase, summary, decision string) {
	if s == nil {
		return
	}
	s.History = append(s.History, workflowRecord{
		At:       at.UTC(),
		Phase:    normalizePhase(phase),
		Summary:  strings.TrimSpace(summary),
		Decision: strings.ToLower(strings.TrimSpace(decision)),
	})
	if len(s.History) > 24 {
		s.History = append([]workflowRecord(nil), s.History[len(s.History)-24:]...)
	}
}

func (r *Runner) nowUTC() time.Time {
	if r == nil || r.now == nil {
		return time.Now().UTC()
	}
	now := r.now()
	if now.IsZero() {
		return time.Now().UTC()
	}
	return now.UTC()
}

func (r *Runner) stateFilePath(stateKey string) string {
	root := strings.TrimSpace(r.stateDir)
	if root == "" {
		root = filepath.Join(".memory", "code_army")
	}
	return filepath.Join(root, sanitizeStateKey(stateKey)+".json")
}

func sanitizeStateKey(raw string) string {
	key := strings.TrimSpace(raw)
	if key == "" {
		return ""
	}
	key = invalidStateKeyPattern.ReplaceAllString(key, "_")
	key = strings.Trim(key, "._-")
	return strings.ToLower(strings.TrimSpace(key))
}

func defaultIfEmpty(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return strings.TrimSpace(fallback)
	}
	return value
}

func clipText(value string, max int) string {
	value = strings.TrimSpace(value)
	if max <= 0 || len(value) <= max {
		return value
	}
	return value[:max] + "..."
}
