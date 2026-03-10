package ingress

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"alice/internal/bus"
	"alice/internal/domain"
)

type NormalizedEvent struct {
	EventType         domain.EventType `json:"event_type"`
	SourceKind        string           `json:"source_kind"`
	TransportKind     string           `json:"transport_kind"`
	SourceRef         string           `json:"source_ref"`
	ActorRef          string           `json:"actor_ref"`
	ActionKind        string           `json:"action_kind"`
	RequestID         string           `json:"request_id"`
	TaskID            string           `json:"task_id"`
	ApprovalRequestID string           `json:"approval_request_id"`
	HumanWaitID       string           `json:"human_wait_id"`
	StepExecutionID   string           `json:"step_execution_id"`
	TargetStepID      string           `json:"target_step_id"`
	WaitingReason     string           `json:"waiting_reason"`
	ReplyToEventID    string           `json:"reply_to_event_id"`
	ConversationID    string           `json:"conversation_id"`
	ThreadID          string           `json:"thread_id"`
	RepoRef           string           `json:"repo_ref"`
	IssueRef          string           `json:"issue_ref"`
	PRRef             string           `json:"pr_ref"`
	CommentRef        string           `json:"comment_ref"`
	ScheduledTaskID   string           `json:"scheduled_task_id"`
	ControlObjectRef  string           `json:"control_object_ref"`
	WorkflowObjectRef string           `json:"workflow_object_ref"`
	CoalescingKey     string           `json:"coalescing_key"`
	PayloadRef        string           `json:"payload_ref"`
	Verified          bool             `json:"verified"`
	IdempotencyKey    string           `json:"idempotency_key"`
	DecisionHash      string           `json:"decision_hash"`
	TraceID           string           `json:"trace_id,omitempty"`
	InputSchemaID     string           `json:"input_schema_id,omitempty"`
	InputPatch        json.RawMessage  `json:"input_patch,omitempty"`
}

type SchedulerFireRequest struct {
	ScheduledTaskID    string    `json:"scheduled_task_id"`
	ScheduledForWindow time.Time `json:"scheduled_for_window"`
}

type WebhookAuthConfig struct {
	GitHubSecret    string
	GitLabSecret    string
	SchedulerSecret string
}

type HTTPIngress struct {
	runtime           *bus.Runtime
	reception         bus.Reception
	humanActionSecret []byte
	gitHubSecret      []byte
	gitLabSecret      string
	schedulerSecret   string
}

func NewHTTPIngress(runtime *bus.Runtime, reception bus.Reception, humanActionSecret string, webhookAuth ...WebhookAuthConfig) *HTTPIngress {
	ing := &HTTPIngress{
		runtime:           runtime,
		reception:         reception,
		humanActionSecret: []byte(humanActionSecret),
	}
	if len(webhookAuth) > 0 {
		ing.gitHubSecret = []byte(webhookAuth[0].GitHubSecret)
		ing.gitLabSecret = webhookAuth[0].GitLabSecret
		ing.schedulerSecret = webhookAuth[0].SchedulerSecret
	}
	return ing
}

func (h *HTTPIngress) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/ingress/cli/messages", h.handleCLIMessage)
	mux.HandleFunc("/v1/ingress/im/feishu", h.handleIngress("direct_input", "im_feishu"))
	mux.HandleFunc("/v1/ingress/web/messages", h.handleIngress("direct_input", "web"))
	mux.HandleFunc("/v1/webhooks/github", h.handleWebhook("repo_comment", "github"))
	mux.HandleFunc("/v1/webhooks/gitlab", h.handleWebhook("repo_comment", "gitlab"))
	mux.HandleFunc("/v1/human-actions/", h.handleHumanAction)
	mux.HandleFunc("/v1/scheduler/fires", h.handleSchedulerFire)
}

func (h *HTTPIngress) handleIngress(sourceKind, transportKind string) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var in NormalizedEvent
		if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		in = h.sanitizeIngressInput(sourceKind, transportKind, in)
		evt := toExternalEvent(in)
		result, err := h.runtime.IngestExternalEvent(req.Context(), evt, h.reception)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusAccepted, writeAcceptedFromResult(result, ""))
	}
}

