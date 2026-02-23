package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"gitee.com/alicespace/alice/internal/automation"
	"gitee.com/alicespace/alice/internal/mcpbridge"
)

const (
	ToolAutomationTaskCreate = "automation_task_create"
	ToolAutomationTaskList   = "automation_task_list"
	ToolAutomationTaskGet    = "automation_task_get"
	ToolAutomationTaskUpdate = "automation_task_update"
	ToolAutomationTaskDelete = "automation_task_delete"
)

type automationScopeContext struct {
	scope   automation.Scope
	route   automation.Route
	creator automation.Actor
	actorID string
	isGroup bool
}

func (s *service) registerAutomationTools(mcpServer *server.MCPServer) {
	if mcpServer == nil {
		return
	}

	mcpServer.AddTool(mcp.NewTool(
		ToolAutomationTaskCreate,
		mcp.WithDescription("创建自动化任务。私聊按用户作用域隔离，群聊按群作用域隔离。"),
		mcp.WithString("title", mcp.Description("任务标题，可选")),
		mcp.WithNumber("every_seconds", mcp.Required(), mcp.Description("执行间隔秒数，最小60秒"), mcp.Min(60)),
		mcp.WithString("action_type", mcp.Description("任务动作类型：send_text 或 run_llm；默认 send_text"), mcp.Enum("send_text", "run_llm")),
		mcp.WithString("text", mcp.Description("发送文本，可选；与 mention_user_ids 至少一项非空")),
		mcp.WithString("prompt", mcp.Description("run_llm 动作的提示词；支持模板变量 {{now}}/{{date}}/{{time}}/{{unix}}")),
		mcp.WithArray("mention_user_ids", mcp.Description("要@的用户id列表，私聊仅允许@当前用户"), mcp.WithStringItems()),
		mcp.WithBoolean("enabled", mcp.Description("是否启用，默认 true")),
		mcp.WithString("manage_mode", mcp.Description("creator_only 或 scope_all（仅群聊可用）"), mcp.Enum("creator_only", "scope_all")),
	), s.handleAutomationTaskCreate)

	mcpServer.AddTool(mcp.NewTool(
		ToolAutomationTaskList,
		mcp.WithDescription("列出当前作用域的自动化任务。"),
		mcp.WithString("status", mcp.Description("all/active/paused/deleted"), mcp.Enum("all", "active", "paused", "deleted")),
		mcp.WithNumber("limit", mcp.Description("返回条数，默认20，最大200"), mcp.Min(1), mcp.Max(200)),
	), s.handleAutomationTaskList)

	mcpServer.AddTool(mcp.NewTool(
		ToolAutomationTaskGet,
		mcp.WithDescription("获取自动化任务详情。"),
		mcp.WithString("task_id", mcp.Required(), mcp.Description("任务ID")),
	), s.handleAutomationTaskGet)

	mcpServer.AddTool(mcp.NewTool(
		ToolAutomationTaskUpdate,
		mcp.WithDescription("更新自动化任务。"),
		mcp.WithString("task_id", mcp.Required(), mcp.Description("任务ID")),
		mcp.WithString("title", mcp.Description("新标题")),
		mcp.WithNumber("every_seconds", mcp.Description("新间隔秒数，最小60秒"), mcp.Min(60)),
		mcp.WithString("action_type", mcp.Description("新动作类型：send_text 或 run_llm"), mcp.Enum("send_text", "run_llm")),
		mcp.WithString("text", mcp.Description("新文本")),
		mcp.WithString("prompt", mcp.Description("run_llm 动作的新提示词；支持模板变量 {{now}}/{{date}}/{{time}}/{{unix}}")),
		mcp.WithArray("mention_user_ids", mcp.Description("新的@用户id列表"), mcp.WithStringItems()),
		mcp.WithBoolean("enabled", mcp.Description("设置启用状态")),
		mcp.WithString("status", mcp.Description("active/paused/deleted"), mcp.Enum("active", "paused", "deleted")),
		mcp.WithString("manage_mode", mcp.Description("creator_only 或 scope_all（仅群聊可用）"), mcp.Enum("creator_only", "scope_all")),
	), s.handleAutomationTaskUpdate)

	mcpServer.AddTool(mcp.NewTool(
		ToolAutomationTaskDelete,
		mcp.WithDescription("删除自动化任务（软删除，状态改为deleted）。"),
		mcp.WithString("task_id", mcp.Required(), mcp.Description("任务ID")),
	), s.handleAutomationTaskDelete)
}

