package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"alice/internal/domain"
	"gopkg.in/yaml.v3"
)

type SchemaValidator interface {
	ValidateManifest(ctx context.Context, manifest *Manifest, rawYAML []byte) error
}

type Registry struct {
	validator SchemaValidator
	manifests map[string]*loadedManifest
}

type loadedManifest struct {
	ref      ManifestRef
	manifest *Manifest
	stepMap  map[string]StepSpec
	gateMap  map[string]GateSpec
}

func NewRegistry(validator SchemaValidator) *Registry {
	return &Registry{
		validator: validator,
		manifests: map[string]*loadedManifest{},
	}
}

func (r *Registry) LoadRoots(ctx context.Context, roots []string) error {
	for _, root := range roots {
		if root == "" {
			continue
		}
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || d.Name() != "manifest.yaml" {
				return nil
			}
			_, loadErr := r.loadManifest(ctx, path)
			return loadErr
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *Registry) Load(ctx context.Context, workflowID string, rev string) (*Manifest, error) {
	key := makeManifestKey(workflowID, rev)
	m, ok := r.manifests[key]
	if !ok {
		return nil, fmt.Errorf("workflow not found: %s", key)
	}
	cp := *m.manifest
	return &cp, nil
}

func (r *Registry) ResolveCandidate(_ context.Context, decision *domain.PromotionDecision, evt *domain.ExternalEvent) ([]ManifestRef, error) {
	if decision == nil || evt == nil {
		return nil, fmt.Errorf("decision and event are required")
	}
	refs := make([]ManifestRef, 0, len(r.manifests))
	for _, m := range r.manifests {
		if decision.IntentKind != "schedule_trigger" && !matchesControlPlanePriority(m.manifest.WorkflowID, evt) {
			continue
		}
		if len(decision.ProposedWorkflowIDs) > 0 && !contains(decision.ProposedWorkflowIDs, m.manifest.WorkflowID) {
			continue
		}
		if decision.IntentKind != "schedule_trigger" {
			if !entryRequiresSatisfied(m.manifest.Entry, evt) {
				continue
			}
			if !entryRefsSatisfied(m.manifest.Entry, evt) {
				continue
			}
			if entryForbidsHit(m.manifest.Entry, evt) {
				continue
			}
			if !entryMCPAllowed(m.manifest.Entry, decisionRequiredMCP(decision)) {
				continue
			}
			if !entryToolsAllowed(m.manifest.Entry, decisionRequiredTools(decision)) {
				continue
			}
		}
		if !riskAllowed(decision.RiskLevel, m.manifest.Entry.MaxRisk) {
			continue
		}
		refs = append(refs, m.ref)
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].WorkflowID == refs[j].WorkflowID {
			return refs[i].WorkflowRev < refs[j].WorkflowRev
		}
		return refs[i].WorkflowID < refs[j].WorkflowID
	})
	return refs, nil
}

func UniqueCandidate(candidates []ManifestRef) (ManifestRef, bool) {
	if len(candidates) != 1 {
		return ManifestRef{}, false
	}
	return candidates[0], true
}

func (r *Registry) Reference(workflowID string, rev string) (ManifestRef, bool) {
	m, ok := r.manifests[makeManifestKey(workflowID, rev)]
	if !ok {
		return ManifestRef{}, false
	}
	return m.ref, true
}

func (r *Registry) loadManifest(ctx context.Context, path string) (ManifestRef, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ManifestRef{}, fmt.Errorf("read manifest %s: %w", path, err)
	}
	var manifest Manifest
	if err := yaml.Unmarshal(raw, &manifest); err != nil {
		return ManifestRef{}, fmt.Errorf("yaml parse %s: %w", path, err)
	}
	if err := validateManifestStructure(&manifest); err != nil {
		return ManifestRef{}, fmt.Errorf("validate manifest %s: %w", path, err)
	}
	if r.validator != nil {
		if err := r.validator.ValidateManifest(ctx, &manifest, raw); err != nil {
			return ManifestRef{}, fmt.Errorf("schema validation %s: %w", path, err)
		}
	}
	canonical, digest, err := canonicalDigest(&manifest)
	if err != nil {
		return ManifestRef{}, err
	}
	_ = canonical // intentionally kept for future schema audit.

	ref := ManifestRef{
		WorkflowID:     manifest.WorkflowID,
		WorkflowSource: manifest.WorkflowSource,
		WorkflowRev:    manifest.WorkflowRev,
		ManifestDigest: digest,
		ManifestPath:   path,
	}
	r.manifests[makeManifestKey(manifest.WorkflowID, manifest.WorkflowRev)] = &loadedManifest{
		ref:      ref,
		manifest: &manifest,
		stepMap:  stepMap(manifest.Steps),
		gateMap:  gateMap(manifest.Gates),
	}
	return ref, nil
}

