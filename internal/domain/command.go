package domain

import "time"

type IngestExternalEventCommand struct {
	Event ExternalEvent
}

type OpenEphemeralRequestCommand struct {
	RequestID         string
	OpenedByEventID   string
	TraceID           string
	RouteSnapshot     RouteSnapshot
	ActivatedRouteKey []string
	ExpiresAt         time.Time
}

type AssessPromotionCommand struct {
	RequestID string
	Decision  PromotionDecision
}

type PromoteAndBindWorkflowCommand struct {
	RequestID         string
	TaskID            string
	BindingID         string
	WorkflowID        string
	WorkflowSource    string
	WorkflowRev       string
	ManifestDigest    string
	EntryStepID       string
	RouteSnapshotRef  string
	ActivatedRouteKey []string
	At                time.Time
}

type RecordScheduleFireCommand struct {
	ScheduledTaskID       string
	SourceScheduleRev     string
	TargetWorkflowID      string
	TargetWorkflowSource  string
	TargetWorkflowRev     string
	ScheduledForWindowUTC time.Time
}
