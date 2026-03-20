package llm

import "context"

type ProgressFunc func(step string)

type RunRequest struct {
	ThreadID        string
	AgentName       string
	UserText        string
	Model           string
	Profile         string
	ReasoningEffort string
	Personality     string
	NoReplyToken    string
	Env             map[string]string
	OnProgress      ProgressFunc
}

type RunResult struct {
	Reply        string
	NextThreadID string
}

type Backend interface {
	Run(ctx context.Context, req RunRequest) (RunResult, error)
}

type Provider interface {
	Backend() Backend
}
