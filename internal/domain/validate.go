package domain

import (
	"errors"
	"fmt"
	"strings"
)

func ValidateProjectSpec(p ProjectSpec) error {
	if p.ProjectID == "" {
		return errors.New("project_id is required")
	}
	if p.Name == "" {
		return errors.New("name is required")
	}
	if p.Goal == "" {
		return errors.New("goal is required")
	}
	if len(p.Repositories) == 0 {
		return errors.New("repositories is required")
	}
	if len(p.WorkspacePolicy.AllowedRoots) == 0 {
		return errors.New("workspace_policy.allowed_roots is required")
	}
	for _, repo := range p.Repositories {
		if err := ValidateRepositorySpec(repo); err != nil {
			return fmt.Errorf("invalid repository %s: %w", repo.RepositoryID, err)
		}
	}
	return nil
}

func ValidateRepositorySpec(r RepositorySpec) error {
	if r.RepositoryID == "" {
		return errors.New("repository_id is required")
	}
	if r.Name == "" {
		return errors.New("name is required")
	}
	if r.Root == "" {
		return errors.New("root is required")
	}
	switch r.WorkspaceStrategy {
	case WorkspaceStrategyShared, WorkspaceStrategyWorktree, WorkspaceStrategyEphemeralCopy:
	default:
		return fmt.Errorf("unsupported workspace_strategy: %q", r.WorkspaceStrategy)
	}
	return nil
}

func ValidateTaskTemplate(t TaskTemplate) error {
	if t.TaskTemplateID == "" || t.ProjectID == "" || t.GoalTemplate == "" || t.DefaultTargetExecutor == "" {
		return errors.New("task_template requires id, project_id, goal_template and default_target_executor")
	}
	if err := validateTaskType(t.TaskType); err != nil {
		return err
	}
	if err := validateWriteScope(t.DefaultWriteScope); err != nil {
		return err
	}
	return nil
}

func ValidateTaskSpec(t TaskSpec) error {
	if t.TaskID == "" || t.ProjectID == "" || t.Goal == "" {
		return errors.New("task requires task_id, project_id and goal")
	}
	if err := validateTaskType(t.TaskType); err != nil {
		return err
	}
	if err := validateWriteScope(t.WriteScope); err != nil {
		return err
	}
	if err := validateTrigger(t.Trigger); err != nil {
		return err
	}
	if t.TaskType == TaskTypeEvaluation {
		if t.WriteScope == WriteScopeNone {
			return errors.New("evaluation task must not have write_scope=none")
		}
	}
	if t.TaskType == TaskTypeScheduledWatch && t.Trigger != TriggerScheduler {
		return errors.New("scheduled_watch must be triggered by scheduler")
	}
	if t.ResourceLimits.TokenLimit < 0 || t.ResourceLimits.GPUMinuteLimit < 0 || t.ResourceLimits.MaxToolCalls < 0 {
		return errors.New("resource_limits must be non-negative")
	}
	if t.RetryPolicy.MaxRetries < 0 {
		return errors.New("retry_policy.max_retries must be non-negative")
	}
	if t.AcceptancePolicy.MinSampleSize < 0 {
		return errors.New("acceptance_policy.min_sample_size must be non-negative")
	}
	return nil
}

func ValidateEvalTask(t EvalTask) error {
	if err := ValidateTaskSpec(t.TaskSpec); err != nil {
		return err
	}
	if t.TaskType != TaskTypeEvaluation {
		return errors.New("eval task must have task_type=evaluation")
	}
	if t.EvalID == "" || t.PRID == "" || t.CommitSHA == "" {
		return errors.New("eval task requires eval_id, pr_id and commit_sha")
	}
	if t.BaselineRef == "" || t.DatasetRef == "" || t.EnvironmentRef == "" || t.BenchmarkSuiteRef == "" || t.SeedPolicy == "" {
		return errors.New("eval task requires baseline, dataset, environment, benchmark and seed policy")
	}
	return nil
}

