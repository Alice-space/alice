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
	"gitee.com/alicespace/alice/internal/codearmy"
	"gitee.com/alicespace/alice/internal/mcpbridge"
)

const (
	ToolAutomationTaskCreate = "automation_task_create"
	ToolAutomationTaskList   = "automation_task_list"
	ToolAutomationTaskGet    = "automation_task_get"
	ToolAutomationTaskUpdate = "automation_task_update"
	ToolAutomationTaskDelete = "automation_task_delete"
	ToolCodeArmyStatusGet    = "code_army_status_get"
)

type automationScopeContext struct {
	scope   automation.Scope
	route   automation.Route
	creator automation.Actor
	actorID string
	isGroup bool
	session mcpbridge.SessionContext
}

func (s *service) registerAutomationTools(mcpServer *server.MCPServer) {
	if mcpServer == nil {
		return
	}

	mcpServer.AddTool(mcp.NewTool(
		ToolAutomationTaskCreate,
		mcp.WithDescription("创建自动化任务。私聊按用户作用域隔离，群聊按群作用域隔离。"),
		mcp.WithString("title", mcp.Description("任务标题，可选")),
		mcp.WithString("schedule_type", mcp.Description("调度类型：interval 或 cron；默认 interval"), mcp.Enum("interval", "cron")),
		mcp.WithNumber("every_seconds", mcp.Description("interval 调度间隔秒数，最小60秒"), mcp.Min(60)),
		mcp.WithString("cron_expr", mcp.Description("cron 调度表达式，支持 5 段标准格式，如 0 9 * * *")),
		mcp.WithString("action_type", mcp.Description("任务动作类型：send_text、run_llm 或 run_workflow；默认 send_text"), mcp.Enum("send_text", "run_llm", "run_workflow")),
		mcp.WithString("text", mcp.Description("发送文本，可选；与 mention_user_ids 至少一项非空")),
		mcp.WithString("prompt", mcp.Description("run_llm 动作的提示词；支持模板变量 {{now}}/{{date}}/{{time}}/{{unix}}")),
		mcp.WithString("model", mcp.Description("run_llm 可选模型名，透传至 codex -m")),
		mcp.WithString("profile", mcp.Description("run_llm 可选 profile，透传至 codex -p")),
		mcp.WithString("workflow", mcp.Description("run_workflow 名称，当前支持 code_army")),
		mcp.WithString("state_key", mcp.Description("run_workflow 状态key，可选，默认任务ID")),
		mcp.WithArray("mention_user_ids", mcp.Description("要@的用户id列表，私聊仅允许@当前用户"), mcp.WithStringItems()),
		mcp.WithNumber("max_runs", mcp.Description("任务执行次数上限，0表示不限制；1表示仅执行一次"), mcp.Min(0)),
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
		mcp.WithString("schedule_type", mcp.Description("新调度类型：interval 或 cron"), mcp.Enum("interval", "cron")),
		mcp.WithNumber("every_seconds", mcp.Description("interval 的新间隔秒数，最小60秒"), mcp.Min(60)),
		mcp.WithString("cron_expr", mcp.Description("cron 的新表达式，如 0 9 * * *")),
		mcp.WithString("action_type", mcp.Description("新动作类型：send_text、run_llm 或 run_workflow"), mcp.Enum("send_text", "run_llm", "run_workflow")),
		mcp.WithString("text", mcp.Description("新文本")),
		mcp.WithString("prompt", mcp.Description("run_llm 动作的新提示词；支持模板变量 {{now}}/{{date}}/{{time}}/{{unix}}")),
		mcp.WithString("model", mcp.Description("run_llm 的新模型名，透传至 codex -m")),
		mcp.WithString("profile", mcp.Description("run_llm 的新 profile，透传至 codex -p")),
		mcp.WithString("workflow", mcp.Description("run_workflow 新名称，当前支持 code_army")),
		mcp.WithString("state_key", mcp.Description("run_workflow 新状态key")),
		mcp.WithArray("mention_user_ids", mcp.Description("新的@用户id列表"), mcp.WithStringItems()),
		mcp.WithNumber("max_runs", mcp.Description("新的执行次数上限，0表示不限制"), mcp.Min(0)),
		mcp.WithBoolean("enabled", mcp.Description("设置启用状态")),
		mcp.WithString("status", mcp.Description("active/paused/deleted"), mcp.Enum("active", "paused", "deleted")),
		mcp.WithString("manage_mode", mcp.Description("creator_only 或 scope_all（仅群聊可用）"), mcp.Enum("creator_only", "scope_all")),
	), s.handleAutomationTaskUpdate)

	mcpServer.AddTool(mcp.NewTool(
		ToolAutomationTaskDelete,
		mcp.WithDescription("删除自动化任务（软删除，状态改为deleted）。"),
		mcp.WithString("task_id", mcp.Required(), mcp.Description("任务ID")),
	), s.handleAutomationTaskDelete)

	mcpServer.AddTool(mcp.NewTool(
		ToolCodeArmyStatusGet,
		mcp.WithDescription("查看当前对话下 code_army 的状态。默认返回当前对话绑定的全部状态；传 state_key 可查看指定状态。"),
		mcp.WithString("state_key", mcp.Description("可选，指定当前对话下某个 code_army 状态key")),
	), s.handleCodeArmyStatusGet)
}