func (s *service) handleAutomationTaskCreate(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.automationStore == nil {
		return mcp.NewToolResultError("automation store is unavailable"), nil
	}
	scopeCtx, err := s.resolveAutomationScope()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	everySeconds := request.GetInt("every_seconds", 0)
	if everySeconds < 60 {
		return mcp.NewToolResultError("every_seconds must be >= 60"), nil
	}
	title := strings.TrimSpace(request.GetString("title", ""))
	text := strings.TrimSpace(request.GetString("text", ""))
	prompt := strings.TrimSpace(request.GetString("prompt", ""))
	actionType, err := resolveActionType(request.GetString("action_type", ""), prompt)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	mentionUserIDs := uniqueNonEmptyStrings(request.GetStringSlice("mention_user_ids", nil))
	if err := validateMentionPermission(scopeCtx, mentionUserIDs); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	enabled := true
	if hasArgument(request, "enabled") {
		enabled = request.GetBool("enabled", true)
	}
	status := automation.TaskStatusActive
	if !enabled {
		status = automation.TaskStatusPaused
	}
	manageMode, err := parseManageMode(request.GetString("manage_mode", ""), scopeCtx.isGroup)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	created, err := s.automationStore.CreateTask(automation.Task{
		Title:      title,
		Scope:      scopeCtx.scope,
		Route:      scopeCtx.route,
		Creator:    scopeCtx.creator,
		ManageMode: manageMode,
		Schedule: automation.Schedule{
			Type:         automation.ScheduleTypeInterval,
			EverySeconds: everySeconds,
		},
		Action: automation.Action{
			Type:           actionType,
			Text:           text,
			Prompt:         prompt,
			MentionUserIDs: mentionUserIDs,
		},
		Status: status,
	})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("create automation task failed: %v", err)), nil
	}

	return mcp.NewToolResultStructured(map[string]any{
		"status":   "ok",
		"task":     created,
		"scope":    created.Scope,
		"next_run": created.NextRunAt.Format(time.RFC3339),
	}, "automation task created"), nil
}

func (s *service) handleAutomationTaskList(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.automationStore == nil {
		return mcp.NewToolResultError("automation store is unavailable"), nil
	}
	scopeCtx, err := s.resolveAutomationScope()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	status := strings.TrimSpace(request.GetString("status", ""))
	limit := request.GetInt("limit", 20)
	tasks, err := s.automationStore.ListTasks(scopeCtx.scope, status, limit)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("list automation tasks failed: %v", err)), nil
	}
	return mcp.NewToolResultStructured(map[string]any{
		"status": "ok",
		"count":  len(tasks),
		"tasks":  tasks,
	}, "automation tasks listed"), nil
}

func (s *service) handleAutomationTaskGet(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.automationStore == nil {
		return mcp.NewToolResultError("automation store is unavailable"), nil
	}
	scopeCtx, err := s.resolveAutomationScope()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	taskID := strings.TrimSpace(request.GetString("task_id", ""))
	if taskID == "" {
		return mcp.NewToolResultError("task_id is required"), nil
	}
	task, err := s.automationStore.GetTask(taskID)
	if err != nil {
		if errors.Is(err, automation.ErrTaskNotFound) {
			return mcp.NewToolResultError("task not found"), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("get automation task failed: %v", err)), nil
	}
	if task.Scope != scopeCtx.scope {
		return mcp.NewToolResultError("task not found in current scope"), nil
	}
	return mcp.NewToolResultStructured(map[string]any{
		"status": "ok",
		"task":   task,
	}, "automation task detail"), nil
}