func (h *HTTPIngress) handleCLIMessage(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var in domain.CLIMessageSubmitRequest
	if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(in.Text) == "" {
		http.Error(w, "text is required", http.StatusBadRequest)
		return
	}
	threadID := strings.TrimSpace(in.ThreadID)
	if threadID == "" {
		threadID = "root"
	}
	sourceRef := strings.TrimSpace(in.SourceRef)
	if sourceRef == "" {
		sourceRef = "cli"
	}
	evt := domain.ExternalEvent{
		EventType:      domain.EventTypeExternalEventIngested,
		SourceKind:     "direct_input",
		TransportKind:  "cli",
		SourceRef:      sourceRef,
		ActorRef:       strings.TrimSpace(in.ActorRef),
		ConversationID: strings.TrimSpace(in.ConversationID),
		ThreadID:       threadID,
		RepoRef:        strings.TrimSpace(in.RepoRef),
		ReplyToEventID: strings.TrimSpace(in.ReplyToEventID),
		IdempotencyKey: strings.TrimSpace(in.IdempotencyKey),
		TraceID:        strings.TrimSpace(in.TraceID),
		PayloadRef:     "cli-message:text",
		Verified:       true,
		ReceivedAt:     time.Now().UTC(),
	}
	result, err := h.runtime.IngestExternalEvent(req.Context(), evt, h.reception)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusAccepted, writeAcceptedFromResult(result, ""))
}

func (h *HTTPIngress) handleSchedulerFire(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := h.authorizeSchedulerFire(req); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	var in SchedulerFireRequest
	if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if in.ScheduledTaskID == "" {
		http.Error(w, "scheduled_task_id is required", http.StatusBadRequest)
		return
	}
	if _, err := h.runtime.RequireEnabledScheduleSource(req.Context(), in.ScheduledTaskID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	window := in.ScheduledForWindow.UTC()
	if window.IsZero() {
		window = time.Now().UTC().Truncate(time.Minute)
	}
	payload, err := h.runtime.RecordScheduleFire(req.Context(), domain.RecordScheduleFireCommand{
		ScheduledTaskID:       in.ScheduledTaskID,
		ScheduledForWindowUTC: window,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusAccepted, payload)
}

func (h *HTTPIngress) authorizeSchedulerFire(req *http.Request) error {
	secret := strings.TrimSpace(h.schedulerSecret)
	if secret == "" {
		return fmt.Errorf("scheduler ingress secret is not configured")
	}
	token := strings.TrimSpace(req.Header.Get("X-Scheduler-Token"))
	if token == "" {
		auth := strings.TrimSpace(req.Header.Get("Authorization"))
		if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
			token = strings.TrimSpace(auth[len("Bearer "):])
		}
	}
	if token == "" || token != secret {
		return fmt.Errorf("unauthorized scheduler fire")
	}
	return nil
}

func (h *HTTPIngress) handleHumanAction(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := strings.TrimPrefix(req.URL.Path, "/v1/human-actions/")
	claims, err := h.verifyHumanActionToken(token)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	var in NormalizedEvent
	if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if in.DecisionHash == "" {
		in.DecisionHash = claims.DecisionHash
	}
	if in.ActionKind == "" {
		in.ActionKind = claims.ActionKind
	}
	if domain.NormalizeHumanActionKind(in.ActionKind) != domain.NormalizeHumanActionKind(claims.ActionKind) {
		http.Error(w, "action kind mismatch", http.StatusUnauthorized)
		return
	}
	if in.DecisionHash == "" || in.DecisionHash != claims.DecisionHash {
		http.Error(w, "decision hash mismatch", http.StatusUnauthorized)
		return
	}
	in.SourceKind = "human_action"
	in.TransportKind = "human_action_token"
	in.Verified = true
	if in.IdempotencyKey == "" {
		in.IdempotencyKey = token + ":" + claims.DecisionHash
	}
	if in.RequestID == "" {
		in.RequestID = claims.RequestID
	}
	if in.TaskID == "" {
		in.TaskID = claims.TaskID
	}
	if in.ReplyToEventID == "" {
		in.ReplyToEventID = claims.ReplyToEventID
	}
	if in.ScheduledTaskID == "" {
		in.ScheduledTaskID = claims.ScheduledTaskID
	}
	if in.ControlObjectRef == "" {
		in.ControlObjectRef = claims.ControlObjectRef
	}
	if in.WorkflowObjectRef == "" {
		in.WorkflowObjectRef = claims.WorkflowObjectRef
	}
	if in.ApprovalRequestID == "" {
		in.ApprovalRequestID = claims.ApprovalRequestID
	}
	if in.HumanWaitID == "" {
		in.HumanWaitID = claims.HumanWaitID
	}
	if in.StepExecutionID == "" {
		in.StepExecutionID = claims.StepExecutionID
	}
	if in.TargetStepID == "" {
		in.TargetStepID = claims.TargetStepID
	}
	if in.WaitingReason == "" {
		in.WaitingReason = claims.WaitingReason
	}
	evt := toExternalEvent(in)
	result, err := h.runtime.IngestExternalEvent(context.WithValue(req.Context(), "action_token", token), evt, h.reception)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusAccepted, writeAcceptedFromResult(result, ""))
}

