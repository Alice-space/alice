package sessionkey

import "testing"

func TestBuild(t *testing.T) {
	if got := Build(" chat_id ", " oc_chat "); got != "chat_id:oc_chat" {
		t.Fatalf("unexpected built session key: %q", got)
	}
	if got := Build("", "oc_chat"); got != "" {
		t.Fatalf("expected empty key, got %q", got)
	}
}

func TestVisibilityKey(t *testing.T) {
	if got := VisibilityKey("chat_id:oc_chat|scene:work|thread:omt_1"); got != "chat_id:oc_chat" {
		t.Fatalf("unexpected visibility key: %q", got)
	}
}

func TestWithoutMessage(t *testing.T) {
	if got := WithoutMessage("chat_id:oc_chat|scene:work|thread:omt_1|message:om_2"); got != "chat_id:oc_chat|scene:work|thread:omt_1" {
		t.Fatalf("unexpected scoped session key: %q", got)
	}
}
