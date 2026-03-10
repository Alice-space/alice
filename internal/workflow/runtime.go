package workflow

import (
	"context"
	"fmt"
	"time"

	"alice/internal/domain"
)

type DispatchResult struct {
	Status             domain.StepStatus `json:"status"`
	CheckpointRef      string            `json:"checkpoint_ref"`
	ResumeToken        string            `json:"resume_token"`
	RemoteExecutionRef string            `json:"remote_execution_ref"`
	OutputArtifactRefs []string          `json:"output_artifact_refs"`
	FailureCode        string            `json:"failure_code"`
	FailureMessage     string            `json:"failure_message"`
	LastHeartbeatAt    time.Time         `json:"last_heartbeat_at"`
	LeaseExpiresAt     time.Time         `json:"lease_expires_at"`
}

type StepRunner interface {
	Start(ctx context.Context, exec *domain.StepExecution) (*DispatchResult, error)
	Resume(ctx context.Context, exec *domain.StepExecution) (*DispatchResult, error)
	Cancel(ctx context.Context, exec *domain.StepExecution) (*DispatchResult, error)
}

type Runtime struct {
	registry *Registry
}

func NewRuntime(registry *Registry) *Runtime {
	return &Runtime{registry: registry}
}

func (r *Runtime) Registry() *Registry {
	return r.registry
}

func (r *Runtime) Bind(ctx context.Context, workflowID, rev string) (*ManifestRef, error) {
	m, err := r.registry.Load(ctx, workflowID, rev)
	if err != nil {
		return nil, err
	}
	ref, ok := r.registry.Reference(m.WorkflowID, m.WorkflowRev)
	if !ok {
		return nil, fmt.Errorf("manifest reference not found: %s@%s", workflowID, rev)
	}
	return &ref, nil
}

func (r *Runtime) OpenGate(gate GateSpec) (*domain.ApprovalRequest, *domain.HumanWaitRecord, error) {
	switch gate.Type {
	case string(domain.GateTypeApproval), string(domain.GateTypeConfirmation), string(domain.GateTypeBudget):
		return &domain.ApprovalRequest{GateType: domain.GateType(gate.Type), Status: domain.GateStatusOpen}, nil, nil
	case string(domain.GateTypeEvaluation):
		// evaluation gate is auto-evaluated; no ApprovalRequest by default.
		return nil, nil, nil
	default:
		return nil, nil, fmt.Errorf("unsupported gate type: %s", gate.Type)
	}
}

func (r *Runtime) EvaluateGate(gate GateSpec, metrics map[string]float64) (bool, error) {
	if gate.Type != string(domain.GateTypeEvaluation) {
		return false, fmt.Errorf("gate %s is not evaluation", gate.ID)
	}
	if len(gate.Rules) == 0 {
		return false, fmt.Errorf("evaluation gate %s has no rules", gate.ID)
	}
	results := make([]bool, 0, len(gate.Rules))
	for _, rule := range gate.Rules {
		value, ok := metrics[rule.Metric]
		if !ok {
			return false, fmt.Errorf("missing metric %s", rule.Metric)
		}
		switch rule.Op {
		case "gte":
			results = append(results, value >= rule.Threshold)
		case "gt":
			results = append(results, value > rule.Threshold)
		case "lte":
			results = append(results, value <= rule.Threshold)
		case "lt":
			results = append(results, value < rule.Threshold)
		case "eq":
			results = append(results, value == rule.Threshold)
		default:
			return false, fmt.Errorf("unsupported op: %s", rule.Op)
		}
	}
	if gate.Aggregate == "any" {
		for _, result := range results {
			if result {
				return true, nil
			}
		}
		return false, nil
	}
	for _, result := range results {
		if !result {
			return false, nil
		}
	}
	return true, nil
}
