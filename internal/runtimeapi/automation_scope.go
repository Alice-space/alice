package runtimeapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	jsonpatch "github.com/evanphx/json-patch/v5"

	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/config"
	"github.com/Alice-space/alice/internal/mcpbridge"
	"github.com/Alice-space/alice/internal/runtimecfg"
)

type automationScopeContext struct {
	scope   automation.Scope
	route   automation.Route
	creator automation.Actor
	actorID string
	isGroup bool
	session mcpbridge.SessionContext
}

func resolveAutomationScope(session mcpbridge.SessionContext) (automationScopeContext, error) {
	runtimeCtx, err := resolveRuntimeSessionContext(session)
	if err != nil {
		return automationScopeContext{}, err
	}
	ctx := automationScopeContext{
		creator: automation.Actor{
			UserID: runtimeCtx.actorUserID,
			OpenID: runtimeCtx.actorOpenID,
		},
		actorID: runtimeCtx.actorID,
		isGroup: runtimeCtx.isGroup,
		session: session,
	}
	if runtimeCtx.isGroup {
		receiveID := runtimeCtx.receiveID
		if receiveID == "" {
			return automationScopeContext{}, errors.New("missing chat_id for group automation scope")
		}
		ctx.scope = automation.Scope{Kind: automation.ScopeKindChat, ID: receiveID}
		ctx.route = automation.Route{ReceiveIDType: "chat_id", ReceiveID: receiveID}
		return ctx, nil
	}
	ctx.scope = automation.Scope{Kind: automation.ScopeKindUser, ID: runtimeCtx.actorID}
	if runtimeCtx.actorUserID != "" {
		ctx.route = automation.Route{ReceiveIDType: "user_id", ReceiveID: runtimeCtx.actorUserID}
	} else if runtimeCtx.actorOpenID != "" {
		ctx.route = automation.Route{ReceiveIDType: "open_id", ReceiveID: runtimeCtx.actorOpenID}
	} else {
		ctx.route = automation.Route{
			ReceiveIDType: runtimeCtx.receiveIDType,
			ReceiveID:     runtimeCtx.receiveID,
		}
	}
	return ctx, nil
}

func (s *Server) buildTaskFromRequest(req CreateTaskRequest, scopeCtx automationScopeContext) (automation.Task, error) {
	task := automation.Task{
		Title:      strings.TrimSpace(req.Title),
		Scope:      scopeCtx.scope,
		Route:      scopeCtx.route,
		Creator:    scopeCtx.creator,
		ManageMode: req.ManageMode,
		Schedule:   req.Schedule,
		Action:     req.Action,
		MaxRuns:    req.MaxRuns,
		Status:     automation.TaskStatusActive,
	}
	if task.ManageMode == "" {
		task.ManageMode = automation.ManageModeCreatorOnly
	}
	switch task.Schedule.Type {
	case "":
		if strings.TrimSpace(task.Schedule.CronExpr) != "" {
			task.Schedule.Type = automation.ScheduleTypeCron
		} else {
			task.Schedule.Type = automation.ScheduleTypeInterval
		}
	}
	if task.Schedule.Type == automation.ScheduleTypeInterval && task.Schedule.EverySeconds < 60 {
		return automation.Task{}, errors.New("every_seconds must be >= 60 for interval schedule")
	}
	if task.Schedule.Type == automation.ScheduleTypeCron && strings.TrimSpace(task.Schedule.CronExpr) == "" {
		return automation.Task{}, errors.New("cron_expr is required for cron schedule")
	}
	if task.Action.Type == "" {
		switch {
		case strings.TrimSpace(task.Action.Workflow) != "":
			task.Action.Type = automation.ActionTypeRunWorkflow
		case strings.TrimSpace(task.Action.Prompt) != "":
			task.Action.Type = automation.ActionTypeRunLLM
		default:
			task.Action.Type = automation.ActionTypeSendText
		}
	}
	applySceneLLMProfileDefaults(&task, scopeCtx, s.runtimeConfig())
	task.Action.SessionKey = scopeSessionKey(scopeCtx.session)
	if err := validateMentionPermission(scopeCtx, task.Action.MentionUserIDs); err != nil {
		return automation.Task{}, err
	}
	if req.Enabled != nil && !*req.Enabled {
		task.Status = automation.TaskStatusPaused
	}
	return automation.NormalizeTask(task), nil
}

