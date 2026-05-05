package codex

import (
	"slices"
	"testing"
)

func TestBuildExecArgs_NewThread(t *testing.T) {
	args := buildExecArgs("", "hello world", "o4-mini", "", "medium", "", ExecPolicyConfig{
		Sandbox:        "workspace-write",
		AskForApproval: "never",
	})
	if !slices.Contains(args, "exec") {
		t.Fatalf("expected 'exec' subcommand, got %v", args)
	}
	if slices.Contains(args, "resume") {
		t.Fatalf("unexpected 'resume' for new thread, got %v", args)
	}
	if args[len(args)-1] != "hello world" {
		t.Fatalf("prompt must be last arg, got %v", args)
	}
}

func TestBuildExecArgs_ResumeThread(t *testing.T) {
	args := buildExecArgs("thread-123", "continue", "o4-mini", "", "", "", ExecPolicyConfig{
		Sandbox:        "workspace-write",
		AskForApproval: "never",
	})
	if !slices.Contains(args, "resume") {
		t.Fatalf("expected 'resume' subcommand, got %v", args)
	}
	if !slices.Contains(args, "thread-123") {
		t.Fatalf("expected thread id in args, got %v", args)
	}
}

func TestBuildExecArgs_DangerousFullAccess(t *testing.T) {
	args := buildExecArgs("", "prompt", "", "", "", "", ExecPolicyConfig{
		Sandbox:        "danger-full-access",
		AskForApproval: "never",
	})
	if !slices.Contains(args, "--dangerously-bypass-approvals-and-sandbox") {
		t.Fatalf("expected bypass flag for danger-full-access, got %v", args)
	}
	// should not set -a or --sandbox when using bypass
	for i, a := range args {
		if a == "-a" || a == "--sandbox" {
			t.Fatalf("unexpected flag %q at index %d with bypass mode, args=%v", a, i, args)
		}
	}
}

func TestBuildExecArgs_Model(t *testing.T) {
	args := buildExecArgs("", "prompt", "gpt-5.4", "", "", "", ExecPolicyConfig{})
	idx := slices.Index(args, "-m")
	if idx < 0 || args[idx+1] != "gpt-5.4" {
		t.Fatalf("expected -m gpt-5.4 in args, got %v", args)
	}
}

func TestBuildExecArgs_Profile(t *testing.T) {
	args := buildExecArgs("", "prompt", "", "my-profile", "", "", ExecPolicyConfig{})
	idx := slices.Index(args, "-p")
	if idx < 0 || args[idx+1] != "my-profile" {
		t.Fatalf("expected -p my-profile in args, got %v", args)
	}
}

func TestBuildExecArgs_AddDirs(t *testing.T) {
	args := buildExecArgs("", "prompt", "", "", "", "", ExecPolicyConfig{
		AddDirs: []string{"/a", "/b"},
	})
	var addDirs []string
	for i, a := range args {
		if a == "--add-dir" && i+1 < len(args) {
			addDirs = append(addDirs, args[i+1])
		}
	}
	if !slices.Contains(addDirs, "/a") || !slices.Contains(addDirs, "/b") {
		t.Fatalf("expected --add-dir /a and /b, got addDirs=%v args=%v", addDirs, args)
	}
}

func TestMergeUsage(t *testing.T) {
	a := Usage{InputTokens: 10, CachedInputTokens: 2, OutputTokens: 5}
	b := Usage{InputTokens: 3, CachedInputTokens: 1, OutputTokens: 4}
	got := mergeUsage(a, b)
	want := Usage{InputTokens: 13, CachedInputTokens: 3, OutputTokens: 9}
	if got != want {
		t.Fatalf("mergeUsage: want %+v, got %+v", want, got)
	}
}

func TestUsage_HasUsage(t *testing.T) {
	if (Usage{}).HasUsage() {
		t.Fatal("zero Usage should not HasUsage")
	}
	if !(Usage{InputTokens: 1}).HasUsage() {
		t.Fatal("Usage with InputTokens should HasUsage")
	}
}

func TestUniqueAddDirs(t *testing.T) {
	got := uniqueAddDirs([]string{"/a", "/b", "/a", "  /b  "})
	if len(got) != 2 {
		t.Fatalf("expected 2 unique dirs, got %v", got)
	}
}
