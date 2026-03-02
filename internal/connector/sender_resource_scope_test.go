package connector

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLarkSender_ResourceRootForScope(t *testing.T) {
	baseRoot := filepath.Join(t.TempDir(), "resources")
	sender := NewLarkSender(nil, baseRoot)

	got := sender.ResourceRootForScope("chat_id:oc_chat")
	want := filepath.Join(baseRoot, "scopes", "chat_id", "oc_chat")
	if got != want {
		t.Fatalf("unexpected scoped resource root: got %q want %q", got, want)
	}
}

func TestLarkSender_WriteAttachmentFile_WritesUnderScopedResourceRoot(t *testing.T) {
	baseRoot := filepath.Join(t.TempDir(), "resources")
	sender := NewLarkSender(nil, baseRoot)
	scopeRoot := sender.ResourceRootForScope("chat_id:oc_chat")
	attachment := &Attachment{Kind: "image", ImageKey: "img_123"}

	if err := sender.writeAttachmentFile(
		scopeRoot,
		"om_source",
		"image",
		"img_123",
		" poster draft .png ",
		bytes.NewBufferString("png-bytes"),
		attachment,
	); err != nil {
		t.Fatalf("write attachment file failed: %v", err)
	}
	if !strings.HasPrefix(attachment.LocalPath, scopeRoot+string(os.PathSeparator)) {
		t.Fatalf("attachment path should be under scoped root, got %q", attachment.LocalPath)
	}
	if _, err := os.Stat(attachment.LocalPath); err != nil {
		t.Fatalf("attachment file should exist, err=%v", err)
	}
}