func applySceneLLMProfileDefaults(task *automation.Task, scopeCtx automationScopeContext, runtime automationRuntimeConfig) {
	if task == nil {
		return
	}
	if task.Action.Type != automation.ActionTypeRunLLM && task.Action.Type != automation.ActionTypeRunWorkflow {
		return
	}
	profile, ok := resolveSceneLLMProfile(runtime, scopeCtx.session.SessionKey)
	if !ok {
		return
	}
	if strings.TrimSpace(task.Action.Model) == "" {
		task.Action.Model = strings.TrimSpace(profile.Model)
	}
	if strings.TrimSpace(task.Action.Provider) == "" {
		task.Action.Provider = strings.TrimSpace(profile.Provider)
		if task.Action.Provider == "" {
			task.Action.Provider = strings.TrimSpace(runtime.llmProvider)
		}
	}
	if strings.TrimSpace(task.Action.Profile) == "" {
		task.Action.Profile = strings.TrimSpace(profile.Profile)
	}
	if strings.TrimSpace(task.Action.ReasoningEffort) == "" {
		task.Action.ReasoningEffort = strings.TrimSpace(profile.ReasoningEffort)
	}
	if strings.TrimSpace(task.Action.Personality) == "" {
		task.Action.Personality = strings.TrimSpace(profile.Personality)
	}
}

func resolveSceneLLMProfile(runtime automationRuntimeConfig, sessionKey string) (config.LLMProfileConfig, bool) {
	return runtimecfg.ResolveSceneLLMProfile(runtime.llmProfiles, runtime.groupScenes, sessionKey)
}

func applyTaskPatch(current automation.Task, patchBytes []byte, contentType string, scopeCtx automationScopeContext) (automation.Task, error) {
	current = automation.NormalizeTask(current)
	currentJSON, err := json.Marshal(current)
	if err != nil {
		return automation.Task{}, err
	}

	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	patchedJSON := patchBytes
	switch contentType {
	case "application/json-patch+json":
		patch, err := jsonpatch.DecodePatch(patchBytes)
		if err != nil {
			return automation.Task{}, err
		}
		patchedJSON, err = patch.Apply(currentJSON)
		if err != nil {
			return automation.Task{}, err
		}
	case "application/merge-patch+json", "application/json", "":
		patchedJSON, err = jsonpatch.MergePatch(currentJSON, patchBytes)
		if err != nil {
			return automation.Task{}, err
		}
	default:
		return automation.Task{}, fmt.Errorf("unsupported patch content type %q", contentType)
	}

	var next automation.Task
	if err := json.Unmarshal(patchedJSON, &next); err != nil {
		return automation.Task{}, err
	}
	next = automation.NormalizeTask(next)
	next.ID = current.ID
	next.Scope = current.Scope
	next.Route = current.Route
	next.Creator = current.Creator
	next.CreatedAt = current.CreatedAt
	next.RunCount = current.RunCount
	next.LastRunAt = current.LastRunAt
	next.LastResult = current.LastResult
	next.ConsecutiveFailures = current.ConsecutiveFailures
	next.Running = current.Running
	next.Revision = current.Revision
	next.Action.SessionKey = scopeSessionKey(scopeCtx.session)
	if err := validateMentionPermission(scopeCtx, next.Action.MentionUserIDs); err != nil {
		return automation.Task{}, err
	}
	return next, nil
}

func validateMentionPermission(scopeCtx automationScopeContext, mentionUserIDs []string) error {
	if scopeCtx.isGroup {
		return nil
	}
	for _, mentionID := range mentionUserIDs {
		if strings.TrimSpace(mentionID) != strings.TrimSpace(scopeCtx.actorID) {
			return errors.New("private scope only allows mention current actor")
		}
	}
	return nil
}

func canManageTask(task automation.Task, actorID string) bool {
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return false
	}
	return actorID == task.Creator.PreferredID() || task.ManageMode == automation.ManageModeScopeAll
}