func ValidateRunRecord(r RunRecord) error {
	if r.RunID == "" || r.TaskID == "" {
		return errors.New("run requires run_id and task_id")
	}
	if err := validateRunMode(r.RunMode); err != nil {
		return err
	}
	if err := validateRunStatus(r.RunStatus); err != nil {
		return err
	}
	if r.RunMode == RunModeFast {
		if r.WorkflowID != "" {
			return errors.New("fast run must not have workflow_id")
		}
		if r.WorkflowPhase != "" {
			return errors.New("fast run must not have workflow_phase")
		}
	} else {
		if r.WorkflowID == "" {
			return errors.New("workflow/evaluation runs require workflow_id")
		}
		if r.WorkflowPhase == "" {
			return errors.New("workflow/evaluation runs require workflow_phase")
		}
		if err := validateWorkflowPhase(r.WorkflowPhase); err != nil {
			return err
		}
	}
	if r.WorkflowPhase == WorkflowPhaseDone && r.RunStatus != RunStatusSucceeded {
		return errors.New("workflow_phase Done requires run_status Succeeded")
	}
	if r.WorkflowPhase == WorkflowPhaseAborted && r.RunStatus != RunStatusAborted {
		return errors.New("workflow_phase Aborted requires run_status Aborted")
	}
	if r.RunStatus == RunStatusSuperseded && r.SupersededBy == "" {
		return errors.New("superseded run requires superseded_by")
	}
	if r.StateVersion < 0 {
		return errors.New("state_version must be non-negative")
	}
	return nil
}

func ValidateIntentSpec(i IntentSpec) error {
	if i.IntentID == "" || i.RawText == "" || i.ActorID == "" || i.Channel == "" {
		return errors.New("intent requires intent_id, raw_text, actor_id and channel")
	}
	if err := validateIntentKind(i.IntentKind); err != nil {
		return err
	}
	if err := validateIntentScope(i.Scope); err != nil {
		return err
	}
	if i.Confidence < 0 || i.Confidence > 1 {
		return errors.New("intent confidence must be within [0, 1]")
	}
	return nil
}

func ValidateRuntimeSetting(s RuntimeSetting) error {
	if s.SettingID == "" || s.SettingKey == "" || s.SourceIntentID == "" {
		return errors.New("runtime setting requires setting_id, setting_key and source_intent_id")
	}
	switch s.MutableClass {
	case RuntimeSettingSafe, RuntimeSettingGuarded, RuntimeSettingImmutable:
	default:
		return fmt.Errorf("invalid mutable_class: %q", s.MutableClass)
	}
	switch s.Status {
	case RuntimeSettingDraft, RuntimeSettingActive, RuntimeSettingRejected, RuntimeSettingSuperseded:
	default:
		return fmt.Errorf("invalid runtime setting status: %q", s.Status)
	}
	return nil
}

func ValidateConfigChangeProposal(p ConfigChangeProposal) error {
	if p.ProposalID == "" || p.SourceIntentID == "" || p.TargetRef == "" {
		return errors.New("config proposal requires proposal_id, source_intent_id and target_ref")
	}
	switch p.ProposalKind {
	case ConfigProposalStaticConfigPatch, ConfigProposalSecretRotation, ConfigProposalIntegrationRebind:
	default:
		return fmt.Errorf("invalid proposal_kind: %q", p.ProposalKind)
	}
	switch p.Status {
	case ConfigProposalDraft, ConfigProposalWaitingHuman, ConfigProposalApproved, ConfigProposalRejected, ConfigProposalApplied:
	default:
		return fmt.Errorf("invalid config proposal status: %q", p.Status)
	}
	return nil
}

func ValidateApprovalTicket(t ApprovalTicket) error {
	if t.ApprovalID == "" || t.RunID == "" || t.ActionKind == "" {
		return errors.New("approval ticket requires approval_id, run_id and action_kind")
	}
	switch t.Status {
	case ApprovalPending, ApprovalApproved, ApprovalRejected, ApprovalExpired:
	default:
		return fmt.Errorf("invalid approval status: %q", t.Status)
	}
	return nil
}

