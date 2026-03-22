package automation

import (
	"context"
	"errors"
	"strings"

	"github.com/Alice-space/alice/internal/llm"
)

const (
	EnvWorkflowName       = "ALICE_AUTOMATION_WORKFLOW"
	EnvWorkflowStateKey   = "ALICE_AUTOMATION_STATE_KEY"
	EnvWorkflowTaskID     = "ALICE_AUTOMATION_TASK_ID"
	EnvWorkflowSessionKey = "ALICE_AUTOMATION_SESSION_KEY"
)

type WorkflowRunRequest struct {
	Workflow        string
	TaskID          string
	StateKey        string
	SessionKey      string
	Scene           string
	Prompt          string
	Provider        string
	Model           string
	Profile         string
	ReasoningEffort string
	Personality     string
	Env             map[string]string
}

type WorkflowRunResult struct {
	Message string
}

type WorkflowRunner interface {
	Run(ctx context.Context, req WorkflowRunRequest) (WorkflowRunResult, error)
}

type PromptWorkflowRunner struct {
	backend llm.Backend
}

func NewPromptWorkflowRunner(backend llm.Backend) *PromptWorkflowRunner {
	return &PromptWorkflowRunner{backend: backend}
}

func (r *PromptWorkflowRunner) Run(ctx context.Context, req WorkflowRunRequest) (WorkflowRunResult, error) {
	if r == nil || r.backend == nil {
		return WorkflowRunResult{}, errors.New("workflow backend is nil")
	}
	workflow := normalizeWorkflowName(req.Workflow)
	if workflow == "" {
		return WorkflowRunResult{}, errors.New("workflow name is empty")
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return WorkflowRunResult{}, errors.New("workflow prompt is empty")
	}

	env := cloneWorkflowEnv(req.Env)
	env[EnvWorkflowName] = workflow
	if taskID := strings.TrimSpace(req.TaskID); taskID != "" {
		env[EnvWorkflowTaskID] = taskID
	}
	if stateKey := strings.TrimSpace(req.StateKey); stateKey != "" {
		env[EnvWorkflowStateKey] = stateKey
	}
	if sessionKey := strings.TrimSpace(req.SessionKey); sessionKey != "" {
		env[EnvWorkflowSessionKey] = sessionKey
	}

	result, err := r.backend.Run(ctx, llm.RunRequest{
		AgentName:       workflowAgentName(workflow),
		UserText:        prompt,
		Scene:           normalizeWorkflowScene(req.Scene, req.SessionKey),
		Provider:        strings.TrimSpace(req.Provider),
		Model:           strings.TrimSpace(req.Model),
		Profile:         strings.TrimSpace(req.Profile),
		ReasoningEffort: strings.TrimSpace(req.ReasoningEffort),
		Personality:     strings.TrimSpace(req.Personality),
		Env:             env,
	})
	if err != nil {
		return WorkflowRunResult{}, err
	}
	return WorkflowRunResult{Message: strings.TrimSpace(result.Reply)}, nil
}

func normalizeWorkflowName(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func workflowAgentName(workflow string) string {
	workflow = normalizeWorkflowName(workflow)
	if workflow == "" {
		return "workflow"
	}
	return "workflow/" + workflow
}

func cloneWorkflowEnv(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in)+4)
	for key, value := range in {
		trimmedKey := strings.TrimSpace(key)
		trimmedValue := strings.TrimSpace(value)
		if trimmedKey == "" || trimmedValue == "" {
			continue
		}
		out[trimmedKey] = trimmedValue
	}
	return out
}

func normalizeWorkflowScene(scene, sessionKey string) string {
	scene = strings.ToLower(strings.TrimSpace(scene))
	if scene == "chat" || scene == "work" {
		return scene
	}
	if strings.Contains(strings.TrimSpace(sessionKey), "|scene:work") {
		return "work"
	}
	return "chat"
}
