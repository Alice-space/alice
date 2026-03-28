package automation

import (
	"context"
	"testing"

	"github.com/Alice-space/alice/internal/llm"
)

type workflowBackendStub struct {
	result llm.RunResult
	err    error
	req    llm.RunRequest
}

func (s workflowBackendStub) Run(_ context.Context, req llm.RunRequest) (llm.RunResult, error) {
	s.req = req
	return s.result, s.err
}

func TestPromptWorkflowRunner_ExtractsHiddenCommands(t *testing.T) {
	backend := workflowBackendStub{
		result: llm.RunResult{
			Reply: "可见回复\n\n<alice_command>/alice needs-human waiting for approval</alice_command>",
		},
	}
	runner := NewPromptWorkflowRunner(backend)

	result, err := runner.Run(context.Background(), WorkflowRunRequest{
		Workflow: "code_army",
		Prompt:   "run",
	})
	if err != nil {
		t.Fatalf("run workflow failed: %v", err)
	}
	if result.Message != "可见回复" {
		t.Fatalf("unexpected visible message: %q", result.Message)
	}
	if len(result.Commands) != 1 {
		t.Fatalf("expected one command, got %#v", result.Commands)
	}
	if result.Commands[0].Text != "/alice needs-human waiting for approval" {
		t.Fatalf("unexpected command: %#v", result.Commands[0])
	}
}

type capturingWorkflowBackendStub struct {
	lastReq llm.RunRequest
	result  llm.RunResult
	err     error
}

func (s *capturingWorkflowBackendStub) Run(_ context.Context, req llm.RunRequest) (llm.RunResult, error) {
	s.lastReq = req
	return s.result, s.err
}

func TestPromptWorkflowRunner_ForwardsStateKeyAsThreadID_ForSupportedProviders(t *testing.T) {
	backend := &capturingWorkflowBackendStub{
		result: llm.RunResult{Reply: "done"},
	}
	runner := NewPromptWorkflowRunner(backend)

	_, err := runner.Run(context.Background(), WorkflowRunRequest{
		Workflow:   "code_army",
		Prompt:     "run",
		Provider:   llm.ProviderCodex,
		StateKey:   "campaign_dispatch:camp_demo:executor:T001:x1",
		SessionKey: "chat_id:oc_chat|scene:work|thread:omt_alpha",
	})
	if err != nil {
		t.Fatalf("run workflow failed: %v", err)
	}
	if backend.lastReq.ThreadID != "campaign_dispatch:camp_demo:executor:T001:x1" {
		t.Fatalf("unexpected workflow thread id: %q", backend.lastReq.ThreadID)
	}
}

func TestPromptWorkflowRunner_DoesNotForwardSyntheticStateKeyToClaude(t *testing.T) {
	backend := &capturingWorkflowBackendStub{
		result: llm.RunResult{Reply: "done"},
	}
	runner := NewPromptWorkflowRunner(backend)

	_, err := runner.Run(context.Background(), WorkflowRunRequest{
		Workflow:   "code_army",
		Prompt:     "run",
		Provider:   llm.ProviderClaude,
		StateKey:   "campaign_dispatch:camp_demo:planner:r1",
		SessionKey: "chat_id:oc_chat|scene:work|thread:omt_alpha",
	})
	if err != nil {
		t.Fatalf("run workflow failed: %v", err)
	}
	if backend.lastReq.ThreadID != "" {
		t.Fatalf("claude should not receive synthetic workflow thread id, got %q", backend.lastReq.ThreadID)
	}
}