func (s *service) handleAutomationTaskCreate(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.automationStore == nil {
		return mcp.NewToolResultError("automation store is unavailable"), nil
	}
	scopeCtx, err := s.resolveAutomationScope()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	schedule, err := resolveCreateSchedule(request)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	title := strings.TrimSpace(request.GetString("title", ""))
	text := strings.TrimSpace(request.GetString("text", ""))
	prompt := strings.TrimSpace(request.GetString("prompt", ""))
	model := strings.TrimSpace(request.GetString("model", ""))
	profile := strings.TrimSpace(request.GetString("profile", ""))
	workflow := strings.TrimSpace(request.GetString("workflow", ""))
	stateKey := strings.TrimSpace(request.GetString("state_key", ""))
	actionType, err := resolveActionType(request.GetString("action_type", ""), prompt, workflow)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	mentionUserIDs := uniqueNonEmptyStrings(request.GetStringSlice("mention_user_ids", nil))
	if err := validateMentionPermission(scopeCtx, mentionUserIDs); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	maxRuns, err := parseMaxRunsForCreate(request)
	if err != nil {
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
		Schedule:   schedule,
		MaxRuns:    maxRuns,
		Action: automation.Action{
			Type:           actionType,
			Text:           text,
			Prompt:         prompt,
			Model:          model,
			Profile:        profile,
			Workflow:       workflow,
			StateKey:       stateKey,
			SessionKey:     workflowSessionKey(scopeCtx, actionType),
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
		if err := patchTaskScheduleFromRequest(request, task); err != nil {
			return err
		}
		if hasArgument(request, "text") {
			task.Action.Text = strings.TrimSpace(request.GetString("text", ""))
		}
		if hasArgument(request, "prompt") {
			task.Action.Prompt = strings.TrimSpace(request.GetString("prompt", ""))
		}
		if hasArgument(request, "model") {
			task.Action.Model = strings.TrimSpace(request.GetString("model", ""))
		}
		if hasArgument(request, "profile") {
			task.Action.Profile = strings.TrimSpace(request.GetString("profile", ""))
		}
		if hasArgument(request, "workflow") {
			task.Action.Workflow = strings.TrimSpace(request.GetString("workflow", ""))
		}
		if hasArgument(request, "state_key") {
			task.Action.StateKey = strings.TrimSpace(request.GetString("state_key", ""))
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
		if err := patchTaskMaxRunsFromRequest(request, task); err != nil {
			return err
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
		task.Action.SessionKey = updatedWorkflowSessionKey(scopeCtx, *task)
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

func (s *service) handleCodeArmyStatusGet(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.codeArmyStatus == nil {
		return mcp.NewToolResultError("code_army inspector is unavailable"), nil
	}

	sessionContext, err := s.loadSessionContext()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	sessionKey := strings.TrimSpace(sessionContext.SessionKey)
	if sessionKey == "" {
		return mcp.NewToolResultError("missing current conversation session key in mcp context"), nil
	}

	stateKey := strings.TrimSpace(request.GetString("state_key", ""))
	if stateKey != "" {
		state, err := s.codeArmyStatus.Get(sessionKey, stateKey)
		if err != nil {
			if errors.Is(err, codearmy.ErrStateNotFound) {
				return mcp.NewToolResultError("code_army state not found in current conversation"), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("get code_army status failed: %v", err)), nil
		}
		return mcp.NewToolResultStructured(map[string]any{
			"status":      "ok",
			"session_key": sessionKey,
			"state":       state,
		}, "code_army status loaded"), nil
	}

	states, err := s.codeArmyStatus.List(sessionKey)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("list code_army status failed: %v", err)), nil
	}
	return mcp.NewToolResultStructured(map[string]any{
		"status":      "ok",
		"session_key": sessionKey,
		"count":       len(states),
		"states":      states,
	}, "code_army statuses loaded"), nil
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
		session: sessionContext,
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