func ValidateMemoryRecord(m MemoryRecord) error {
	if m.MemoryID == "" || m.Summary == "" || m.Source == "" {
		return errors.New("memory requires memory_id, summary and source")
	}
	switch m.Scope {
	case MemoryScopeGlobal, MemoryScopeProject, MemoryScopeTask, MemoryScopeRun:
	default:
		return fmt.Errorf("invalid memory scope: %q", m.Scope)
	}
	switch m.MemoryType {
	case MemoryTypePreference, MemoryTypeFact, MemoryTypeDecision, MemoryTypeFailurePattern, MemoryTypePlaybook, MemoryTypeEvaluationBaseline:
	default:
		return fmt.Errorf("invalid memory type: %q", m.MemoryType)
	}
	if m.Confidence < 0 || m.Confidence > 1 {
		return errors.New("memory confidence must be within [0, 1]")
	}
	return nil
}

func ValidateScheduleEntry(s ScheduleEntry) error {
	if s.ScheduleID == "" || s.TaskTemplateID == "" {
		return errors.New("schedule requires schedule_id and task_template_id")
	}
	switch s.Status {
	case ScheduleActive, SchedulePaused, ScheduleArchived:
	default:
		return fmt.Errorf("invalid schedule status: %q", s.Status)
	}
	if strings.TrimSpace(s.Timezone) == "" {
		return errors.New("schedule timezone is required")
	}
	if s.TriggerSpec.Interval <= 0 && strings.TrimSpace(s.TriggerSpec.CronExpr) == "" {
		return errors.New("schedule requires either interval or cron_expr")
	}
	return nil
}

func ValidateEvent(e Event) error {
	if e.EventID == "" || e.EventType == "" || e.Source == "" || e.CreatedAt.IsZero() {
		return errors.New("event requires event_id, event_type, source, created_at")
	}
	if e.IdempotencyKey == "" {
		return errors.New("event requires idempotency_key")
	}
	return nil
}

func ValidateLockRecord(l LockRecord) error {
	if l.LockID == "" || l.LockType == "" || l.LockKey == "" || l.OwnerRunID == "" {
		return errors.New("lock requires lock_id, lock_type, lock_key and owner_run_id")
	}
	if l.LeaseToken <= 0 {
		return errors.New("lease_token must be positive")
	}
	if !l.ExpiresAt.After(l.CreatedAt) {
		return errors.New("lock expires_at must be after created_at")
	}
	return nil
}

func ValidateRunStatusTransition(from, to RunStatus) error {
	if from == to {
		return nil
	}
	transitions := map[RunStatus]map[RunStatus]struct{}{
		RunStatusQueued: {
			RunStatusRunning: {},
		},
		RunStatusRunning: {
			RunStatusWaitingHuman: {},
			RunStatusBlocked:      {},
			RunStatusSucceeded:    {},
			RunStatusFailed:       {},
			RunStatusAborted:      {},
			RunStatusSuperseded:   {},
		},
		RunStatusWaitingHuman: {
			RunStatusRunning: {},
			RunStatusAborted: {},
		},
		RunStatusBlocked: {
			RunStatusRunning: {},
			RunStatusFailed:  {},
			RunStatusAborted: {},
		},
	}
	if from.IsTerminal() {
		return fmt.Errorf("terminal run status %s cannot transition", from)
	}
	if _, ok := transitions[from][to]; !ok {
		return fmt.Errorf("invalid run status transition %s -> %s", from, to)
	}
	return nil
}

