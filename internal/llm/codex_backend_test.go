package llm

import "testing"

func TestCodexBackend_ResolveExecPolicyUsesProfileDefaults(t *testing.T) {
	backend := newCodexBackend(CodexConfig{
		DefaultExecPolicy: ExecPolicyConfig{
			Sandbox:        "workspace-write",
			AskForApproval: "never",
		},
		ProfileOverrides: map[string]ProfileRunnerConfig{
			"planner_reviewer": {
				ExecPolicy: ExecPolicyConfig{
					Sandbox:        "danger-full-access",
					AskForApproval: "never",
					AddDirs:        []string{"/tmp/campaign"},
				},
			},
		},
	}, nil)

	policy := backend.resolveExecPolicy(RunRequest{Profile: "planner_reviewer"})
	if policy.Sandbox != "danger-full-access" {
		t.Fatalf("unexpected sandbox: %q", policy.Sandbox)
	}
	if policy.AskForApproval != "never" {
		t.Fatalf("unexpected ask_for_approval: %q", policy.AskForApproval)
	}
	if len(policy.AddDirs) != 1 || policy.AddDirs[0] != "/tmp/campaign" {
		t.Fatalf("unexpected add_dirs: %#v", policy.AddDirs)
	}
}

func TestCodexBackend_ResolveExecPolicyMergesRequestOverrides(t *testing.T) {
	backend := newCodexBackend(CodexConfig{
		DefaultExecPolicy: ExecPolicyConfig{
			Sandbox:        "workspace-write",
			AskForApproval: "never",
		},
		ProfileOverrides: map[string]ProfileRunnerConfig{
			"reviewer": {
				ExecPolicy: ExecPolicyConfig{
					Sandbox:        "danger-full-access",
					AskForApproval: "never",
					AddDirs:        []string{"/tmp/campaign"},
				},
			},
		},
	}, nil)

	policy := backend.resolveExecPolicy(RunRequest{
		Profile: "reviewer",
		ExecPolicy: ExecPolicyConfig{
			AddDirs: []string{"/tmp/resources"},
		},
	})
	if policy.Sandbox != "danger-full-access" {
		t.Fatalf("unexpected sandbox: %q", policy.Sandbox)
	}
	if len(policy.AddDirs) != 2 {
		t.Fatalf("unexpected merged add_dirs: %#v", policy.AddDirs)
	}
	if policy.AddDirs[0] != "/tmp/campaign" || policy.AddDirs[1] != "/tmp/resources" {
		t.Fatalf("unexpected merged add_dirs order: %#v", policy.AddDirs)
	}
}
