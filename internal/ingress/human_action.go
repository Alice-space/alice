package ingress

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"alice/internal/bus"
	"alice/internal/domain"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

func (h *HTTPIngress) handleHumanAction(c *gin.Context) {
	token := strings.TrimPrefix(c.Param("token"), "/")
	var in NormalizedEvent
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := h.ingestHumanAction(c.Request.Context(), token, in, "human_action_token")
	if err != nil {
		status := http.StatusBadRequest
		if isHumanActionAuthError(err) {
			status = http.StatusUnauthorized
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, writeAcceptedFromResult(result, ""))
}

func (h *HTTPIngress) ingestHumanAction(ctx context.Context, token string, in NormalizedEvent, transportKind string) (*bus.ProcessResult, error) {
	claims, err := h.verifyHumanActionToken(token)
	if err != nil {
		return nil, err
	}
	normalized, err := h.normalizeHumanActionWithClaims(token, claims, in, transportKind)
	if err != nil {
		return nil, err
	}
	evt := toExternalEvent(normalized)
	ctx = context.WithValue(ctx, "action_token", token)
	result, err := h.runtime.IngestExternalEvent(ctx, evt, h.reception)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (h *HTTPIngress) normalizeHumanActionWithClaims(token string, claims *domain.HumanActionClaims, in NormalizedEvent, transportKind string) (NormalizedEvent, error) {
	if claims == nil {
		return NormalizedEvent{}, domain.ErrInvalidToken
	}
	if in.DecisionHash == "" {
		in.DecisionHash = claims.DecisionHash
	}
	if in.ActionKind == "" {
		in.ActionKind = claims.ActionKind
	}
	if domain.NormalizeHumanActionKind(in.ActionKind) != domain.NormalizeHumanActionKind(claims.ActionKind) {
		return NormalizedEvent{}, fmt.Errorf("action kind mismatch")
	}
	if in.DecisionHash == "" || in.DecisionHash != claims.DecisionHash {
		return NormalizedEvent{}, fmt.Errorf("decision hash mismatch")
	}

	in.EventType = domain.EventTypeExternalEventIngested
	in.SourceKind = "human_action"
	in.TransportKind = transportKind
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
	return in, nil
}

func isHumanActionAuthError(err error) bool {
	if err == nil {
		return false
	}
	if err == domain.ErrUnauthorized || err == domain.ErrInvalidToken || err == domain.ErrTokenExpired {
		return true
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "mismatch")
}

// verifyHumanActionToken verifies a JWT token using golang-jwt/jwt/v5.
func (h *HTTPIngress) verifyHumanActionToken(tokenString string) (*domain.HumanActionClaims, error) {
	if len(h.humanActionSecret) == 0 {
		return nil, domain.ErrUnauthorized
	}

	if strings.HasPrefix(tokenString, "v1.") {
		return h.verifyHumanActionTokenV1(tokenString)
	}

	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, domain.ErrInvalidToken
		}
		return h.humanActionSecret, nil
	})
	if err != nil {
		return nil, domain.ErrInvalidToken
	}

	if !token.Valid {
		return nil, domain.ErrInvalidToken
	}

	mapClaims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, domain.ErrInvalidToken
	}

	claims, err := domain.HumanActionClaimsFromMap(mapClaims)
	if err != nil {
		return nil, err
	}

	if claims.ExpiresAt.IsZero() || time.Now().UTC().After(claims.ExpiresAt) {
		return nil, domain.ErrTokenExpired
	}
	if claims.DecisionHash == "" || claims.Nonce == "" {
		return nil, domain.ErrInvalidToken
	}
	if err := domain.ValidateHumanActionClaims(claims); err != nil {
		return nil, err
	}

	claims.ActionKind = string(domain.NormalizeHumanActionKind(claims.ActionKind))
	return &claims, nil
}

func (h *HTTPIngress) verifyHumanActionTokenV1(tokenString string) (*domain.HumanActionClaims, error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 || parts[0] != "v1" {
		return nil, domain.ErrInvalidToken
	}
	payloadPart := parts[1]
	sigPart := parts[2]

	gotSig, err := base64.RawURLEncoding.DecodeString(sigPart)
	if err != nil {
		return nil, domain.ErrInvalidToken
	}
	mac := hmac.New(sha256.New, h.humanActionSecret)
	_, _ = mac.Write([]byte(payloadPart))
	expectedSig := mac.Sum(nil)
	if !hmac.Equal(gotSig, expectedSig) {
		return nil, domain.ErrInvalidToken
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(payloadPart)
	if err != nil {
		return nil, domain.ErrInvalidToken
	}
	var claims domain.HumanActionClaims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, domain.ErrInvalidToken
	}
	if claims.ExpiresAt.IsZero() || time.Now().UTC().After(claims.ExpiresAt) {
		return nil, domain.ErrTokenExpired
	}
	if claims.DecisionHash == "" || claims.Nonce == "" {
		return nil, domain.ErrInvalidToken
	}
	if err := domain.ValidateHumanActionClaims(claims); err != nil {
		return nil, err
	}
	claims.ActionKind = string(domain.NormalizeHumanActionKind(claims.ActionKind))
	return &claims, nil
}
