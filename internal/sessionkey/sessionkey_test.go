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
	if got := VisibilityKey("chat_id:oc_chat|thread:omt_1"); got != "chat_id:oc_chat" {
		t.Fatalf("unexpected visibility key: %q", got)
	}
}

func TestThreadScope(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"chat_id:oc_chat", ""},
		// work key now returns seed-scoped thread to prevent cross-thread blocking
		{"chat_id:oc_chat|work:om_AAA", "|seed:om_AAA"},
		{"chat_id:oc_chat|thread:omt_1", "|thread:omt_1"},
		// seed token (when present) takes precedence over thread
		{"chat_id:oc_chat|seed:om_AAA|thread:omt_1", "|seed:om_AAA"},
		// seed takes precedence over work
		{"chat_id:oc_chat|seed:om_AAA|work:om_BBB", "|seed:om_AAA"},
		{"", ""},
	}
	for _, c := range cases {
		if got := ThreadScope(c.input); got != c.want {
			t.Fatalf("ThreadScope(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestWithoutMessage(t *testing.T) {
	if got := WithoutMessage("chat_id:oc_chat|thread:omt_1|message:om_2"); got != "chat_id:oc_chat|thread:omt_1" {
		t.Fatalf("unexpected scoped session key: %q", got)
	}
}
