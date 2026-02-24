package bootstrap

import (
	"path/filepath"
	"testing"
)

func TestResolveMemoryDir(t *testing.T) {
	cases := []struct {
		name        string
		workspace   string
		memory      string
		expectedAbs bool
		expected    string
	}{
		{
			name:        "absolute memory dir",
			workspace:   "/tmp/work",
			memory:      "/var/lib/alice-memory",
			expectedAbs: true,
			expected:    "/var/lib/alice-memory",
		},
		{
			name:        "relative memory dir",
			workspace:   "/tmp/work",
			memory:      ".memory",
			expectedAbs: true,
			expected:    filepath.Join("/tmp/work", ".memory"),
		},
		{
			name:        "default memory dir with empty workspace",
			workspace:   "",
			memory:      "",
			expectedAbs: false,
			expected:    filepath.Join(".", ".memory"),
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveMemoryDir(tc.workspace, tc.memory)
			if got != tc.expected {
				t.Fatalf("unexpected memory dir, got=%q want=%q", got, tc.expected)
			}
			if tc.expectedAbs != filepath.IsAbs(got) {
				t.Fatalf("unexpected absolute flag, got=%v want=%v", filepath.IsAbs(got), tc.expectedAbs)
			}
		})
	}
}

func TestResolveConfigPath(t *testing.T) {
	if got := ResolveConfigPath(""); got != "config.yaml" {
		t.Fatalf("empty config path should fallback to config.yaml, got=%q", got)
	}

	got := ResolveConfigPath("config.yaml")
	if !filepath.IsAbs(got) {
		t.Fatalf("relative config path should resolve absolute path, got=%q", got)
	}
	if filepath.Base(got) != "config.yaml" {
		t.Fatalf("unexpected resolved config base, got=%q", filepath.Base(got))
	}
}
