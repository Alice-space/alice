package automation

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Alice-space/alice/internal/llm"
)

type senderStub struct {
	mu                sync.Mutex
	sendTextCalls     int
	sendCardCalls     int
	urgentAppCalls    int
	lastReceiveType   string
	lastReceiveID     string
	lastText          string
	lastCard          string
	urgentMessageID   string
	urgentUserIDType  string
	urgentUserIDs     []string
	sendTextErr       error
	sendCardErr       error
	sendTextMessageID string
	sendCardMessageID string
	urgentAppErr      error
}

func (s *senderStub) SendText(_ context.Context, receiveIDType, receiveID, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sendTextCalls++
	s.lastReceiveType = receiveIDType
	s.lastReceiveID = receiveID
	s.lastText = text
	return s.sendTextErr
}

func (s *senderStub) SendCard(_ context.Context, receiveIDType, receiveID, cardContent string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sendCardCalls++
	s.lastReceiveType = receiveIDType
	s.lastReceiveID = receiveID
	s.lastCard = cardContent
	return s.sendCardErr
}

func (s *senderStub) SendTextMessage(ctx context.Context, receiveIDType, receiveID, text string) (string, error) {
	if err := s.SendText(ctx, receiveIDType, receiveID, text); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(s.sendTextMessageID) != "" {
		return s.sendTextMessageID, nil
	}
	return "om_text", nil
}

func (s *senderStub) SendCardMessage(ctx context.Context, receiveIDType, receiveID, cardContent string) (string, error) {
	if err := s.SendCard(ctx, receiveIDType, receiveID, cardContent); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(s.sendCardMessageID) != "" {
		return s.sendCardMessageID, nil
	}
	return "om_card", nil
}

func (s *senderStub) UrgentApp(_ context.Context, messageID, userIDType string, userIDs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.urgentAppCalls++
	s.urgentMessageID = messageID
	s.urgentUserIDType = userIDType
	s.urgentUserIDs = append([]string(nil), userIDs...)
	return s.urgentAppErr
}

func cardTitleFromJSON(t *testing.T, raw string) string {
	t.Helper()
	var card struct {
		Header struct {
			Title struct {
				Content string `json:"content"`
			} `json:"title"`
		} `json:"header"`
	}
	if err := json.Unmarshal([]byte(raw), &card); err != nil {
		t.Fatalf("unmarshal card failed: %v, raw=%q", err, raw)
	}
	return card.Header.Title.Content
}

type deadlineSenderStub struct {
	mu          sync.Mutex
	deadlineSet bool
	deadline    time.Time
}

func (s *deadlineSenderStub) SendText(ctx context.Context, _, _, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if deadline, ok := ctx.Deadline(); ok {
		s.deadlineSet = true
		s.deadline = deadline
	}
	return nil
}

func (s *deadlineSenderStub) SendCard(ctx context.Context, _, _, _ string) error {
	return s.SendText(ctx, "", "", "")
}

func TestTaskUrgentRecipient_PrefersOpenID(t *testing.T) {
	userIDType, userID, ok := taskUrgentRecipient(Actor{
		UserID: "u_actor",
		OpenID: "ou_actor",
	})
	if !ok {
		t.Fatal("expected urgent recipient to resolve")
	}
	if userIDType != "open_id" {
		t.Fatalf("expected open_id, got %q", userIDType)
	}
	if userID != "ou_actor" {
		t.Fatalf("unexpected urgent recipient id: %q", userID)
	}
}

type llmRunnerStub struct {
	mu      sync.Mutex
	calls   int
	lastReq llm.RunRequest
	result  llm.RunResult
	err     error
}

func (s *llmRunnerStub) Run(_ context.Context, req llm.RunRequest) (llm.RunResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.lastReq = req
	return s.result, s.err
}

type workflowRunnerStub struct {
	mu          sync.Mutex
	calls       int
	lastReq     WorkflowRunRequest
	result      WorkflowRunResult
	err         error
	deadlineSet bool
	deadline    time.Time
}

func (s *workflowRunnerStub) Run(ctx context.Context, req WorkflowRunRequest) (WorkflowRunResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.lastReq = req
	if deadline, ok := ctx.Deadline(); ok {
		s.deadlineSet = true
		s.deadline = deadline
	}
	return s.result, s.err
}
