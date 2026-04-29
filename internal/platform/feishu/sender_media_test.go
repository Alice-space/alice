package feishu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFeishuSenderResolveUploadPathAllowsOutsideResourceDir(t *testing.T) {
	resourceDir := t.TempDir()
	outsideDir := t.TempDir()
	filePath := filepath.Join(outsideDir, "report.txt")
	if err := os.WriteFile(filePath, []byte("report"), 0o600); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	sender := NewFeishuSender(nil, resourceDir)
	resolved, info, err := sender.resolveUploadPath(filePath)
	if err != nil {
		t.Fatalf("resolve upload path failed: %v", err)
	}
	if resolved != filePath {
		t.Fatalf("unexpected resolved path: got %q want %q", resolved, filePath)
	}
	if info == nil || info.Size() != int64(len("report")) {
		t.Fatalf("unexpected file info: %#v", info)
	}
}

func TestFeishuSenderResolveUploadPathRejectsInvalidFiles(t *testing.T) {
	emptyFile := filepath.Join(t.TempDir(), "empty.txt")
	if err := os.WriteFile(emptyFile, nil, 0o600); err != nil {
		t.Fatalf("write empty file: %v", err)
	}
	dir := t.TempDir()

	tests := []struct {
		name    string
		path    string
		wantErr string
	}{
		{name: "empty path", path: "", wantErr: "local path is empty"},
		{name: "directory", path: dir, wantErr: "path is directory"},
		{name: "empty file", path: emptyFile, wantErr: "file is empty"},
	}

	sender := NewFeishuSender(nil, t.TempDir())
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := sender.resolveUploadPath(tc.path)
			if err == nil {
				t.Fatalf("expected error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}
