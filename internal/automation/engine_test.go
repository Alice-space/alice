package automation

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/Alice-space/alice/internal/llm"
)

type senderStub struct {
	mu              sync.Mutex
	sendTextCalls   int
	sendCardCalls   int
	lastReceiveType string
	lastReceiveID   string
	lastText        string
	lastCard        string
}

func (s *senderStub) SendText(_ context.Context, receiveIDType, receiveID, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sendTextCalls++
	s.lastReceiveType = receiveIDType
	s.lastReceiveID = receiveID
	s.lastText = text
	return nil
}

func (s *senderStub) SendCard(_ context.Context, receiveIDType, receiveID, cardContent string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sendCardCalls++
	s.lastReceiveType = receiveIDType
	s.lastReceiveID = receiveID
	s.lastCard = cardContent
	return nil
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
