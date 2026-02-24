package mcpserver

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"gitee.com/alicespace/alice/internal/automation"
	"gitee.com/alicespace/alice/internal/mcpbridge"
)

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

func resolveActionType(raw, prompt, workflow string) (automation.ActionType, error) {
	if strings.TrimSpace(raw) == "" {
		if strings.TrimSpace(workflow) != "" {
			return automation.ActionTypeRunWorkflow, nil
		}
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
	case automation.ActionTypeSendText, automation.ActionTypeRunLLM, automation.ActionTypeRunWorkflow:
		return actionType, nil
	default:
		return "", fmt.Errorf("invalid action_type %q", raw)
	}
}

func resolveCreateSchedule(request mcp.CallToolRequest) (automation.Schedule, error) {
	scheduleType, err := resolveScheduleTypeForCreate(request)
	if err != nil {
		return automation.Schedule{}, err
	}

	schedule := automation.Schedule{Type: scheduleType}
	switch scheduleType {
	case automation.ScheduleTypeInterval:
		everySeconds := request.GetInt("every_seconds", 0)
		if everySeconds < 60 {
			return automation.Schedule{}, errors.New("every_seconds must be >= 60 for interval schedule")
		}
		schedule.EverySeconds = everySeconds
	case automation.ScheduleTypeCron:
		cronExpr := strings.TrimSpace(request.GetString("cron_expr", ""))
		if cronExpr == "" {
			return automation.Schedule{}, errors.New("cron_expr is required for cron schedule")
		}
		schedule.CronExpr = cronExpr
	default:
		return automation.Schedule{}, fmt.Errorf("invalid schedule_type %q", scheduleType)
	}
	return schedule, nil
}

func resolveScheduleTypeForCreate(request mcp.CallToolRequest) (automation.ScheduleType, error) {
	rawType := strings.TrimSpace(request.GetString("schedule_type", ""))
	cronExpr := strings.TrimSpace(request.GetString("cron_expr", ""))
	if rawType == "" {
		if cronExpr != "" {
			return automation.ScheduleTypeCron, nil
		}
		return automation.ScheduleTypeInterval, nil
	}
	return parseScheduleType(rawType)
}

func parseScheduleType(raw string) (automation.ScheduleType, error) {
	scheduleType := automation.ScheduleType(strings.ToLower(strings.TrimSpace(raw)))
	switch scheduleType {
	case automation.ScheduleTypeInterval, automation.ScheduleTypeCron:
		return scheduleType, nil
	default:
		return "", fmt.Errorf("invalid schedule_type %q", raw)
	}
}

func parseMaxRunsForCreate(request mcp.CallToolRequest) (int, error) {
	maxRuns := request.GetInt("max_runs", 0)
	if maxRuns < 0 {
		return 0, errors.New("max_runs must be >= 0")
	}
	return maxRuns, nil
}

func patchTaskScheduleFromRequest(request mcp.CallToolRequest, task *automation.Task) error {
	if task == nil {
		return errors.New("task is nil")
	}
	hasScheduleType := hasArgument(request, "schedule_type")
	hasEverySeconds := hasArgument(request, "every_seconds")
	hasCronExpr := hasArgument(request, "cron_expr")
	if !hasScheduleType && !hasEverySeconds && !hasCronExpr {
		return nil
	}

	if hasScheduleType {
		scheduleType, err := parseScheduleType(request.GetString("schedule_type", ""))
		if err != nil {
			return err
		}
		task.Schedule.Type = scheduleType
	}

	if hasEverySeconds {
		everySeconds := request.GetInt("every_seconds", 0)
		if everySeconds < 60 {
			return errors.New("every_seconds must be >= 60 for interval schedule")
		}
		task.Schedule.EverySeconds = everySeconds
		if !hasScheduleType {
			task.Schedule.Type = automation.ScheduleTypeInterval
		}
	}

	if hasCronExpr {
		task.Schedule.CronExpr = strings.TrimSpace(request.GetString("cron_expr", ""))
		if !hasScheduleType {
			task.Schedule.Type = automation.ScheduleTypeCron
		}
	}

	switch task.Schedule.Type {
	case automation.ScheduleTypeInterval:
		task.Schedule.CronExpr = ""
	case automation.ScheduleTypeCron:
		task.Schedule.EverySeconds = 0
	}
	task.NextRunAt = automation.NextRunAt(time.Now().UTC(), task.Schedule)
	return nil
}

func patchTaskMaxRunsFromRequest(request mcp.CallToolRequest, task *automation.Task) error {
	if task == nil {
		return errors.New("task is nil")
	}
	if !hasArgument(request, "max_runs") {
		return nil
	}
	maxRuns := request.GetInt("max_runs", 0)
	if maxRuns < 0 {
		return errors.New("max_runs must be >= 0")
	}
	task.MaxRuns = maxRuns
	if task.MaxRuns > 0 && task.RunCount >= task.MaxRuns {
		task.Status = automation.TaskStatusPaused
		task.NextRunAt = time.Time{}
	}
	return nil
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