func (h *HTTPIngress) handleWebhook(sourceKind, transportKind string) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(req.Body)
		if err != nil {
			http.Error(w, "read body failed", http.StatusBadRequest)
			return
		}
		if err := h.verifyWebhook(req, transportKind, body); err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}

		var in NormalizedEvent
		if err := json.Unmarshal(body, &in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		// Webhooks are third-party input: never trust explicit object ids.
		in.RequestID = ""
		in.TaskID = ""
		in.SourceKind = sourceKind
		in.TransportKind = transportKind
		in.Verified = true

		if deliveryID := h.webhookDeliveryID(req, transportKind); deliveryID != "" {
			in.IdempotencyKey = transportKind + ":" + deliveryID
		}

		evt := toExternalEvent(in)
		result, err := h.runtime.IngestExternalEvent(req.Context(), evt, h.reception)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusAccepted, writeAcceptedFromResult(result, ""))
	}
}

func (h *HTTPIngress) sanitizeIngressInput(sourceKind, transportKind string, in NormalizedEvent) NormalizedEvent {
	in.SourceKind = sourceKind
	in.TransportKind = transportKind
	switch sourceKind {
	case "direct_input":
		in.EventType = domain.EventTypeExternalEventIngested
		in.RequestID = ""
		in.TaskID = ""
		in.ActionKind = ""
		in.ApprovalRequestID = ""
		in.HumanWaitID = ""
		in.StepExecutionID = ""
		in.TargetStepID = ""
		in.WaitingReason = ""
		in.DecisionHash = ""
		in.Verified = false
	}
	return in
}

func toExternalEvent(in NormalizedEvent) domain.ExternalEvent {
	return domain.ExternalEvent{
		EventType:         in.EventType,
		SourceKind:        in.SourceKind,
		TransportKind:     in.TransportKind,
		SourceRef:         in.SourceRef,
		ActorRef:          in.ActorRef,
		ActionKind:        string(domain.NormalizeHumanActionKind(in.ActionKind)),
		RequestID:         in.RequestID,
		TaskID:            in.TaskID,
		ApprovalRequestID: in.ApprovalRequestID,
		HumanWaitID:       in.HumanWaitID,
		StepExecutionID:   in.StepExecutionID,
		TargetStepID:      in.TargetStepID,
		WaitingReason:     in.WaitingReason,
		DecisionHash:      in.DecisionHash,
		ReplyToEventID:    in.ReplyToEventID,
		ConversationID:    in.ConversationID,
		ThreadID:          in.ThreadID,
		RepoRef:           in.RepoRef,
		IssueRef:          in.IssueRef,
		PRRef:             in.PRRef,
		CommentRef:        in.CommentRef,
		ScheduledTaskID:   in.ScheduledTaskID,
		ControlObjectRef:  in.ControlObjectRef,
		WorkflowObjectRef: in.WorkflowObjectRef,
		CoalescingKey:     in.CoalescingKey,
		Verified:          in.Verified,
		PayloadRef:        in.PayloadRef,
		IdempotencyKey:    in.IdempotencyKey,
		TraceID:           in.TraceID,
		InputSchemaID:     in.InputSchemaID,
		InputPatch:        in.InputPatch,
		ReceivedAt:        time.Now().UTC(),
	}
}

