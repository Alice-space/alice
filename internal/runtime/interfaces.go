package runtime

import (
	"context"

	"github.com/Alice-space/alice/internal/domain"
)

type StructuredModel interface {
	CompleteJSON(ctx context.Context, profile string, instruction string, input map[string]any) (map[string]any, error)
}

type IntentCompiler interface {
	Compile(ctx context.Context, actorID, channel, text string) (domain.IntentSpec, error)
}

type TaskClassifier interface {
	Classify(ctx context.Context, task domain.TaskSpec, memory []domain.MemoryRecord) (domain.TaskClassifiedResult, error)
}

type ModelRouter interface {
	Route(task domain.TaskSpec, classified domain.TaskClassifiedResult) (RuntimeType, string, error)
}

type RuntimeType string

const (
	RuntimeTypeFastSingle RuntimeType = "FastSingleAgentRuntime"
	RuntimeTypeAgentic    RuntimeType = "AgenticRuntime"
	RuntimeTypeLLM        RuntimeType = "LLMRuntime"
)

type RuntimeSession interface {
	Type() RuntimeType
	Plan(ctx context.Context, pkt domain.ContextPacket) (domain.RuntimeResult, error)
	Act(ctx context.Context, pkt domain.ContextPacket) (domain.RuntimeResult, error)
	Review(ctx context.Context, pkt domain.ContextPacket) (domain.RuntimeResult, error)
	Summarize(ctx context.Context, pkt domain.ContextPacket) (domain.RuntimeResult, error)
	ClassifyCapability(ctx context.Context, pkt domain.ContextPacket) ([]string, error)
}

type SessionFactory interface {
	NewSession(rt RuntimeType, modelProfile string) (RuntimeSession, error)
}
