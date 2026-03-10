package mcp

import (
	"encoding/json"
	"time"

	"alice/internal/domain"
)

type MCPActionRequest struct {
	ActionID       string          `json:"action_id"`
	IdempotencyKey string          `json:"idempotency_key"`
	TraceID        string          `json:"trace_id"`
	TaskID         string          `json:"task_id"`
	ExecutionID    string          `json:"execution_id"`
	Domain         string          `json:"domain"`
	ActionType     string          `json:"action_type"`
	TargetRef      string          `json:"target_ref"`
	Payload        json.RawMessage `json:"payload"`
	DeadlineAt     time.Time       `json:"deadline_at"`
}

type MCPActionResponse struct {
	ActionID        string          `json:"action_id"`
	Status          string          `json:"status"`
	ExternalRef     string          `json:"external_ref"`
	Result          json.RawMessage `json:"result"`
	ErrorCode       string          `json:"error_code"`
	ErrorMessage    string          `json:"error_message"`
	RemoteRequestID string          `json:"remote_request_id"`
}

type MCPActionLookupRequest struct {
	ActionID        string `json:"action_id"`
	RemoteRequestID string `json:"remote_request_id"`
	IdempotencyKey  string `json:"idempotency_key"`
}

type MCPActionStatusResponse struct {
	ActionID        string          `json:"action_id"`
	RemoteRequestID string          `json:"remote_request_id"`
	IdempotencyKey  string          `json:"idempotency_key"`
	Status          string          `json:"status"`
	ExternalRef     string          `json:"external_ref"`
	Result          json.RawMessage `json:"result"`
	ErrorCode       string          `json:"error_code"`
	ErrorMessage    string          `json:"error_message"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

type MCPQueryRequest struct {
	TraceID     string          `json:"trace_id"`
	QueryType   string          `json:"query_type"`
	QueryTarget string          `json:"query_target"`
	Params      json.RawMessage `json:"params"`
	DeadlineAt  time.Time       `json:"deadline_at"`
}

type MCPQueryResponse struct {
	Status       string          `json:"status"`
	ExternalRef  string          `json:"external_ref"`
	Result       json.RawMessage `json:"result"`
	ErrorCode    string          `json:"error_code"`
	ErrorMessage string          `json:"error_message"`
}

type MCPWebhookReceipt struct {
	ActionID        string          `json:"action_id"`
	RemoteRequestID string          `json:"remote_request_id"`
	IdempotencyKey  string          `json:"idempotency_key"`
	Status          string          `json:"status"`
	ExternalRef     string          `json:"external_ref"`
	Result          json.RawMessage `json:"result"`
	ErrorCode       string          `json:"error_code"`
	ErrorMessage    string          `json:"error_message"`
	ReceivedAt      time.Time       `json:"received_at"`
}

func AdvanceOutboxStatusByActionResult(record *domain.OutboxRecord, resp *MCPActionResponse) domain.OutboxStatus {
	switch resp.Status {
	case "completed":
		record.LastExternalRef = resp.ExternalRef
		record.RemoteRequestID = resp.RemoteRequestID
		record.LastReceiptStatus = "completed"
		return domain.OutboxStatusSucceeded
	case "accepted":
		record.RemoteRequestID = resp.RemoteRequestID
		record.LastReceiptStatus = "accepted"
		record.ReceiptWindowUntil = time.Now().UTC().Add(10 * time.Minute)
		return domain.OutboxStatusDispatching
	case "rejected":
		record.LastErrorCode = resp.ErrorCode
		record.LastErrorMessage = resp.ErrorMessage
		switch classifyError(resp.ErrorCode) {
		case "retryable", "rate_limited":
			record.NextAttemptAt = time.Now().UTC().Add(backoffDuration(record.AttemptCount))
			return domain.OutboxStatusRetryWait
		case "conflict":
			record.ReceiptWindowUntil = time.Now().UTC().Add(10 * time.Minute)
			return domain.OutboxStatusDispatching
		default:
			return domain.OutboxStatusDead
		}
	default:
		record.LastErrorCode = "unknown_action_status"
		record.LastErrorMessage = resp.Status
		return domain.OutboxStatusRetryWait
	}
}

func AdvanceOutboxStatusByLookup(record *domain.OutboxRecord, status *MCPActionStatusResponse) domain.OutboxStatus {
	record.LastReceiptStatus = status.Status
	record.RemoteRequestID = status.RemoteRequestID
	record.LastExternalRef = status.ExternalRef
	record.LastErrorCode = status.ErrorCode
	record.LastErrorMessage = status.ErrorMessage
	switch status.Status {
	case "pending", "running":
		record.ReceiptWindowUntil = time.Now().UTC().Add(10 * time.Minute)
		return domain.OutboxStatusDispatching
	case "completed":
		return domain.OutboxStatusSucceeded
	case "failed":
		switch classifyError(status.ErrorCode) {
		case "retryable", "rate_limited", "conflict":
			record.NextAttemptAt = time.Now().UTC().Add(backoffDuration(record.AttemptCount))
			return domain.OutboxStatusRetryWait
		default:
			return domain.OutboxStatusDead
		}
	case "unknown":
		if record.AttemptCount > 5 {
			return domain.OutboxStatusDead
		}
		record.NextAttemptAt = time.Now().UTC().Add(backoffDuration(record.AttemptCount))
		return domain.OutboxStatusDispatching
	default:
		return domain.OutboxStatusDispatching
	}
}

func classifyError(code string) string {
	switch code {
	case "retryable", "rate_limited", "unauthorized", "conflict", "not_found", "permanent":
		return code
	default:
		return "retryable"
	}
}

func backoffDuration(attempt uint32) time.Duration {
	if attempt == 0 {
		return time.Second
	}
	sec := 1 << minInt(int(attempt), 8)
	return time.Duration(sec) * time.Second
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