func (h *HTTPIngress) verifyWebhook(req *http.Request, transportKind string, body []byte) error {
	switch transportKind {
	case "github":
		if len(h.gitHubSecret) == 0 {
			return fmt.Errorf("github webhook secret is not configured")
		}
		if req.Header.Get("X-GitHub-Event") == "" {
			return fmt.Errorf("missing github event header")
		}
		if req.Header.Get("X-GitHub-Delivery") == "" {
			return fmt.Errorf("missing github delivery id")
		}
		header := strings.TrimSpace(req.Header.Get("X-Hub-Signature-256"))
		if !strings.HasPrefix(header, "sha256=") {
			return fmt.Errorf("missing github signature")
		}
		signatureHex := strings.TrimPrefix(header, "sha256=")
		got, err := hex.DecodeString(signatureHex)
		if err != nil {
			return fmt.Errorf("invalid github signature")
		}
		mac := hmac.New(sha256.New, h.gitHubSecret)
		_, _ = mac.Write(body)
		expected := mac.Sum(nil)
		if !hmac.Equal(got, expected) {
			return fmt.Errorf("invalid github signature")
		}
		return nil
	case "gitlab":
		if strings.TrimSpace(h.gitLabSecret) == "" {
			return fmt.Errorf("gitlab webhook secret is not configured")
		}
		if req.Header.Get("X-Gitlab-Event") == "" {
			return fmt.Errorf("missing gitlab event header")
		}
		if req.Header.Get("X-Gitlab-Token") != h.gitLabSecret {
			return fmt.Errorf("invalid gitlab webhook token")
		}
		return nil
	default:
		return fmt.Errorf("unsupported webhook source=%s", transportKind)
	}
}

func (h *HTTPIngress) webhookDeliveryID(req *http.Request, transportKind string) string {
	switch transportKind {
	case "github":
		return strings.TrimSpace(req.Header.Get("X-GitHub-Delivery"))
	case "gitlab":
		if v := strings.TrimSpace(req.Header.Get("X-Gitlab-Event-UUID")); v != "" {
			return v
		}
		return strings.TrimSpace(req.Header.Get("X-Request-Id"))
	default:
		return ""
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeAcceptedFromResult(result *bus.ProcessResult, adminActionID string) domain.WriteAcceptedResponse {
	resp := domain.WriteAcceptedResponse{
		Accepted:      true,
		AdminActionID: strings.TrimSpace(adminActionID),
	}
	if result == nil {
		return resp
	}
	resp.EventID = strings.TrimSpace(result.EventID)
	resp.RequestID = strings.TrimSpace(result.RequestID)
	resp.TaskID = strings.TrimSpace(result.TaskID)
	resp.RouteTargetKind = strings.TrimSpace(result.RouteTargetKind)
	resp.RouteTargetID = strings.TrimSpace(result.RouteTargetID)
	if resp.RouteTargetKind == "" {
		if resp.TaskID != "" {
			resp.RouteTargetKind = string(domain.RouteTargetTask)
			resp.RouteTargetID = resp.TaskID
		} else if resp.RequestID != "" {
			resp.RouteTargetKind = string(domain.RouteTargetRequest)
			resp.RouteTargetID = resp.RequestID
		}
	}
	resp.CommitHLC = strings.TrimSpace(result.CommitHLC)
	return resp
}

func (h *HTTPIngress) verifyHumanActionToken(token string) (*domain.HumanActionTokenClaims, error) {
	if len(h.humanActionSecret) == 0 {
		return nil, fmt.Errorf("human action secret is not configured")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != "v1" {
		return nil, fmt.Errorf("invalid token format")
	}
	payloadRaw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid token payload")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("invalid token signature")
	}
	mac := hmac.New(sha256.New, h.humanActionSecret)
	_, _ = mac.Write([]byte(parts[1]))
	expected := mac.Sum(nil)
	if !hmac.Equal(sig, expected) {
		return nil, fmt.Errorf("invalid token signature")
	}
	var claims domain.HumanActionTokenClaims
	if err := json.Unmarshal(payloadRaw, &claims); err != nil {
		return nil, fmt.Errorf("invalid token claims")
	}
	if claims.ExpiresAt.IsZero() || time.Now().UTC().After(claims.ExpiresAt) {
		return nil, fmt.Errorf("expired token")
	}
	if claims.DecisionHash == "" || claims.Nonce == "" {
		return nil, fmt.Errorf("missing token integrity claims")
	}
	if err := domain.ValidateHumanActionClaims(claims); err != nil {
		return nil, err
	}
	claims.ActionKind = string(domain.NormalizeHumanActionKind(claims.ActionKind))
	return &claims, nil
}
