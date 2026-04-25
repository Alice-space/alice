package runtimeapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidatePathUnderRoot(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "sub", "file.txt")
	outside := filepath.Join(filepath.Dir(root), "outside.txt")
	escapedViaLink := filepath.Join(root, "link-outside", "outside.txt")
	if err := os.Symlink(filepath.Dir(root), filepath.Join(root, "link-outside")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	tests := []struct {
		name    string
		path    string
		root    string
		wantErr string
	}{
		{
			name:    "empty path",
			path:    "",
			root:    root,
			wantErr: "path is empty",
		},
		{
			name:    "relative path",
			path:    "relative.txt",
			root:    root,
			wantErr: "path must be absolute",
		},
		{
			name:    "empty root",
			path:    inside,
			root:    "",
			wantErr: "resource root is empty",
		},
		{
			name:    "missing root",
			path:    inside,
			root:    filepath.Join(root, "missing"),
			wantErr: "path out of allowed root",
		},
		{
			name:    "inside root",
			path:    inside,
			root:    root,
			wantErr: "",
		},
		{
			name:    "outside root",
			path:    outside,
			root:    root,
			wantErr: "path out of allowed root",
		},
		{
			name:    "outside root via symlink",
			path:    escapedViaLink,
			root:    root,
			wantErr: "path out of allowed root",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePathUnderRoot(tc.path, tc.root)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}