func (s *service) handleAutomationTaskUpdate(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.automationStore == nil {
		return mcp.NewToolResultError("automation store is unavailable"), nil
	}
	scopeCtx, err := s.resolveAutomationScope()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	taskID := strings.TrimSpace(request.GetString("task_id", ""))
	if taskID == "" {
		return mcp.NewToolResultError("task_id is required"), nil
	}

	updated, err := s.automationStore.PatchTask(taskID, func(task *automation.Task) error {
		if task.Scope != scopeCtx.scope {
			return errors.New("task not found in current scope")
		}
		if !canManageTask(*task, scopeCtx.actorID) {
			return errors.New("permission denied for task update")
		}

		if hasArgument(request, "title") {
			task.Title = strings.TrimSpace(request.GetString("title", ""))
		}
		if hasArgument(request, "every_seconds") {
			everySeconds := request.GetInt("every_seconds", 0)
			if everySeconds < 60 {
				return errors.New("every_seconds must be >= 60")
			}
			task.Schedule.EverySeconds = everySeconds
			task.NextRunAt = automation.NextRunAt(time.Now().UTC(), task.Schedule)
		}
		if hasArgument(request, "text") {
			task.Action.Text = strings.TrimSpace(request.GetString("text", ""))
		}
		if hasArgument(request, "prompt") {
			task.Action.Prompt = strings.TrimSpace(request.GetString("prompt", ""))
		}
		if hasArgument(request, "action_type") {
			actionType, err := parseActionType(request.GetString("action_type", ""))
			if err != nil {
				return err
			}
			task.Action.Type = actionType
		}
		if hasArgument(request, "mention_user_ids") {
			mentions := uniqueNonEmptyStrings(request.GetStringSlice("mention_user_ids", nil))
			if err := validateMentionPermission(scopeCtx, mentions); err != nil {
				return err
			}
			task.Action.MentionUserIDs = mentions
		}
		if hasArgument(request, "enabled") {
			enabled := request.GetBool("enabled", true)
			if enabled {
				task.Status = automation.TaskStatusActive
				if task.NextRunAt.IsZero() {
					task.NextRunAt = automation.NextRunAt(time.Now().UTC(), task.Schedule)
				}
			} else {
				task.Status = automation.TaskStatusPaused
			}
		}
		if hasArgument(request, "status") {
			status := automation.TaskStatus(strings.ToLower(strings.TrimSpace(request.GetString("status", ""))))
			switch status {
			case automation.TaskStatusActive, automation.TaskStatusPaused, automation.TaskStatusDeleted:
				task.Status = status
			default:
				return fmt.Errorf("invalid status %q", status)
			}
		}
		if hasArgument(request, "manage_mode") {
			mode, err := parseManageMode(request.GetString("manage_mode", ""), scopeCtx.isGroup)
			if err != nil {
				return err
			}
			task.ManageMode = mode
		}
		if task.Status == automation.TaskStatusActive && task.NextRunAt.IsZero() {
			task.NextRunAt = automation.NextRunAt(time.Now().UTC(), task.Schedule)
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, automation.ErrTaskNotFound) {
			return mcp.NewToolResultError("task not found"), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("update automation task failed: %v", err)), nil
	}
	return mcp.NewToolResultStructured(map[string]any{
		"status": "ok",
		"task":   updated,
	}, "automation task updated"), nil
}

func (s *service) handleAutomationTaskDelete(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.automationStore == nil {
		return mcp.NewToolResultError("automation store is unavailable"), nil
	}
	scopeCtx, err := s.resolveAutomationScope()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	taskID := strings.TrimSpace(request.GetString("task_id", ""))
	if taskID == "" {
		return mcp.NewToolResultError("task_id is required"), nil
	}

	deleted, err := s.automationStore.PatchTask(taskID, func(task *automation.Task) error {
		if task.Scope != scopeCtx.scope {
			return errors.New("task not found in current scope")
		}
		if !canManageTask(*task, scopeCtx.actorID) {
			return errors.New("permission denied for task delete")
		}
		task.Status = automation.TaskStatusDeleted
		return nil
	})
	if err != nil {
		if errors.Is(err, automation.ErrTaskNotFound) {
			return mcp.NewToolResultError("task not found"), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("delete automation task failed: %v", err)), nil
	}
	return mcp.NewToolResultStructured(map[string]any{
		"status": "ok",
		"task":   deleted,
	}, "automation task deleted"), nil
}

func (s *service) resolveAutomationScope() (automationScopeContext, error) {
	sessionContext, err := s.loadSessionContext()
	if err != nil {
		return automationScopeContext{}, err
	}
	actorUserID := strings.TrimSpace(sessionContext.ActorUserID)
	actorOpenID := strings.TrimSpace(sessionContext.ActorOpenID)
	actorID := actorUserID
	if actorID == "" {
		actorID = actorOpenID
	}
	if actorID == "" {
		return automationScopeContext{}, errors.New("missing actor id in mcp context")
	}

	chatType := strings.ToLower(strings.TrimSpace(sessionContext.ChatType))
	isGroup := chatType == "group" || chatType == "topic_group"
	ctx := automationScopeContext{
		creator: automation.Actor{
			UserID: actorUserID,
			OpenID: actorOpenID,
		},
		actorID: actorID,
		isGroup: isGroup,
	}
	if isGroup {
		receiveID := strings.TrimSpace(sessionContext.ReceiveID)
		if receiveID == "" {
			return automationScopeContext{}, errors.New("missing chat_id for group automation scope")
		}
		ctx.scope = automation.Scope{Kind: automation.ScopeKindChat, ID: receiveID}
		ctx.route = automation.Route{ReceiveIDType: "chat_id", ReceiveID: receiveID}
		return ctx, nil
	}

	ctx.scope = automation.Scope{Kind: automation.ScopeKindUser, ID: actorID}
	if actorUserID != "" {
		ctx.route = automation.Route{ReceiveIDType: "user_id", ReceiveID: actorUserID}
	} else if actorOpenID != "" {
		ctx.route = automation.Route{ReceiveIDType: "open_id", ReceiveID: actorOpenID}
	} else {
		ctx.route = automation.Route{ReceiveIDType: strings.TrimSpace(sessionContext.ReceiveIDType), ReceiveID: strings.TrimSpace(sessionContext.ReceiveID)}
	}
	return ctx, nil
}

func parseManageMode(raw string, groupScope bool) (automation.ManageMode, error) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return automation.ManageModeCreatorOnly, nil
	}
	mode := automation.ManageMode(raw)
	switch mode {
	case automation.ManageModeCreatorOnly:
		return mode, nil
	case automation.ManageModeScopeAll:
		if !groupScope {
			return "", errors.New("scope_all is only allowed in group scope")
		}
		return mode, nil
	default:
		return "", fmt.Errorf("invalid manage_mode %q", raw)
	}
}

