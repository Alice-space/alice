package imagegen

import (
	"strings"
	"testing"

	"github.com/openai/openai-go/v3"
)

func TestResolveOpenAIProxyConfig_UsesDedicatedEnvOverrides(t *testing.T) {
	got := resolveOpenAIProxyConfig(map[string]string{
		"OPENAI_HTTP_PROXY":  "  http://env-http:8080  ",
		"OPENAI_HTTPS_PROXY": "  http://env-https:8080  ",
		"OPENAI_ALL_PROXY":   "  socks5://env-all:1080  ",
		"OPENAI_NO_PROXY":    "  open.feishu.cn,.example.com  ",
	})

	if got.HTTPProxy != "http://env-http:8080" {
		t.Fatalf("unexpected http proxy: %q", got.HTTPProxy)
	}
	if got.HTTPSProxy != "http://env-https:8080" {
		t.Fatalf("unexpected https proxy: %q", got.HTTPSProxy)
	}
	if got.ALLProxy != "socks5://env-all:1080" {
		t.Fatalf("unexpected all proxy: %q", got.ALLProxy)
	}
	if got.NoProxy != "open.feishu.cn,.example.com" {
		t.Fatalf("unexpected no proxy: %q", got.NoProxy)
	}
}

func TestResolveOpenAIProxyConfig_EmptyWhenEnvIsEmpty(t *testing.T) {
	got := resolveOpenAIProxyConfig(map[string]string{
		"OPENAI_HTTPS_PROXY": "   ",
	})

	if got != (openAIProxyConfig{}) {
		t.Fatalf("unexpected proxy config: %#v", got)
	}
}

func TestImageContentTypeForPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{path: "refs/base.png", want: "image/png"},
		{path: "refs/base.jpg", want: "image/jpeg"},
		{path: "refs/base.jpeg", want: "image/jpeg"},
		{path: "refs/base.webp", want: "image/webp"},
	}

	for _, tc := range tests {
		got, err := imageContentTypeForPath(tc.path)
		if err != nil {
			t.Fatalf("imageContentTypeForPath(%q) returned error: %v", tc.path, err)
		}
		if got != tc.want {
			t.Fatalf("imageContentTypeForPath(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestImageContentTypeForPath_RejectsUnsupportedExtension(t *testing.T) {
	_, err := imageContentTypeForPath("refs/base.gif")
	if err == nil {
		t.Fatal("expected error for unsupported extension")
	}
}

func TestOpenAIFileWrapperCarriesImageMetadata(t *testing.T) {
	reader := openai.File(strings.NewReader("png-bytes"), "base.png", "image/png")

	if reader.Filename() != "base.png" {
		t.Fatalf("unexpected filename: %q", reader.Filename())
	}

	if reader.ContentType() != "image/png" {
		t.Fatalf("unexpected content type: %q", reader.ContentType())
	}
}
