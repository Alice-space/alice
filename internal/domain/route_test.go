package domain

import (
	"testing"
	"time"
)

func TestCanonicalRouteKeys(t *testing.T) {
	enc := NewCanonicalRouteKeyEncoder()
	got := enc.RepoIssue("GitHub.com/Owner/Repo", "00012")
	want := "repo_issue:github:owner/repo:12"
	if got != want {
		t.Fatalf("repo issue key mismatch: got %q want %q", got, want)
	}

	conv := enc.Conversation("Feishu", "conv-1", "")
	if conv != "conversation:feishu:conv-1:root" {
		t.Fatalf("conversation key mismatch: %s", conv)
	}

	bucket := time.Date(2026, 3, 10, 12, 3, 40, 0, time.UTC)
	c1 := enc.Coalescing("im", "u1", "query", "repo", bucket)
	c2 := enc.Coalescing("im", "u1", "query", "repo", bucket.Add(1*time.Minute))
	if c1 != c2 {
		t.Fatalf("coalescing key should be stable in same 5m bucket")
	}
}