func validateManifestStructure(m *Manifest) error {
	if m.WorkflowID == "" || m.WorkflowSource == "" || m.WorkflowRev == "" {
		return fmt.Errorf("workflow_id/workflow_source/workflow_rev are required")
	}
	if len(m.Steps) == 0 {
		return fmt.Errorf("steps must not be empty")
	}
	steps := stepMap(m.Steps)
	if len(steps) != len(m.Steps) {
		return fmt.Errorf("duplicated step id")
	}
	for _, s := range m.Steps {
		if s.ID == "" || s.RunnerKind == "" || s.Slot == "" {
			return fmt.Errorf("step %q missing id/runner_kind/slot", s.ID)
		}
		if s.OnSuccess != "" {
			if _, ok := steps[s.OnSuccess]; !ok {
				return fmt.Errorf("step %q on_success target missing: %s", s.ID, s.OnSuccess)
			}
		}
		if s.OnFailure != "" {
			if _, ok := steps[s.OnFailure]; !ok {
				return fmt.Errorf("step %q on_failure target missing: %s", s.ID, s.OnFailure)
			}
		}
	}
	for _, g := range m.Gates {
		if g.ID == "" || g.Type == "" {
			return fmt.Errorf("gate missing id/type")
		}
		if g.AttachAfter == "" && g.AttachBefore == "" {
			return fmt.Errorf("gate %q must declare attach_after or attach_before", g.ID)
		}
		if g.AttachAfter != "" {
			if _, ok := steps[g.AttachAfter]; !ok {
				return fmt.Errorf("gate %q attach_after target missing: %s", g.ID, g.AttachAfter)
			}
		}
		if g.AttachBefore != "" {
			if _, ok := steps[g.AttachBefore]; !ok {
				return fmt.Errorf("gate %q attach_before target missing: %s", g.ID, g.AttachBefore)
			}
		}
	}
	return nil
}

func canonicalDigest(m *Manifest) ([]byte, string, error) {
	cp := *m
	cp.Steps = append([]StepSpec(nil), m.Steps...)
	cp.Gates = append([]GateSpec(nil), m.Gates...)
	sort.Slice(cp.Steps, func(i, j int) bool { return cp.Steps[i].ID < cp.Steps[j].ID })
	sort.Slice(cp.Gates, func(i, j int) bool { return cp.Gates[i].ID < cp.Gates[j].ID })
	for i := range cp.Steps {
		sort.Strings(cp.Steps[i].AllowedTools)
		sort.Strings(cp.Steps[i].AllowedMCP)
	}
	for i := range cp.Gates {
		sort.Strings(cp.Gates[i].RequiredSlots)
		sort.Slice(cp.Gates[i].Rules, func(a, b int) bool {
			if cp.Gates[i].Rules[a].Metric == cp.Gates[i].Rules[b].Metric {
				return cp.Gates[i].Rules[a].Op < cp.Gates[i].Rules[b].Op
			}
			return cp.Gates[i].Rules[a].Metric < cp.Gates[i].Rules[b].Metric
		})
	}
	sort.Strings(cp.Entry.Requires)
	sort.Strings(cp.Entry.RequiredRefs)
	sort.Strings(cp.Entry.Forbids)
	sort.Strings(cp.Entry.AllowedMCP)
	sort.Strings(cp.Entry.AllowedTools)
	canonical, err := json.Marshal(cp)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(canonical)
	return canonical, hex.EncodeToString(sum[:]), nil
}

func stepMap(steps []StepSpec) map[string]StepSpec {
	out := make(map[string]StepSpec, len(steps))
	for _, s := range steps {
		out[s.ID] = s
	}
	return out
}

func gateMap(gates []GateSpec) map[string]GateSpec {
	out := make(map[string]GateSpec, len(gates))
	for _, g := range gates {
		out[g.ID] = g
	}
	return out
}

