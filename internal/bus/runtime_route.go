package bus

import (
	"context"
	"fmt"
	"strings"

	"alice/internal/domain"
)

// resolveRoute determines the routing target for an external event.
func (r *Runtime) resolveRoute(ctx context.Context, evt domain.ExternalEvent) (domain.RouteTarget, string, error) {
	if isScheduleTriggerEvent(evt) && evt.ScheduledTaskID != "" {
		source, ok, err := r.store.Indexes.GetScheduleSource(ctx, evt.ScheduledTaskID)
		if err != nil {
			return domain.RouteTarget{}, "", err
		}
		if !ok || !source.Enabled {
			return domain.RouteTarget{}, "scheduled_source_missing", nil
		}
		return domain.RouteTarget{}, "scheduled_source", nil
	}

	// 1) Explicit task/request route target.
	if strings.TrimSpace(evt.TaskID) != "" {
		active, err := r.store.Indexes.IsTaskActive(ctx, evt.TaskID)
		if err != nil {
			return domain.RouteTarget{}, "", err
		}
		if !active {
			return domain.RouteTarget{}, "", fmt.Errorf("%w: task_id=%s", domain.ErrTerminalObjectNotRoutable, evt.TaskID)
		}
		return domain.RouteTarget{Kind: domain.RouteTargetTask, ID: strings.TrimSpace(evt.TaskID)}, "task_id", nil
	}
	if strings.TrimSpace(evt.RequestID) != "" {
		open, err := r.store.Indexes.IsRequestOpen(ctx, evt.RequestID)
		if err != nil {
			return domain.RouteTarget{}, "", err
		}
		if !open {
			return domain.RouteTarget{}, "", fmt.Errorf("%w: request_id=%s", domain.ErrTerminalObjectNotRoutable, evt.RequestID)
		}
		return domain.RouteTarget{Kind: domain.RouteTargetRequest, ID: strings.TrimSpace(evt.RequestID)}, "request_id", nil
	}

	type candidate struct {
		key string
		tag string
	}
	candidates := make([]candidate, 0, 8)
	if evt.ReplyToEventID != "" {
		candidates = append(candidates, candidate{key: r.routeKeys.ReplyTo(evt.ReplyToEventID), tag: "reply_to_event_id"})
	}
	if evt.RepoRef != "" && evt.IssueRef != "" {
		candidates = append(candidates, candidate{key: r.routeKeys.RepoIssue(evt.RepoRef, evt.IssueRef), tag: "repo_ref+issue_ref"})
	}
	if evt.RepoRef != "" && evt.PRRef != "" {
		candidates = append(candidates, candidate{key: r.routeKeys.RepoPR(evt.RepoRef, evt.PRRef), tag: "repo_ref+pr_ref"})
	}
	if evt.ScheduledTaskID != "" {
		candidates = append(candidates, candidate{key: r.routeKeys.ScheduledTask(evt.ScheduledTaskID), tag: "scheduled_task_id"})
	}
	if evt.ControlObjectRef != "" {
		candidates = append(candidates, candidate{key: r.routeKeys.ControlObject(evt.ControlObjectRef), tag: "control_object_ref"})
	}
	if evt.WorkflowObjectRef != "" {
		candidates = append(candidates, candidate{key: r.routeKeys.WorkflowObject(evt.WorkflowObjectRef), tag: "workflow_object_ref"})
	}
	if evt.ConversationID != "" {
		candidates = append(candidates, candidate{key: r.routeKeys.Conversation(evt.SourceKind, evt.ConversationID, evt.ThreadID), tag: "conversation_id+thread_id"})
	}
	if evt.CoalescingKey != "" {
		candidates = append(candidates, candidate{key: evt.CoalescingKey, tag: "coalescing_key"})
	}

	for _, c := range candidates {
		target, err := r.store.Indexes.GetRouteTarget(ctx, c.key)
		if err != nil {
			return domain.RouteTarget{}, "", err
		}
		if !target.Found() {
			continue
		}
		if target.Kind == domain.RouteTargetTask {
			active, err := r.store.Indexes.IsTaskActive(ctx, target.ID)
			if err != nil {
				return domain.RouteTarget{}, "", err
			}
			if !active {
				continue
			}
		}
		if target.Kind == domain.RouteTargetRequest {
			open, err := r.store.Indexes.IsRequestOpen(ctx, target.ID)
			if err != nil {
				return domain.RouteTarget{}, "", err
			}
			if !open {
				continue
			}
		}
		if c.tag == "reply_to_event_id" && target.Kind == domain.RouteTargetTask {
			conflict, err := r.hasGovernanceRouteConflict(ctx, evt, target)
			if err != nil {
				return domain.RouteTarget{}, "", err
			}
			if conflict {
				continue
			}
		}
		return target, c.tag, nil
	}

	return domain.RouteTarget{}, "new_request", nil
}

