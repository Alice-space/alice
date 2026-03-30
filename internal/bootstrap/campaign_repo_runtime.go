package bootstrap

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/campaign"
	"github.com/Alice-space/alice/internal/campaignrepo"
	"github.com/Alice-space/alice/internal/config"
	"github.com/Alice-space/alice/internal/logging"
)

const (
	campaignRepoReconcileInterval   = 5 * time.Minute
	campaignRepoDispatchLease       = 2 * time.Hour
	campaignRepoTaskEverySeconds    = 60
	campaignDispatchFailureCooldown = 1 * time.Minute
	campaignDispatchStatePrefix     = "campaign_dispatch:"
	campaignWakeStatePrefix         = "campaign_wake:"
)

func (b *connectorRuntimeBuilder) registerCampaignRepoSystemTask() error {
	if b == nil || b.automationEngine == nil || b.automationStore == nil || b.campaignStore == nil {
		return nil
	}
	return b.automationEngine.RegisterSystemTask("system.campaign_repo_reconcile", campaignRepoReconcileInterval, func(context.Context) {
		b.runCampaignRepoReconcile()
	})
}

func (b *connectorRuntimeBuilder) runCampaignRepoReconcile() {
	if b == nil || b.campaignStore == nil {
		return
	}
	b.campaignRepoMu.Lock()
	defer b.campaignRepoMu.Unlock()

	campaigns, err := b.campaignStore.ListAllCampaigns("", -1)
	if err != nil {
		logging.Warnf("list campaigns for repo reconcile failed: %v", err)
		return
	}
	now := time.Now().Local()
	for _, item := range campaigns {
		b.reconcileCampaignRepo(item, now)
	}
}

func (b *connectorRuntimeBuilder) handleCampaignRepoAutomationTaskCompletion(task automation.Task, runErr error) {
	campaignID, ok := campaignIDFromAutomationStateKey(task.Action.StateKey)
	if !ok {
		return
	}
	if b == nil || b.campaignStore == nil {
		return
	}
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return
	}
	b.campaignRepoMu.Lock()
	defer b.campaignRepoMu.Unlock()
	b.handleCampaignRepoTaskSignals(campaignID, task)
	if runErr == nil {
		item, err := b.campaignStore.GetCampaign(campaignID)
		if err != nil {
			logging.Warnf("load campaign for post-run validation failed campaign=%s: %v", campaignID, err)
		} else {
			item = campaign.NormalizeCampaign(item)
			if event, ok, err := validateCampaignRepoTaskCompletion(item, task); err != nil {
				logging.Warnf("campaign repo post-run validation failed campaign=%s state_key=%s: %v", campaignID, task.Action.StateKey, err)
			} else if ok {
				b.sendCampaignNotifications(item, []campaignrepo.ReconcileEvent{event})
			}
		}
	}
	b.reconcileCampaignRepoLocked(campaignID)
}

func (b *connectorRuntimeBuilder) runCampaignRepoReconcileCampaign(campaignID string) {
	if b == nil || b.campaignStore == nil {
		return
	}
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return
	}
	b.campaignRepoMu.Lock()
	defer b.campaignRepoMu.Unlock()
	b.reconcileCampaignRepoLocked(campaignID)
}

// reconcileCampaignRepoLocked runs a single-campaign reconcile assuming campaignRepoMu is held.
func (b *connectorRuntimeBuilder) reconcileCampaignRepoLocked(campaignID string) {
	item, err := b.campaignStore.GetCampaign(campaignID)
	if err != nil {
		logging.Warnf("load campaign for event-driven reconcile failed campaign=%s: %v", campaignID, err)
		return
	}
	b.reconcileCampaignRepo(item, time.Now().Local())
}