func makeManifestKey(workflowID string, rev string) string {
	return strings.TrimSpace(workflowID) + "@" + strings.TrimSpace(rev)
}

func contains(list []string, target string) bool {
	for _, v := range list {
		if v == target {
			return true
		}
	}
	return false
}

func matchesControlPlanePriority(workflowID string, evt *domain.ExternalEvent) bool {
	switch {
	case evt.ScheduledTaskID != "" || evt.ControlObjectRef != "":
		return workflowID == "schedule-management"
	case evt.WorkflowObjectRef != "":
		return workflowID == "workflow-management"
	default:
		return true
	}
}

func entryRefsSatisfied(entry EntrySpec, evt *domain.ExternalEvent) bool {
	if len(entry.RequiredRefs) == 0 {
		return true
	}
	for _, ref := range entry.RequiredRefs {
		if !externalEventHasRef(evt, ref) {
			return false
		}
	}
	return true
}

func entryRequiresSatisfied(entry EntrySpec, evt *domain.ExternalEvent) bool {
	if len(entry.Requires) == 0 {
		return true
	}
	for _, req := range entry.Requires {
		if !externalEventHasRef(evt, req) {
			return false
		}
	}
	return true
}

func entryForbidsHit(entry EntrySpec, evt *domain.ExternalEvent) bool {
	for _, f := range entry.Forbids {
		switch strings.ToLower(strings.TrimSpace(f)) {
		case "source:scheduler":
			if evt.SourceKind == "scheduler" {
				return true
			}
		case "source:human-action":
			if evt.SourceKind == "human_action" || evt.SourceKind == "human-action" {
				return true
			}
		case "control_object":
			if evt.ControlObjectRef != "" || evt.ScheduledTaskID != "" {
				return true
			}
		case "workflow_object":
			if evt.WorkflowObjectRef != "" {
				return true
			}
		}
	}
	return false
}

func entryMCPAllowed(entry EntrySpec, required []string) bool {
	if len(required) == 0 {
		return true
	}
	if len(entry.AllowedMCP) == 0 {
		return false
	}
	for _, req := range required {
		if !contains(entry.AllowedMCP, req) {
			return false
		}
	}
	return true
}

func entryToolsAllowed(entry EntrySpec, required []string) bool {
	if len(required) == 0 {
		return true
	}
	if len(entry.AllowedTools) == 0 {
		return false
	}
	for _, req := range required {
		if !contains(entry.AllowedTools, req) {
			return false
		}
	}
	return true
}

func decisionRequiredMCP(decision *domain.PromotionDecision) []string {
	switch decision.IntentKind {
	case "issue_delivery":
		return []string{"github"}
	case "research_exploration":
		return []string{"cluster"}
	case "schedule_management":
		return []string{"control"}
	case "workflow_management":
		return []string{"workflow-registry"}
	case "schedule_trigger":
		return nil
	default:
		if decision.ExternalWrite {
			return []string{"github"}
		}
		return nil
	}
}

func decisionRequiredTools(decision *domain.PromotionDecision) []string {
	switch decision.IntentKind {
	case "issue_delivery":
		return []string{"repo_write"}
	case "schedule_management":
		return []string{"schedule_parser"}
	case "workflow_management":
		return []string{"workflow_editor"}
	default:
		if decision.MultiStep {
			return []string{"repo_read"}
		}
		return nil
	}
}

func externalEventHasRef(evt *domain.ExternalEvent, ref string) bool {
	switch ref {
	case "repo_ref":
		return evt.RepoRef != ""
	case "issue_ref":
		return evt.IssueRef != ""
	case "pr_ref":
		return evt.PRRef != ""
	case "scheduled_task_id":
		return evt.ScheduledTaskID != ""
	case "control_object_ref":
		return evt.ControlObjectRef != ""
	case "workflow_object_ref":
		return evt.WorkflowObjectRef != ""
	default:
		return false
	}
}

func riskAllowed(decisionRisk string, maxRisk string) bool {
	if maxRisk == "" {
		return true
	}
	levels := map[string]int{
		"low":    1,
		"medium": 2,
		"high":   3,
	}
	dr := levels[strings.ToLower(decisionRisk)]
	mr := levels[strings.ToLower(maxRisk)]
	if dr == 0 || mr == 0 {
		return true
	}
	return dr <= mr
}
