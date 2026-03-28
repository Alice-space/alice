package campaignrepo

import (
	"path/filepath"
	"testing"
)

func TestDefaultRoleConfig_UsesRuntimeDispatchedGenericRoles(t *testing.T) {
	tests := []struct {
		name         string
		cfg          RoleConfig
		wantRole     string
		wantWorkflow string
	}{
		{
			name:         "executor",
			cfg:          defaultExecutorRoleConfig(),
			wantRole:     "executor",
			wantWorkflow: "code_army",
		},
		{
			name:         "reviewer",
			cfg:          defaultReviewerRoleConfig(),
			wantRole:     "reviewer",
			wantWorkflow: "code_army",
		},
		{
			name:         "planner",
			cfg:          defaultPlannerRoleConfig(),
			wantRole:     "planner",
			wantWorkflow: "code_army",
		},
		{
			name:         "planner reviewer",
			cfg:          defaultPlannerReviewerRoleConfig(),
			wantRole:     "planner_reviewer",
			wantWorkflow: "code_army",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.cfg.Role != tt.wantRole {
				t.Fatalf("unexpected role: got=%q want=%q", tt.cfg.Role, tt.wantRole)
			}
			if tt.cfg.Provider != "" {
				t.Fatalf("default role should leave provider to runtime dispatch, got=%q", tt.cfg.Provider)
			}
			if tt.cfg.Model != "" {
				t.Fatalf("default role should not pin model, got=%q", tt.cfg.Model)
			}
			if tt.cfg.Profile != "" {
				t.Fatalf("default role should not pin profile, got=%q", tt.cfg.Profile)
			}
			if tt.cfg.Workflow != tt.wantWorkflow {
				t.Fatalf("unexpected workflow: got=%q want=%q", tt.cfg.Workflow, tt.wantWorkflow)
			}
			if tt.cfg.ReasoningEffort != "" {
				t.Fatalf("default role should not pin reasoning effort, got=%q", tt.cfg.ReasoningEffort)
			}
			if tt.cfg.Personality != "" {
				t.Fatalf("default role should not pin personality, got=%q", tt.cfg.Personality)
			}
		})
	}
}

func TestResolveRoleConfig_GenericRoleKeepsProviderEmpty(t *testing.T) {
	cfg := resolveRoleConfig(RoleConfig{}, RoleConfig{Role: "executor"}, "executor")
	if cfg.Role != "executor" {
		t.Fatalf("unexpected role: %q", cfg.Role)
	}
	if cfg.Provider != "" {
		t.Fatalf("generic role should not infer provider, got=%q", cfg.Provider)
	}
	if cfg.Workflow != "code_army" {
		t.Fatalf("unexpected workflow: %q", cfg.Workflow)
	}
}

func TestResolvePlannerRole_IgnoresDeprecatedCampaignFrontmatterSelectors(t *testing.T) {
	root := t.TempDir()
	mustWriteTestFile(t, filepath.Join(root, "campaign.md"), `---
campaign_id: camp_demo
title: "Demo Campaign"
default_planner:
  role: planner
  provider: codex
  model: gpt-5.4
  profile: work
  workflow: code_army
---
`)
	repo, err := Load(root)
	if err != nil {
		t.Fatalf("load repo failed: %v", err)
	}
	repo.ConfigRoleDefaults = CampaignRoleDefaults{
		Planner: RoleConfig{
			Role:        "planner",
			Provider:    "claude",
			Model:       "claude-opus-4-6",
			Profile:     "planner",
			Workflow:    "code_army",
			Personality: "pragmatic",
		},
	}

	cfg := resolvePlannerRole(repo)
	if cfg.Provider != "claude" {
		t.Fatalf("deprecated campaign provider should be ignored, got=%q", cfg.Provider)
	}
	if cfg.Model != "claude-opus-4-6" {
		t.Fatalf("deprecated campaign model should be ignored, got=%q", cfg.Model)
	}
	if cfg.Profile != "planner" {
		t.Fatalf("deprecated campaign profile should be ignored, got=%q", cfg.Profile)
	}
}

func TestResolvePlannerRole_DoesNotInventReasoningOrPersonality(t *testing.T) {
	repo := Repository{
		ConfigRoleDefaults: CampaignRoleDefaults{
			Planner: RoleConfig{
				Role:     "planner",
				Provider: "claude",
				Model:    "claude-opus-4-6",
				Profile:  "planner",
				Workflow: "code_army",
			},
		},
	}

	cfg := resolvePlannerRole(repo)
	if cfg.ReasoningEffort != "" {
		t.Fatalf("planner reasoning effort should stay empty unless explicitly configured, got=%q", cfg.ReasoningEffort)
	}
	if cfg.Personality != "" {
		t.Fatalf("planner personality should stay empty unless explicitly configured, got=%q", cfg.Personality)
	}
}