func resolveActionType(raw, prompt string) (automation.ActionType, error) {
	if strings.TrimSpace(raw) == "" {
		if strings.TrimSpace(prompt) != "" {
			return automation.ActionTypeRunLLM, nil
		}
		return automation.ActionTypeSendText, nil
	}
	return parseActionType(raw)
}

func parseActionType(raw string) (automation.ActionType, error) {
	actionType := automation.ActionType(strings.ToLower(strings.TrimSpace(raw)))
	switch actionType {
	case automation.ActionTypeSendText, automation.ActionTypeRunLLM:
		return actionType, nil
	default:
		return "", fmt.Errorf("invalid action_type %q", raw)
	}
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
	if actorID == task.Creator.PreferredID() {
		return true
	}
	if task.Scope.Kind == automation.ScopeKindChat && task.ManageMode == automation.ManageModeScopeAll {
		return true
	}
	return false
}

func hasArgument(request mcp.CallToolRequest, key string) bool {
	_, ok := request.GetArguments()[key]
	return ok
}

func uniqueNonEmptyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func buildAutomationScopeHint(sessionContext mcpbridge.SessionContext) string {
	return fmt.Sprintf("scope chat_type=%s receive_id_type=%s receive_id=%s actor_user_id=%s actor_open_id=%s",
		strings.TrimSpace(sessionContext.ChatType),
		strings.TrimSpace(sessionContext.ReceiveIDType),
		strings.TrimSpace(sessionContext.ReceiveID),
		strings.TrimSpace(sessionContext.ActorUserID),
		strings.TrimSpace(sessionContext.ActorOpenID),
	)
}
