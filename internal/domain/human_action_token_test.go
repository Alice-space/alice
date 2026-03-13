package domain

import (
	"testing"
	"time"
)

func TestSignAndVerifyHumanActionTokenV1(t *testing.T) {
	secret := []byte("token-secret")
	expiresAt := time.Date(2026, 3, 13, 16, 0, 0, 0, time.UTC)
	token, err := SignHumanActionTokenV1(secret, HumanActionClaims{
		ActionKind:        string(HumanActionApprove),
		TaskID:            "task_1",
		ApprovalRequestID: "apr_1",
		StepExecutionID:   "exec_1",
		DecisionHash:      "hash_1",
		Nonce:             "nonce_1",
		ExpiresAt:         expiresAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := VerifyHumanActionTokenV1(secret, token, expiresAt.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if claims.ActionKind != string(HumanActionApprove) {
		t.Fatalf("unexpected action kind: %s", claims.ActionKind)
	}
	if claims.TaskID != "task_1" || claims.ApprovalRequestID != "apr_1" || claims.StepExecutionID != "exec_1" {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}