func (b *connectorRuntimeBuilder) reconcileCampaignRepo(item campaign.Campaign, now time.Time) {
	item = campaign.NormalizeCampaign(item)
	if !shouldAutoReconcileCampaign(item) {
		return
	}
	previousBlockedReasons := loadPreviousBlockedReasons(item.CampaignRepoPath)
	result, err := campaignrepo.ReconcileAndPrepare(item.CampaignRepoPath, now, item.MaxParallelTrials, campaignRepoDispatchLease, b.campaignRoleDefaults())
	if err != nil {
		logging.Warnf("campaign repo reconcile failed campaign=%s path=%s: %v", item.ID, item.CampaignRepoPath, err)
		return
	}
	events := append([]campaignrepo.ReconcileEvent(nil), result.Events...)
	events = append(events, newSummaryBlockedEvents(item.ID, previousBlockedReasons, result.Summary)...)
	if len(events) > 0 {
		b.sendCampaignNotifications(item, events)
	}
	if err := b.syncCampaignDispatchTasks(item, result.DispatchTasks, now); err != nil {
		logging.Warnf("sync dispatch tasks failed campaign=%s: %v", item.ID, err)
	}
	if _, err := campaignrepo.WriteLiveReport(item.CampaignRepoPath, result.Summary); err != nil {
		logging.Warnf("write live report failed campaign=%s path=%s: %v", item.ID, item.CampaignRepoPath, err)
	}
	if err := b.syncCampaignWakeTasks(item, result.Summary); err != nil {
		logging.Warnf("sync wake tasks failed campaign=%s: %v", item.ID, err)
	}
	if err := b.updateCampaignRepoSummary(item, result.Summary); err != nil {
		logging.Warnf("patch campaign summary failed campaign=%s: %v", item.ID, err)
	}
}

type campaignAutomationTaskState struct {
	target          automationTaskTarget
	existingByState map[string]automation.Task
}

func shouldAutoReconcileCampaign(item campaign.Campaign) bool {
	if strings.TrimSpace(item.CampaignRepoPath) == "" {
		return false
	}
	switch item.Status {
	case campaign.StatusMerged, campaign.StatusRejected, campaign.StatusCompleted, campaign.StatusCanceled:
		return false
	default:
		return true
	}
}

func (b *connectorRuntimeBuilder) updateCampaignRepoSummary(item campaign.Campaign, summary campaignrepo.Summary) error {
	nextSummary := summary.SummaryLine()
	if strings.TrimSpace(item.Summary) == nextSummary {
		return nil
	}
	_, err := b.campaignStore.PatchCampaign(item.ID, func(current *campaign.Campaign) error {
		current.Summary = nextSummary
		return nil
	})
	return err
}

func (b *connectorRuntimeBuilder) loadCampaignAutomationTaskState(item campaign.Campaign, prefix string, limit int) (campaignAutomationTaskState, bool, error) {
	target, ok := automationTargetFromCampaign(item)
	if !ok {
		return campaignAutomationTaskState{}, false, nil
	}
	existing, err := b.automationStore.ListTasks(target.Scope, "all", limit)
	if err != nil {
		return campaignAutomationTaskState{}, false, err
	}
	existingByState := make(map[string]automation.Task, len(existing))
	for _, task := range existing {
		stateKey := strings.TrimSpace(task.Action.StateKey)
		if !strings.HasPrefix(stateKey, prefix) {
			continue
		}
		existingByState[stateKey] = task
	}
	return campaignAutomationTaskState{
		target:          target,
		existingByState: existingByState,
	}, true, nil
}

