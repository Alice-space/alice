package bus

import (
	"context"
	"strings"

	"alice/internal/domain"
)

// resolveRoute determines the routing target for an external event.
func (r *Runtime) resolveRoute(ctx context.Context, evt domain.ExternalEvent) (domain.RouteTarget, string, error) {
	// Check explicit route target fields first
	candidates := []struct {
		id   string
		tag  string
		kind domain.RouteTargetKind
	}{
		{evt.TaskID, "task_id", domain.RouteTargetTask},
		{evt.RequestID, "request_id", domain.RouteTargetRequest},
		{evt.ApprovalRequestID, "approval_request_id", domain.RouteTargetTask},
		{evt.HumanWaitID, "human_wait_id", domain.RouteTargetTask},
		{evt.StepExecutionID, "step_execution_id", domain.RouteTargetTask},
	}

	for _, c := range candidates {
		if strings.TrimSpace(c.id) != "" {
			target := domain.RouteTarget{Kind: c.kind, ID: strings.TrimSpace(c.id)}
			if c.tag == "approval_request_id" || c.tag == "human_wait_id" || c.tag == "step_execution_id" {
				taskID, err := r.lookupTaskIDForChild(ctx, c.tag, c.id)
				if err != nil {
					return domain.RouteTarget{}, "", err
				}
				if taskID == "" {
					continue
				}
				target = domain.RouteTarget{Kind: domain.RouteTargetTask, ID: taskID}
			}
			return target, c.tag, nil
		}
	}

	// Derive route keys from event fields and lookup
	keys := r.deriveTaskRouteKeys(evt)
	for _, key := range keys {
		target, err := r.store.Indexes.GetRouteTarget(ctx, key)
		if err != nil {
			return domain.RouteTarget{}, "", err
		}
		if target.Found() {
			return target, key, nil
		}
	}

	// Check governance routes for conflicts
	for _, c := range candidates {
		if c.id != "" && c.tag == "reply_to_event_id" {
			target := domain.RouteTarget{Kind: c.kind, ID: c.id}
			conflict, err := r.hasGovernanceRouteConflict(ctx, evt, target)
			if err != nil {
				return domain.RouteTarget{}, "", err
			}
			if conflict {
				continue
			}
			return target, c.tag, nil
		}
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
	return "ingest_" + taskID
}

func isScheduleTriggerEvent(evt domain.ExternalEvent) bool {
	return evt.EventType == domain.EventTypeScheduleTriggered && evt.SourceKind == "scheduler"
}

func isHumanActionSource(sourceKind string) bool {
	return sourceKind == "human_action"
}
