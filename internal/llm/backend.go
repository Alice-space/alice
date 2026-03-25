package llm

import "context"

type ProgressFunc func(step string)

type Usage struct {
	InputTokens       int64
	CachedInputTokens int64
	OutputTokens      int64
}

func (u Usage) TotalTokens() int64 {
	return u.InputTokens + u.OutputTokens
}

func (u Usage) HasUsage() bool {
	return u.InputTokens != 0 || u.CachedInputTokens != 0 || u.OutputTokens != 0
}

type ExecPolicyConfig struct {
	Sandbox        string
	AskForApproval string
	AddDirs        []string
}

type RunRequest struct {
	ThreadID        string
	AgentName       string
	UserText        string
	Scene           string
	Provider        string
	Model           string
	Profile         string
	ReasoningEffort string
	Personality     string
	NoReplyToken    string
	PromptPrefix    string
	ExecPolicy      ExecPolicyConfig
	Env             map[string]string
	OnProgress      ProgressFunc
}

type RunResult struct {
	Reply        string
	NextThreadID string
	Usage        Usage
}

type Backend interface {
	Run(ctx context.Context, req RunRequest) (RunResult, error)
}

type Provider interface {
	Backend() Backend
}
