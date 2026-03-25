package campaignrepo

// ReconcileEventKind is the type of a reconcile event.
type ReconcileEventKind string

const (
	EventPlanningStarted      ReconcileEventKind = "planning_started"
	EventProposalSubmitted    ReconcileEventKind = "proposal_submitted"
	EventPlanReviewVerdict    ReconcileEventKind = "plan_review_verdict"
	EventHumanApprovalNeeded  ReconcileEventKind = "human_approval_needed"
	EventPlanApproved         ReconcileEventKind = "plan_approved"
	EventTaskDispatched       ReconcileEventKind = "task_dispatched"
	EventReviewVerdictApplied ReconcileEventKind = "review_verdict_applied"
	EventReplanRequested      ReconcileEventKind = "replan_requested"
	EventTaskBlocked          ReconcileEventKind = "task_blocked"
	EventDiscoveryReported    ReconcileEventKind = "discovery_reported"
)

// ReconcileEvent represents a state change event produced during reconciliation.
type ReconcileEvent struct {
	Kind       ReconcileEventKind
	CampaignID string
	TaskID     string // empty for campaign-level events
	Title      string
	Detail     string
	Severity   string // "info" | "success" | "warning" | "error"
}