func (r *Runtime) lookupTaskIDForChild(ctx context.Context, tag, id string) (string, error) {
	switch tag {
	case "approval_request_id":
		approval, ok, err := r.store.Indexes.GetApprovalRequest(ctx, id)
		if err != nil {
			return "", err
		}
		if ok {
			return approval.TaskID, nil
		}
	case "human_wait_id":
		wait, ok, err := r.store.Indexes.GetHumanWait(ctx, id)
		if err != nil {
			return "", err
		}
		if ok {
			return wait.TaskID, nil
		}
	case "step_execution_id":
		return r.store.Indexes.GetTaskIDByExecutionID(ctx, id)
	}
	return "", nil
}

func (r *Runtime) hasGovernanceRouteConflict(ctx context.Context, evt domain.ExternalEvent, replyTarget domain.RouteTarget) (bool, error) {
	keys := []string{}
	if evt.ScheduledTaskID != "" {
		keys = append(keys, r.routeKeys.ScheduledTask(evt.ScheduledTaskID))
	}
	if evt.ControlObjectRef != "" {
		keys = append(keys, r.routeKeys.ControlObject(evt.ControlObjectRef))
	}
	if evt.WorkflowObjectRef != "" {
		keys = append(keys, r.routeKeys.WorkflowObject(evt.WorkflowObjectRef))
	}
	if len(keys) == 0 {
		return false, nil
	}
	for _, key := range keys {
		target, err := r.store.Indexes.GetRouteTarget(ctx, key)
		if err != nil {
			return false, err
		}
		if !target.Found() {
			continue
		}
		if target.Kind != replyTarget.Kind || target.ID != replyTarget.ID {
			return true, nil
		}
	}
	return false, nil
}

func (r *Runtime) deriveRequestRouteKeys(evt domain.ExternalEvent) []string {
	keys := []string{}
	if evt.ConversationID != "" {
		keys = append(keys, r.routeKeys.Conversation(evt.SourceKind, evt.ConversationID, evt.ThreadID))
	}
	if evt.CoalescingKey != "" {
		keys = append(keys, evt.CoalescingKey)
	}
	return keys
}

func (r *Runtime) deriveTaskRouteKeys(evt domain.ExternalEvent) []string {
	keys := []string{}
	if evt.ReplyToEventID != "" {
		keys = append(keys, r.routeKeys.ReplyTo(evt.ReplyToEventID))
	}
	if evt.RepoRef != "" && evt.IssueRef != "" {
		keys = append(keys, r.routeKeys.RepoIssue(evt.RepoRef, evt.IssueRef))
	}
	if evt.RepoRef != "" && evt.PRRef != "" {
		keys = append(keys, r.routeKeys.RepoPR(evt.RepoRef, evt.PRRef))
	}
	if evt.ScheduledTaskID != "" {
		keys = append(keys, r.routeKeys.ScheduledTask(evt.ScheduledTaskID))
	}
	if evt.ControlObjectRef != "" {
		keys = append(keys, r.routeKeys.ControlObject(evt.ControlObjectRef))
	}
	if evt.WorkflowObjectRef != "" {
		keys = append(keys, r.routeKeys.WorkflowObject(evt.WorkflowObjectRef))
	}
	return keys
}

func (r *Runtime) ingestAggregateID(evt domain.ExternalEvent, taskID string) string {
	if evt.RequestID != "" {
		return evt.RequestID
	}
	if taskID != "" {
		return "req_for_task:" + taskID
	}
	return "req_ingress:" + evt.EventID
}

func isScheduleTriggerEvent(evt domain.ExternalEvent) bool {
	return evt.EventType == domain.EventTypeScheduleTriggered && evt.SourceKind == "scheduler"
}

func isHumanActionSource(source string) bool {
	switch strings.TrimSpace(source) {
	case "human_action", "human-action":
		return true
	default:
		return false
	}
}