func ValidateWorkflowPhaseTransition(from, to WorkflowPhase) error {
	if from == to {
		return nil
	}
	if from.IsTerminal() {
		return fmt.Errorf("terminal workflow phase %s cannot transition", from)
	}
	transitions := map[WorkflowPhase]map[WorkflowPhase]struct{}{
		WorkflowPhasePlanning: {
			WorkflowPhaseImplementing:  {},
			WorkflowPhaseExperimenting: {},
			WorkflowPhaseWaitingHuman:  {},
			WorkflowPhaseBlocked:       {},
			WorkflowPhaseAborted:       {},
		},
		WorkflowPhaseImplementing: {
			WorkflowPhaseEvaluating:   {},
			WorkflowPhaseWaitingHuman: {},
			WorkflowPhaseBlocked:      {},
			WorkflowPhaseReporting:    {},
			WorkflowPhaseAborted:      {},
		},
		WorkflowPhaseExperimenting: {
			WorkflowPhaseEvaluating:   {},
			WorkflowPhaseWaitingHuman: {},
			WorkflowPhaseBlocked:      {},
			WorkflowPhaseAborted:      {},
		},
		WorkflowPhaseEvaluating: {
			WorkflowPhaseReporting:    {},
			WorkflowPhaseImplementing: {},
			WorkflowPhasePlanning:     {},
			WorkflowPhaseWaitingHuman: {},
			WorkflowPhaseBlocked:      {},
			WorkflowPhaseAborted:      {},
		},
		WorkflowPhaseReporting: {
			WorkflowPhaseDone:    {},
			WorkflowPhaseAborted: {},
		},
		WorkflowPhaseBlocked: {
			WorkflowPhasePlanning:      {},
			WorkflowPhaseImplementing:  {},
			WorkflowPhaseExperimenting: {},
			WorkflowPhaseEvaluating:    {},
			WorkflowPhaseReporting:     {},
			WorkflowPhaseWaitingHuman:  {},
			WorkflowPhaseAborted:       {},
		},
		WorkflowPhaseWaitingHuman: {
			WorkflowPhasePlanning:      {},
			WorkflowPhaseImplementing:  {},
			WorkflowPhaseExperimenting: {},
			WorkflowPhaseEvaluating:    {},
			WorkflowPhaseReporting:     {},
			WorkflowPhaseAborted:       {},
		},
	}
	if _, ok := transitions[from][to]; !ok {
		return fmt.Errorf("invalid workflow phase transition %s -> %s", from, to)
	}
	return nil
}

func validateTaskType(t TaskType) error {
	switch t {
	case TaskTypeQuery, TaskTypeCodeChange, TaskTypeExperiment, TaskTypeEvaluation, TaskTypeScheduledWatch, TaskTypeReview, TaskTypeReport, TaskTypeMaintenance:
		return nil
	default:
		return fmt.Errorf("unsupported task type: %q", t)
	}
}

func validateTrigger(t TriggerType) error {
	switch t {
	case TriggerUser, TriggerScheduler, TriggerWebhook, TriggerEvalRetry, TriggerManualSignal, TriggerRecovery:
		return nil
	default:
		return fmt.Errorf("unsupported trigger: %q", t)
	}
}

func validateWriteScope(s WriteScope) error {
	switch s {
	case WriteScopeNone, WriteScopeRepoBranch, WriteScopeProject, WriteScopeSettings:
		return nil
	default:
		return fmt.Errorf("unsupported write scope: %q", s)
	}
}

func validateRunMode(m RunMode) error {
	switch m {
	case RunModeFast, RunModeWorkflow, RunModeEvaluation, RunModeMaintenance:
		return nil
	default:
		return fmt.Errorf("unsupported run mode: %q", m)
	}
}

func validateRunStatus(s RunStatus) error {
	switch s {
	case RunStatusQueued, RunStatusRunning, RunStatusWaitingHuman, RunStatusBlocked, RunStatusSucceeded, RunStatusFailed, RunStatusAborted, RunStatusSuperseded:
		return nil
	default:
		return fmt.Errorf("unsupported run status: %q", s)
	}
}

func validateWorkflowPhase(p WorkflowPhase) error {
	switch p {
	case WorkflowPhasePlanning, WorkflowPhaseImplementing, WorkflowPhaseExperimenting, WorkflowPhaseEvaluating, WorkflowPhaseReporting, WorkflowPhaseWaitingHuman, WorkflowPhaseBlocked, WorkflowPhaseDone, WorkflowPhaseAborted:
		return nil
	default:
		return fmt.Errorf("unsupported workflow phase: %q", p)
	}
}

func validateIntentKind(k IntentKind) error {
	switch k {
	case IntentKindTaskRequest, IntentKindSettingChange, IntentKindStateQuery, IntentKindApprovalResponse, IntentKindManualSignal:
		return nil
	default:
		return fmt.Errorf("unsupported intent kind: %q", k)
	}
}

func validateIntentScope(s IntentScope) error {
	switch s {
	case IntentScopeGlobal, IntentScopeProject, IntentScopeRun:
		return nil
	default:
		return fmt.Errorf("unsupported intent scope: %q", s)
	}
}
