package imagegen

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3"

	"github.com/Alice-space/alice/internal/config"
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

func TestOpenAIProviderEditImage_UsesMinimalCompatibleParams(t *testing.T) {
	t.Parallel()

	type uploadedImage struct {
		FormName    string
		FileName    string
		ContentType string
	}
	type requestCapture struct {
		Path   string
		Fields map[string][]string
		Images []uploadedImage
	}

	captured := requestCapture{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Helper()
		captured.Path = r.URL.Path
		captured.Fields = map[string][]string{}
		reader, err := r.MultipartReader()
		if err != nil {
			t.Fatalf("MultipartReader() failed: %v", err)
		}
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("NextPart() failed: %v", err)
			}
			body, readErr := io.ReadAll(part)
			if readErr != nil {
				t.Fatalf("ReadAll(%q) failed: %v", part.FormName(), readErr)
			}
			if strings.HasPrefix(part.FormName(), "image") {
				captured.Images = append(captured.Images, uploadedImage{
					FormName:    part.FormName(),
					FileName:    part.FileName(),
					ContentType: part.Header.Get("Content-Type"),
				})
				continue
			}
			captured.Fields[part.FormName()] = append(captured.Fields[part.FormName()], string(body))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString([]byte("ok")) + `"}]}`))
	}))
	defer server.Close()

	provider := mustNewTestOpenAIProvider(t, server.URL)
	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "out.png")
	refPath := filepath.Join(tempDir, "ref.png")
	if err := os.WriteFile(refPath, []byte("fake-png"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) failed: %v", refPath, err)
	}

	_, err := provider.Generate(context.Background(), Request{
		Prompt:          "draw Mea waving",
		ReferenceImages: []string{refPath},
		OutputPath:      outputPath,
		UserID:          "user-123",
	})
	if err != nil {
		t.Fatalf("Generate() failed: %v", err)
	}

	if captured.Path != "/images/edits" {
		t.Fatalf("unexpected path: %q", captured.Path)
	}
	for _, field := range []string{"background", "size", "quality", "output_format", "input_fidelity"} {
		if got := firstField(captured.Fields, field); got != "" {
			t.Fatalf("%s should be omitted for edit requests, got %q", field, got)
		}
	}
	if len(captured.Images) != 1 {
		t.Fatalf("unexpected image count: %d", len(captured.Images))
	}
	if captured.Images[0].ContentType != "image/png" {
		t.Fatalf("unexpected image content type: %q", captured.Images[0].ContentType)
	}
	if captured.Images[0].FileName != "ref.png" {
		t.Fatalf("unexpected image filename: %q", captured.Images[0].FileName)
	}
}

func TestOpenAIProviderGenerateImage_KeepsBackground(t *testing.T) {
	t.Parallel()

	type generateRequest struct {
		Path       string `json:"-"`
		Background string `json:"background"`
		Prompt     string `json:"prompt"`
		Model      string `json:"model"`
		Quality    string `json:"quality"`
		Size       string `json:"size"`
	}

	var captured generateRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Helper()
		captured.Path = r.URL.Path
		if mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil {
			t.Fatalf("ParseMediaType() failed: %v", err)
		} else if mediaType != "application/json" {
			t.Fatalf("unexpected content type: %q", mediaType)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("Decode() failed: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString([]byte("ok")) + `"}]}`))
	}))
	defer server.Close()

	provider := mustNewTestOpenAIProvider(t, server.URL)
	outputPath := filepath.Join(t.TempDir(), "out.png")

	_, err := provider.Generate(context.Background(), Request{
		Prompt:     "draw Mea waving",
		OutputPath: outputPath,
		UserID:     "user-123",
	})
	if err != nil {
		t.Fatalf("Generate() failed: %v", err)
	}

	if captured.Path != "/images/generations" {
		t.Fatalf("unexpected path: %q", captured.Path)
	}
	if captured.Background != "auto" {
		t.Fatalf("unexpected background: %q", captured.Background)
	}
	if captured.Model != "gpt-image-1.5" {
		t.Fatalf("unexpected model: %q", captured.Model)
	}
}

func mustNewTestOpenAIProvider(t *testing.T, baseURL string) Provider {
	t.Helper()

	provider, err := newOpenAIProvider(config.ImageGenerationConfig{
		Provider:              "openai",
		Model:                 "gpt-image-1.5",
		BaseURL:               baseURL,
		TimeoutSecs:           5,
		Size:                  "1024x1536",
		Quality:               "high",
		Background:            "auto",
		OutputFormat:          "png",
		InputFidelity:         "high",
		UseCurrentAttachments: true,
	}, map[string]string{
		"OPENAI_API_KEY": "test-key",
	})
	if err != nil {
		t.Fatalf("newOpenAIProvider() failed: %v", err)
	}
	return provider
}

func firstField(fields map[string][]string, key string) string {
	values := fields[key]
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
