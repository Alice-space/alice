package runtimeapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	jsonpatch "github.com/evanphx/json-patch/v5"

	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/sessionctx"
	"github.com/Alice-space/alice/internal/sessionkey"
)

type automationScopeContext struct {
	scope   automation.Scope
	route   automation.Route
	creator automation.Actor
	actorID string
	isGroup bool
	session sessionctx.SessionContext
}

func resolveAutomationScope(session sessionctx.SessionContext) (automationScopeContext, error) {
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
		scopeID := strings.TrimSpace(session.SessionKey)
		if scopeID != "" {
			scopeID = sessionkey.WithoutMessage(scopeID)
		}
		if scopeID == "" {
			scopeID = runtimeCtx.receiveID
		}
		receiveID := runtimeCtx.receiveID
		if receiveID == "" {
			return automationScopeContext{}, errors.New("missing chat_id for group automation scope")
		}
		ctx.scope = automation.Scope{Kind: automation.ScopeKindChat, ID: scopeID}
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
		Title:   strings.TrimSpace(req.Title),
		Prompt:  strings.TrimSpace(req.Prompt),
		Fresh:   req.Fresh,
		MaxRuns: req.MaxRuns,
		Schedule: automation.Schedule{
			EverySeconds: req.EverySeconds,
			CronExpr:     strings.TrimSpace(req.CronExpr),
		},
		Scope:          scopeCtx.scope,
		Route:          scopeCtx.route,
		Creator:        scopeCtx.creator,
		ManageMode:     req.ManageMode,
		ResumeThreadID: strings.TrimSpace(req.ResumeThreadID),
		NextRunAt:      req.NextRunAt,
		Status:         automation.TaskStatusActive,
	}

	if task.ManageMode == "" {
		task.ManageMode = automation.ManageModeCreatorOnly
	}

	if task.Schedule.CronExpr != "" {
		task.Schedule.EverySeconds = 0
	}
	if task.Schedule.EverySeconds > 0 && task.Schedule.EverySeconds < 60 {
		return automation.Task{}, errors.New("every_seconds must be >= 60")
	}

	task.SessionKey = scopeSessionKey(scopeCtx.session)

	if sourceMsgID := strings.TrimSpace(scopeCtx.session.SourceMessageID); sourceMsgID != "" {
		task.SessionKey = task.SessionKey + sessionkey.MessageToken + sourceMsgID
		task.Route = automation.Route{ReceiveIDType: "source_message_id", ReceiveID: sourceMsgID}
	}

	if req.Enabled != nil && !*req.Enabled {
		task.Status = automation.TaskStatusPaused
	}

	return automation.NormalizeTask(task), nil
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
	next.SessionKey = current.SessionKey
	next.ResumeThreadID = current.ResumeThreadID
	next.SourceMessageID = current.SourceMessageID
	next.RunCount = current.RunCount
	next.LastRunAt = current.LastRunAt
	next.LastResult = current.LastResult
	next.ConsecutiveFailures = current.ConsecutiveFailures
	next.Running = current.Running
	next.Revision = current.Revision

	return next, nil
}

func canManageTask(task automation.Task, actorID string) bool {
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return false
	}
	return actorID == task.Creator.PreferredID() || task.ManageMode == automation.ManageModeScopeAll
}
