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
	"github.com/Alice-space/alice/internal/sessionkey"
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
		NextRunAt:  req.NextRunAt,
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
	if resumeKey := strings.TrimSpace(req.ResumeSessionKey); resumeKey != "" {
		resumeRoute, resumeScope, err := routeAndScopeFromSessionKey(resumeKey)
		if err != nil {
			return automation.Task{}, fmt.Errorf("invalid resume_session_key: %w", err)
		}
		if err := validateResumeScope(resumeScope, scopeCtx); err != nil {
			return automation.Task{}, fmt.Errorf("resume_session_key out of scope: %w", err)
		}
		task.Route = resumeRoute
		task.Action.SessionKey = sessionkey.WithoutMessage(resumeKey)
	}
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
	if profileName := strings.ToLower(strings.TrimSpace(task.Action.Profile)); profileName != "" {
		profile, ok := runtime.llmProfiles[profileName]
		if !ok {
			return
		}
		applyAutomationTaskLLMProfile(task, profileName, profile, runtime.llmProvider)
		return
	}
	if strings.TrimSpace(task.Action.Provider) != "" {
		return
	}
	selection, ok := resolveSceneLLMProfile(runtime, scopeCtx.session.SessionKey)
	if !ok {
		return
	}
	applyAutomationTaskLLMProfile(task, selection.Name, selection.Profile, runtime.llmProvider)
}

func applyAutomationTaskLLMProfile(task *automation.Task, profileName string, profile config.LLMProfileConfig, runtimeProvider string) {
	if task == nil {
		return
	}
	if strings.TrimSpace(task.Action.Model) == "" {
		task.Action.Model = strings.TrimSpace(profile.Model)
	}
	if strings.TrimSpace(task.Action.Provider) == "" {
		task.Action.Provider = strings.TrimSpace(profile.Provider)
		if task.Action.Provider == "" {
			task.Action.Provider = strings.TrimSpace(runtimeProvider)
		}
	}
	if strings.TrimSpace(task.Action.Profile) == "" {
		task.Action.Profile = strings.TrimSpace(profileName)
	}
	if strings.TrimSpace(task.Action.ReasoningEffort) == "" {
		task.Action.ReasoningEffort = strings.TrimSpace(profile.ReasoningEffort)
	}
	if strings.TrimSpace(task.Action.Personality) == "" {
		task.Action.Personality = strings.TrimSpace(profile.Personality)
	}
	if strings.TrimSpace(task.Action.PromptPrefix) == "" {
		task.Action.PromptPrefix = strings.TrimSpace(profile.PromptPrefix)
	}
}

func resolveSceneLLMProfile(runtime automationRuntimeConfig, sessionKey string) (runtimecfg.SceneLLMProfileSelection, bool) {
	return runtimecfg.ResolveSceneLLMProfileSelection(runtime.llmProfiles, runtime.groupScenes, sessionKey)
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
	// If the task's route matches the caller's current scope route, this is a
	// normal (non-resume) task and we refresh the session key from the caller's
	// current session as before.
	// If the routes differ the task was created with resume_session_key (e.g.
	// the route is thread_id:omt_xxx while the caller's scope is chat_id:oc_xxx).
	// In that case the stored session key must be preserved so that env/scene
	// stay consistent with the delivery route.
	if next.Route == scopeCtx.route {
		next.Action.SessionKey = scopeSessionKey(scopeCtx.session)
	} else {
		next.Action.SessionKey = current.Action.SessionKey
	}
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

// routeAndScopeFromSessionKey derives the Feishu Route and automation Scope from
// a session key such as "chat_id:oc_xxx|scene:work|thread:omt_yyy".
//
// If a thread token is present the Route targets the thread directly
// (receive_id_type=thread_id), so automation messages land in the correct
// Feishu thread rather than the top-level chat.  The Scope is always anchored
// to the base channel (chat_id or user/open_id) for access-control purposes.
func routeAndScopeFromSessionKey(sk string) (automation.Route, automation.Scope, error) {
	sk = strings.TrimSpace(sk)
	if sk == "" {
		return automation.Route{}, automation.Scope{}, errors.New("session key is empty")
	}
	base := sessionkey.VisibilityKey(sk) // "chat_id:oc_xxx" or "open_id:ou_xxx"
	colonIdx := strings.Index(base, ":")
	if colonIdx <= 0 || colonIdx == len(base)-1 {
		return automation.Route{}, automation.Scope{}, fmt.Errorf("malformed session key %q: expected type:id format", sk)
	}
	receiveIDType := strings.TrimSpace(base[:colonIdx])
	receiveID := strings.TrimSpace(base[colonIdx+1:])
	if receiveIDType == "" || receiveID == "" {
		return automation.Route{}, automation.Scope{}, fmt.Errorf("malformed session key %q: empty type or id", sk)
	}

	var scope automation.Scope
	if receiveIDType == "chat_id" {
		scope = automation.Scope{Kind: automation.ScopeKindChat, ID: receiveID}
	} else {
		// user_id / open_id — scope ID is the channel ID itself; validation
		// against the creator's actorID is done by validateResumeScope.
		scope = automation.Scope{Kind: automation.ScopeKindUser, ID: receiveID}
	}

	// If the session key encodes a Feishu thread, send directly to that thread.
	if threadID := sessionkey.ExtractThreadID(sk); threadID != "" {
		route := automation.Route{ReceiveIDType: "thread_id", ReceiveID: threadID}
		return route, scope, nil
	}
	route := automation.Route{ReceiveIDType: receiveIDType, ReceiveID: receiveID}
	return route, scope, nil
}

// validateResumeScope ensures the resume session is within the caller's own
// scope so that a task cannot be redirected to an unrelated channel.
//
//   - Group scope: the resume session key must belong to the same chat_id.
//   - User scope: the resume session key must belong to the same actor.
func validateResumeScope(resumeScope automation.Scope, scopeCtx automationScopeContext) error {
	if scopeCtx.isGroup {
		// Creator is in a group; resume key must refer to the same group.
		if resumeScope.Kind != automation.ScopeKindChat {
			return errors.New("group scope can only resume a chat session key")
		}
		if resumeScope.ID != scopeCtx.scope.ID {
			return errors.New("resume session key does not belong to the current group")
		}
		return nil
	}
	// Creator is in a P2P chat; resume key must identify the same actor.
	// We compare against scopeCtx.actorID (the normalized identity preferred
	// by resolveRuntimeSessionContext: ActorUserID first, then ActorOpenID)
	// rather than the raw route.ReceiveID, because the session key and the
	// current runtime context may use different Feishu ID types (user_id vs
	// open_id) for the same person.
	if resumeScope.Kind != automation.ScopeKindUser {
		return errors.New("user scope can only resume a user (P2P) session key")
	}
	if resumeScope.ID != scopeCtx.actorID &&
		resumeScope.ID != strings.TrimSpace(scopeCtx.session.ActorUserID) &&
		resumeScope.ID != strings.TrimSpace(scopeCtx.session.ActorOpenID) {
		return errors.New("resume session key does not belong to the current user")
	}
	return nil
}