func (b *connectorRuntimeBuilder) deleteStaleCampaignAutomationTasks(existingByState map[string]automation.Task, desired map[string]struct{}) error {
	for stateKey, task := range existingByState {
		if _, ok := desired[stateKey]; ok {
			continue
		}
		if task.Status == automation.TaskStatusDeleted {
			continue
		}
		if _, err := b.automationStore.PatchTask(task.ID, func(current *automation.Task) error {
			current.Status = automation.TaskStatusDeleted
			current.NextRunAt = time.Time{}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

func (b *connectorRuntimeBuilder) syncCampaignDispatchTasks(item campaign.Campaign, specs []campaignrepo.DispatchTaskSpec, now time.Time) error {
	state, ok, err := b.loadCampaignAutomationTaskState(item, campaignDispatchStatePrefix+item.ID+":", 400)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	desired := make(map[string]struct{}, len(specs))
	for _, spec := range specs {
		if spec.StateKey == "" {
			continue
		}
		desired[spec.StateKey] = struct{}{}
		task, ok := state.existingByState[spec.StateKey]
		if ok {
			if shouldKeepExistingDispatchTask(task, spec, now) {
				continue
			}
			if err := b.upsertDispatchTask(task, state.target, spec); err != nil {
				return err
			}
			continue
		}
		if _, err := b.automationStore.CreateTask(buildDispatchAutomationTask(state.target, spec)); err != nil {
			return err
		}
	}

	return b.deleteStaleCampaignAutomationTasks(state.existingByState, desired)
}

func (b *connectorRuntimeBuilder) syncCampaignWakeTasks(item campaign.Campaign, summary campaignrepo.Summary) error {
	state, ok, err := b.loadCampaignAutomationTaskState(item, campaignWakeStatePrefix+item.ID+":", 200)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	desired := make(map[string]struct{}, len(summary.WakeTasks))
	for _, spec := range summary.WakeTasks {
		if spec.StateKey == "" || spec.RunAt.IsZero() {
			continue
		}
		desired[spec.StateKey] = struct{}{}
		task, ok := state.existingByState[spec.StateKey]
		if ok {
			if shouldKeepExistingWakeTask(task, spec) {
				continue
			}
			if err := b.upsertWakeTask(task, state.target, spec); err != nil {
				return err
			}
			continue
		}
		if _, err := b.automationStore.CreateTask(buildWakeAutomationTask(state.target, spec)); err != nil {
			return err
		}
	}

	return b.deleteStaleCampaignAutomationTasks(state.existingByState, desired)
}

func shouldKeepExistingDispatchTask(task automation.Task, spec campaignrepo.DispatchTaskSpec, now time.Time) bool {
	task = automation.NormalizeTask(task)
	if strings.TrimSpace(task.Action.Prompt) != strings.TrimSpace(spec.Prompt) {
		return false
	}
	if strings.TrimSpace(task.Title) != strings.TrimSpace(spec.Title) {
		return false
	}
	if strings.TrimSpace(task.Action.Provider) != strings.TrimSpace(spec.Role.Provider) {
		return false
	}
	if strings.TrimSpace(task.Action.Model) != strings.TrimSpace(spec.Role.Model) {
		return false
	}
	if strings.TrimSpace(task.Action.Profile) != strings.TrimSpace(spec.Role.Profile) {
		return false
	}
	if strings.TrimSpace(task.Action.Workflow) != strings.TrimSpace(spec.Role.Workflow) {
		return false
	}
	if strings.TrimSpace(task.Action.ReasoningEffort) != strings.TrimSpace(spec.Role.ReasoningEffort) {
		return false
	}
	if strings.TrimSpace(task.Action.Personality) != strings.TrimSpace(spec.Role.Personality) {
		return false
	}
	if task.Status == automation.TaskStatusActive {
		return true
	}
	return keepFailedDispatchTaskCooling(task, now)
}

func keepFailedDispatchTaskCooling(task automation.Task, now time.Time) bool {
	task = automation.NormalizeTask(task)
	if task.Status != automation.TaskStatusPaused {
		return false
	}
	if !strings.HasPrefix(strings.TrimSpace(task.LastResult), "error:") {
		return false
	}
	if task.UpdatedAt.IsZero() {
		return false
	}
	if now.IsZero() {
		now = time.Now().Local()
	}
	return task.UpdatedAt.Add(campaignDispatchFailureCooldown).After(now.Local())
}

func shouldKeepExistingWakeTask(task automation.Task, spec campaignrepo.WakeTaskSpec) bool {
	task = automation.NormalizeTask(task)
	if strings.TrimSpace(task.Action.Prompt) != strings.TrimSpace(spec.Prompt) {
		return false
	}
	if strings.TrimSpace(task.Title) != strings.TrimSpace(spec.Title) {
		return false
	}
	if !task.NextRunAt.IsZero() && !task.NextRunAt.Equal(spec.RunAt) {
		return false
	}
	return task.Status == automation.TaskStatusActive
}

func (b *connectorRuntimeBuilder) upsertDispatchTask(task automation.Task, target automationTaskTarget, spec campaignrepo.DispatchTaskSpec) error {
	_, err := b.automationStore.PatchTask(task.ID, func(current *automation.Task) error {
		current.Title = spec.Title
		current.Scope = target.Scope
		current.Route = target.Route
		current.Creator = target.Creator
		current.ManageMode = target.ManageMode
		current.Schedule = automation.Schedule{Type: automation.ScheduleTypeInterval, EverySeconds: campaignRepoTaskEverySeconds}
		current.Action = automation.Action{
			Type:            automation.ActionTypeRunWorkflow,
			Text:            fmt.Sprintf("Scheduled %s for `%s`", spec.Kind, spec.TaskID),
			Prompt:          spec.Prompt,
			Provider:        spec.Role.Provider,
			Model:           spec.Role.Model,
			Profile:         spec.Role.Profile,
			Workflow:        spec.Role.Workflow,
			StateKey:        spec.StateKey,
			SessionKey:      target.SessionKey,
			ReasoningEffort: spec.Role.ReasoningEffort,
			Personality:     spec.Role.Personality,
		}
		current.MaxRuns = 1
		current.RunCount = 0
		current.LastRunAt = time.Time{}
		current.LastResult = ""
		current.LastSignalKind = ""
		current.LastSignalMessage = ""
		current.ConsecutiveFailures = 0
		current.Running = false
		current.DeletedAt = time.Time{}
		current.Status = automation.TaskStatusActive
		current.NextRunAt = spec.RunAt
		return nil
	})
	return err
}

func (b *connectorRuntimeBuilder) upsertWakeTask(task automation.Task, target automationTaskTarget, spec campaignrepo.WakeTaskSpec) error {
	_, err := b.automationStore.PatchTask(task.ID, func(current *automation.Task) error {
		current.Title = spec.Title
		current.Scope = target.Scope
		current.Route = target.Route
		current.Creator = target.Creator
		current.ManageMode = target.ManageMode
		current.Schedule = automation.Schedule{Type: automation.ScheduleTypeInterval, EverySeconds: campaignRepoTaskEverySeconds}
		current.Action = automation.Action{
			Type:       automation.ActionTypeRunWorkflow,
			Text:       fmt.Sprintf("Scheduled wake for `%s`", spec.TaskID),
			Prompt:     spec.Prompt,
			Workflow:   "code_army",
			StateKey:   spec.StateKey,
			SessionKey: target.SessionKey,
		}
		current.MaxRuns = 1
		current.RunCount = 0
		current.LastRunAt = time.Time{}
		current.LastResult = ""
		current.LastSignalKind = ""
		current.LastSignalMessage = ""
		current.ConsecutiveFailures = 0
		current.Running = false
		current.DeletedAt = time.Time{}
		current.Status = automation.TaskStatusActive
		current.NextRunAt = spec.RunAt
		return nil
	})
	return err
}

type automationTaskTarget struct {
	Scope      automation.Scope
	Route      automation.Route
	Creator    automation.Actor
	ManageMode automation.ManageMode
	SessionKey string
}

func automationTargetFromCampaign(item campaign.Campaign) (automationTaskTarget, bool) {
	item = campaign.NormalizeCampaign(item)
	target := automationTaskTarget{
		Creator: automation.Actor{
			UserID: item.Creator.UserID,
			OpenID: item.Creator.OpenID,
			Name:   item.Creator.Name,
		},
		SessionKey: item.Session.ScopeKey,
	}
	if item.ManageMode == campaign.ManageModeScopeAll {
		target.ManageMode = automation.ManageModeScopeAll
	} else {
		target.ManageMode = automation.ManageModeCreatorOnly
	}

	chatType := strings.ToLower(strings.TrimSpace(item.Session.ChatType))
	receiveType := strings.TrimSpace(item.Session.ReceiveIDType)
	receiveID := strings.TrimSpace(item.Session.ReceiveID)
	if receiveType == "chat_id" || chatType == "group" || chatType == "topic_group" {
		if receiveID == "" {
			return automationTaskTarget{}, false
		}
		target.Scope = automation.Scope{Kind: automation.ScopeKindChat, ID: receiveID}
		target.Route = automation.Route{ReceiveIDType: "chat_id", ReceiveID: receiveID}
		return target, true
	}

	preferredID := item.Creator.PreferredID()
	if preferredID == "" {
		return automationTaskTarget{}, false
	}
	target.Scope = automation.Scope{Kind: automation.ScopeKindUser, ID: preferredID}
	switch {
	case item.Creator.UserID != "":
		target.Route = automation.Route{ReceiveIDType: "user_id", ReceiveID: item.Creator.UserID}
	case item.Creator.OpenID != "":
		target.Route = automation.Route{ReceiveIDType: "open_id", ReceiveID: item.Creator.OpenID}
	case receiveType != "" && receiveID != "":
		target.Route = automation.Route{ReceiveIDType: receiveType, ReceiveID: receiveID}
	default:
		return automationTaskTarget{}, false
	}
	return target, true
}

func buildWakeAutomationTask(target automationTaskTarget, spec campaignrepo.WakeTaskSpec) automation.Task {
	return automation.Task{
		Title:      spec.Title,
		Scope:      target.Scope,
		Route:      target.Route,
		Creator:    target.Creator,
		ManageMode: target.ManageMode,
		Schedule: automation.Schedule{
			Type:         automation.ScheduleTypeInterval,
			EverySeconds: campaignRepoTaskEverySeconds,
		},
		Action: automation.Action{
			Type:       automation.ActionTypeRunWorkflow,
			Text:       fmt.Sprintf("Scheduled wake for `%s`", spec.TaskID),
			Prompt:     spec.Prompt,
			Workflow:   "code_army",
			StateKey:   spec.StateKey,
			SessionKey: target.SessionKey,
		},
		Status:    automation.TaskStatusActive,
		MaxRuns:   1,
		NextRunAt: spec.RunAt,
	}
}

func buildDispatchAutomationTask(target automationTaskTarget, spec campaignrepo.DispatchTaskSpec) automation.Task {
	return automation.Task{
		Title:      spec.Title,
		Scope:      target.Scope,
		Route:      target.Route,
		Creator:    target.Creator,
		ManageMode: target.ManageMode,
		Schedule: automation.Schedule{
			Type:         automation.ScheduleTypeInterval,
			EverySeconds: campaignRepoTaskEverySeconds,
		},
		Action: automation.Action{
			Type:            automation.ActionTypeRunWorkflow,
			Text:            fmt.Sprintf("Scheduled %s for `%s`", spec.Kind, spec.TaskID),
			Prompt:          spec.Prompt,
			Provider:        spec.Role.Provider,
			Model:           spec.Role.Model,
			Profile:         spec.Role.Profile,
			Workflow:        spec.Role.Workflow,
			StateKey:        spec.StateKey,
			SessionKey:      target.SessionKey,
			ReasoningEffort: spec.Role.ReasoningEffort,
			Personality:     spec.Role.Personality,
		},
		Status:    automation.TaskStatusActive,
		MaxRuns:   1,
		NextRunAt: spec.RunAt,
	}
}

func campaignIDFromAutomationStateKey(stateKey string) (string, bool) {
	stateKey = strings.TrimSpace(stateKey)
	if stateKey == "" {
		return "", false
	}
	parts := strings.Split(stateKey, ":")
	if len(parts) < 2 {
		return "", false
	}
	switch parts[0] {
	case strings.TrimSuffix(campaignDispatchStatePrefix, ":"), strings.TrimSuffix(campaignWakeStatePrefix, ":"):
	default:
		return "", false
	}
	campaignID := strings.TrimSpace(parts[1])
	if campaignID == "" || campaignID == "unknown" {
		return "", false
	}
	return campaignID, true
}

func dispatchKindAndTaskIDFromStateKey(stateKey string) (campaignrepo.DispatchKind, string, bool) {
	stateKey = strings.TrimSpace(stateKey)
	if stateKey == "" {
		return "", "", false
	}
	parts := strings.Split(stateKey, ":")
	if len(parts) < 5 || parts[0] != strings.TrimSuffix(campaignDispatchStatePrefix, ":") {
		return "", "", false
	}
	kind := campaignrepo.DispatchKind(strings.TrimSpace(parts[2]))
	taskID := strings.TrimSpace(parts[3])
	if taskID == "" {
		return "", "", false
	}
	switch kind {
	case campaignrepo.DispatchKindExecutor, campaignrepo.DispatchKindReviewer:
		return kind, taskID, true
	default:
		return "", "", false
	}
}

func shouldSkipPostRunValidationForSignal(task automation.Task) bool {
	signalKind := strings.ToLower(strings.TrimSpace(task.LastSignalKind))
	switch signalKind {
	case "blocked", "replan":
		return true
	default:
		return false
	}
}

func validateCampaignRepoTaskCompletion(item campaign.Campaign, task automation.Task) (campaignrepo.ReconcileEvent, bool, error) {
	task = automation.NormalizeTask(task)
	if strings.TrimSpace(item.CampaignRepoPath) == "" || shouldSkipPostRunValidationForSignal(task) {
		return campaignrepo.ReconcileEvent{}, false, nil
	}
	kind, taskID, ok := dispatchKindAndTaskIDFromStateKey(task.Action.StateKey)
	if !ok {
		return campaignrepo.ReconcileEvent{}, false, nil
	}
	validation, err := campaignrepo.ValidateTaskPostRun(item.CampaignRepoPath, taskID, kind)
	if err != nil || validation.Valid {
		return campaignrepo.ReconcileEvent{}, false, err
	}
	reason := summarizeValidationIssues(validation.Issues, 3)
	switch kind {
	case campaignrepo.DispatchKindExecutor:
		outcome, err := campaignrepo.HandleTaskBlocked(item.CampaignRepoPath, taskID, "post-run validation failed after executor round: "+reason)
		if err != nil {
			return campaignrepo.ReconcileEvent{}, false, err
		}
		title := "执行收尾校验失败"
		detail := fmt.Sprintf("任务 **%s** executor 回合结束后未通过状态校验，已停止继续派发。\n\n**问题**:\n%s", taskID, reason)
		if outcome.GuidanceRequested {
			title = "执行收尾校验失败，转评审指导"
			detail = fmt.Sprintf("任务 **%s** executor 回合结束后未通过状态校验，已转 reviewer 指导（第 %d/%d 次）。\n\n**问题**:\n%s", taskID, outcome.GuidanceAttempt, 3, reason)
		} else if outcome.TerminalBlocked {
			title = "执行收尾校验失败，任务阻塞"
			detail = fmt.Sprintf("任务 **%s** executor 回合结束后未通过状态校验，且指导预算已耗尽，已进入真正阻塞状态。\n\n**问题**:\n%s", taskID, reason)
		}
		return campaignrepo.ReconcileEvent{
			Kind:       campaignrepo.EventTaskBlocked,
			CampaignID: item.ID,
			TaskID:     taskID,
			Title:      title,
			Detail:     detail,
			Severity:   "warning",
		}, true, nil
	case campaignrepo.DispatchKindReviewer:
		if err := campaignrepo.MarkTaskBlocked(item.CampaignRepoPath, taskID, "post-run validation failed after reviewer round: "+reason); err != nil {
			return campaignrepo.ReconcileEvent{}, false, err
		}
		return campaignrepo.ReconcileEvent{
			Kind:       campaignrepo.EventTaskBlocked,
			CampaignID: item.ID,
			TaskID:     taskID,
			Title:      "评审收尾校验失败，任务阻塞",
			Detail:     fmt.Sprintf("任务 **%s** reviewer 回合结束后未通过状态校验，已阻止继续推进。\n\n**问题**:\n%s", taskID, reason),
			Severity:   "error",
		}, true, nil
	default:
		return campaignrepo.ReconcileEvent{}, false, nil
	}
}

func summarizeValidationIssues(issues []campaignrepo.ValidationIssue, limit int) string {
	if len(issues) == 0 {
		return "- unknown validation failure"
	}
	if limit <= 0 || limit > len(issues) {
		limit = len(issues)
	}
	lines := make([]string, 0, limit+1)
	for _, issue := range issues[:limit] {
		lines = append(lines, "- "+strings.TrimSpace(issue.Message))
	}
	if extra := len(issues) - limit; extra > 0 {
		lines = append(lines, fmt.Sprintf("- 另外还有 %d 条校验问题", extra))
	}
	return strings.Join(lines, "\n")
}

func (b *connectorRuntimeBuilder) campaignRoleDefaults() campaignrepo.CampaignRoleDefaults {
	if b == nil {
		return campaignrepo.CampaignRoleDefaults{}
	}
	return CampaignRoleDefaultsFromConfig(b.cfg)
}

func CampaignRoleDefaultsFromConfig(cfg config.Config) campaignrepo.CampaignRoleDefaults {
	return campaignrepo.CampaignRoleDefaults{
		Executor:        configRoleToRepoRole(cfg, cfg.CampaignRoleDefaults.Executor, "executor"),
		Reviewer:        configRoleToRepoRole(cfg, cfg.CampaignRoleDefaults.Reviewer, "reviewer"),
		Planner:         configRoleToRepoRole(cfg, cfg.CampaignRoleDefaults.Planner, "planner"),
		PlannerReviewer: configRoleToRepoRole(cfg, cfg.CampaignRoleDefaults.PlannerReviewer, "planner_reviewer"),
	}
}

func configRoleToRepoRole(cfg config.Config, c config.CampaignRoleDefaultConfig, fallbackRole string) campaignrepo.RoleConfig {
	role := strings.TrimSpace(c.Role)
	if role == "" {
		role = fallbackRole
	}
	workflow := strings.ToLower(strings.TrimSpace(c.Workflow))
	if workflow == "" {
		workflow = "code_army"
	}

	resolved := campaignrepo.RoleConfig{
		Role:     role,
		Workflow: workflow,
	}

	profileName := strings.ToLower(strings.TrimSpace(c.LLMProfile))
	if profileName == "" {
		return resolved
	}
	resolved.Profile = profileName

	profile, ok := cfg.LLMProfiles[profileName]
	if !ok {
		return resolved
	}
	resolved.Provider = strings.ToLower(strings.TrimSpace(profile.Provider))
	if resolved.Provider == "" {
		resolved.Provider = strings.ToLower(strings.TrimSpace(cfg.LLMProvider))
		if resolved.Provider == "" {
			resolved.Provider = config.DefaultLLMProvider
		}
	}
	resolved.Model = strings.TrimSpace(profile.Model)
	resolved.ReasoningEffort = strings.ToLower(strings.TrimSpace(profile.ReasoningEffort))
	resolved.Personality = strings.ToLower(strings.TrimSpace(profile.Personality))
	return resolved
}
