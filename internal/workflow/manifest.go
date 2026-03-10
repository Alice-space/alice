package workflow

type Manifest struct {
	WorkflowID     string     `yaml:"workflow_id" json:"workflow_id"`
	WorkflowSource string     `yaml:"workflow_source" json:"workflow_source"`
	WorkflowRev    string     `yaml:"workflow_rev" json:"workflow_rev"`
	Entry          EntrySpec  `yaml:"entry" json:"entry"`
	Steps          []StepSpec `yaml:"steps" json:"steps"`
	Gates          []GateSpec `yaml:"gates,omitempty" json:"gates,omitempty"`
}

type EntrySpec struct {
	Requires     []string `yaml:"requires,omitempty" json:"requires,omitempty"`
	RequiredRefs []string `yaml:"required_refs,omitempty" json:"required_refs,omitempty"`
	Forbids      []string `yaml:"forbids,omitempty" json:"forbids,omitempty"`
	AllowedMCP   []string `yaml:"allowed_mcp,omitempty" json:"allowed_mcp,omitempty"`
	AllowedTools []string `yaml:"allowed_tools,omitempty" json:"allowed_tools,omitempty"`
	MaxRisk      string   `yaml:"max_risk,omitempty" json:"max_risk,omitempty"`
}

type IOSpec struct {
	Family   string `yaml:"family" json:"family"`
	SchemaID string `yaml:"schema_id" json:"schema_id"`
	Required bool   `yaml:"required" json:"required"`
}

type StepSpec struct {
	ID              string   `yaml:"id" json:"id"`
	RunnerKind      string   `yaml:"runner_kind" json:"runner_kind"`
	Slot            string   `yaml:"slot" json:"slot"`
	Role            string   `yaml:"role,omitempty" json:"role,omitempty"`
	Inputs          []IOSpec `yaml:"inputs,omitempty" json:"inputs,omitempty"`
	Outputs         []IOSpec `yaml:"outputs,omitempty" json:"outputs,omitempty"`
	AllowedTools    []string `yaml:"allowed_tools,omitempty" json:"allowed_tools,omitempty"`
	AllowedMCP      []string `yaml:"allowed_mcp,omitempty" json:"allowed_mcp,omitempty"`
	SandboxTemplate string   `yaml:"sandbox_template,omitempty" json:"sandbox_template,omitempty"`
	TimeoutSeconds  int      `yaml:"timeout_seconds,omitempty" json:"timeout_seconds,omitempty"`
	MaxRetries      int      `yaml:"max_retries,omitempty" json:"max_retries,omitempty"`
	OnSuccess       string   `yaml:"on_success,omitempty" json:"on_success,omitempty"`
	OnFailure       string   `yaml:"on_failure,omitempty" json:"on_failure,omitempty"`
}

type EvaluationRule struct {
	Metric    string  `yaml:"metric" json:"metric"`
	Op        string  `yaml:"op" json:"op"`
	Threshold float64 `yaml:"threshold" json:"threshold"`
}

type GateSpec struct {
	ID             string           `yaml:"id" json:"id"`
	Type           string           `yaml:"type" json:"type"`
	AttachAfter    string           `yaml:"attach_after,omitempty" json:"attach_after,omitempty"`
	AttachBefore   string           `yaml:"attach_before,omitempty" json:"attach_before,omitempty"`
	RequiredSlots  []string         `yaml:"required_slots,omitempty" json:"required_slots,omitempty"`
	TimeoutSeconds int              `yaml:"timeout_seconds,omitempty" json:"timeout_seconds,omitempty"`
	ResultFamily   string           `yaml:"result_family,omitempty" json:"result_family,omitempty"`
	Rules          []EvaluationRule `yaml:"rules,omitempty" json:"rules,omitempty"`
	Aggregate      string           `yaml:"aggregate,omitempty" json:"aggregate,omitempty"`
	OnApprove      string           `yaml:"on_approve,omitempty" json:"on_approve,omitempty"`
	OnReject       string           `yaml:"on_reject,omitempty" json:"on_reject,omitempty"`
	OnPass         string           `yaml:"on_pass,omitempty" json:"on_pass,omitempty"`
	OnFail         string           `yaml:"on_fail,omitempty" json:"on_fail,omitempty"`
	OnError        string           `yaml:"on_error,omitempty" json:"on_error,omitempty"`
	OnExpire       string           `yaml:"on_expire,omitempty" json:"on_expire,omitempty"`
}

type ManifestRef struct {
	WorkflowID     string `json:"workflow_id"`
	WorkflowSource string `json:"workflow_source"`
	WorkflowRev    string `json:"workflow_rev"`
	ManifestDigest string `json:"manifest_digest"`
	ManifestPath   string `json:"manifest_path"`
}
