package campaignrepo

import "testing"

func TestDefaultRoleConfig_UsesRuntimeDispatchedGenericRoles(t *testing.T) {
	tests := []struct {
		name            string
		cfg             RoleConfig
		wantRole        string
		wantWorkflow    string
		wantPersonality string
	}{
		{
			name:            "executor",
			cfg:             defaultExecutorRoleConfig(),
			wantRole:        "executor",
			wantWorkflow:    "code_army",
			wantPersonality: "pragmatic",
		},
		{
			name:            "reviewer",
			cfg:             defaultReviewerRoleConfig(),
			wantRole:        "reviewer",
			wantWorkflow:    "code_army",
			wantPersonality: "analytical",
		},
		{
			name:            "planner",
			cfg:             defaultPlannerRoleConfig(),
			wantRole:        "planner",
			wantWorkflow:    "code_army",
			wantPersonality: "analytical",
		},
		{
			name:            "planner reviewer",
			cfg:             defaultPlannerReviewerRoleConfig(),
			wantRole:        "planner_reviewer",
			wantWorkflow:    "code_army",
			wantPersonality: "analytical",
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
			if tt.cfg.Personality != tt.wantPersonality {
				t.Fatalf("unexpected personality: got=%q want=%q", tt.cfg.Personality, tt.wantPersonality)
			}
		})
	}
}

func TestResolveRoleConfig_GenericRoleKeepsProviderEmpty(t *testing.T) {
	cfg := resolveRoleConfig(RoleConfig{}, RoleConfig{Role: "executor"}, RoleConfig{}, "executor")
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
