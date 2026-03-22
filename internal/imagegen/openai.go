package imagegen

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"golang.org/x/net/http/httpproxy"

	"github.com/Alice-space/alice/internal/config"
)

type openAIProvider struct {
	client        openai.Client
	model         openai.ImageModel
	size          string
	quality       string
	background    string
	outputFormat  string
	inputFidelity string
}

func newOpenAIProvider(cfg config.ImageGenerationConfig, env map[string]string) (Provider, error) {
	httpClient, err := newHTTPClient(resolveOpenAIProxyConfig(env), time.Duration(cfg.TimeoutSecs)*time.Second)
	if err != nil {
		return nil, err
	}

	opts := []option.RequestOption{
		option.WithHTTPClient(httpClient),
		option.WithRequestTimeout(time.Duration(cfg.TimeoutSecs) * time.Second),
	}
	if apiKey := lookupEnv(env, "OPENAI_API_KEY"); apiKey != "" {
		opts = append(opts, option.WithAPIKey(apiKey))
	}

	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		baseURL = lookupEnv(env, "OPENAI_BASE_URL")
	}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}

	client := openai.NewClient(opts...)
	return &openAIProvider{
		client:        client,
		model:         openai.ImageModel(strings.TrimSpace(cfg.Model)),
		size:          strings.TrimSpace(cfg.Size),
		quality:       strings.TrimSpace(cfg.Quality),
		background:    strings.TrimSpace(cfg.Background),
		outputFormat:  strings.TrimSpace(cfg.OutputFormat),
		inputFidelity: strings.TrimSpace(cfg.InputFidelity),
	}, nil
}

type openAIProxyConfig struct {
	HTTPProxy  string
	HTTPSProxy string
	ALLProxy   string
	NoProxy    string
}

func resolveOpenAIProxyConfig(env map[string]string) openAIProxyConfig {
	return openAIProxyConfig{
		HTTPProxy:  lookupEnv(env, "OPENAI_HTTP_PROXY"),
		HTTPSProxy: lookupEnv(env, "OPENAI_HTTPS_PROXY"),
		ALLProxy:   lookupEnv(env, "OPENAI_ALL_PROXY"),
		NoProxy:    lookupEnv(env, "OPENAI_NO_PROXY"),
	}
}

func (p *openAIProvider) Generate(ctx context.Context, req Request) (Result, error) {
	if p == nil {
		return Result{}, errors.New("openai image provider is nil")
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return Result{}, errors.New("image prompt is empty")
	}
	if strings.TrimSpace(req.OutputPath) == "" {
		return Result{}, errors.New("image output path is empty")
	}

	var (
		resp openai.ImagesResponse
		err  error
	)
	references := compactExistingPaths(req.ReferenceImages, 16)
	if len(references) > 0 {
		resp, err = p.editImage(ctx, req, references)
	} else {
		resp, err = p.generateImage(ctx, req)
	}
	if err != nil {
		return Result{}, err
	}
	if len(resp.Data) == 0 {
		return Result{}, errors.New("openai image response is empty")
	}
	imageData := strings.TrimSpace(resp.Data[0].B64JSON)
	if imageData == "" {
		return Result{}, errors.New("openai image response missing base64 data")
	}
	decoded, err := base64.StdEncoding.DecodeString(imageData)
	if err != nil {
		return Result{}, fmt.Errorf("decode openai image failed: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(req.OutputPath), 0o755); err != nil {
		return Result{}, fmt.Errorf("prepare image output dir failed: %w", err)
	}
	if err := os.WriteFile(req.OutputPath, decoded, 0o644); err != nil {
		return Result{}, fmt.Errorf("write generated image failed: %w", err)
	}
	return Result{
		LocalPath:     req.OutputPath,
		RevisedPrompt: strings.TrimSpace(resp.Data[0].RevisedPrompt),
	}, nil
}

func (p *openAIProvider) generateImage(ctx context.Context, req Request) (openai.ImagesResponse, error) {
	params := openai.ImageGenerateParams{
		Prompt:       req.Prompt,
		Model:        p.model,
		Size:         openai.ImageGenerateParamsSize(p.size),
		Quality:      openai.ImageGenerateParamsQuality(p.quality),
		Background:   openai.ImageGenerateParamsBackground(p.background),
		OutputFormat: openai.ImageGenerateParamsOutputFormat(p.outputFormat),
	}
	if strings.TrimSpace(req.UserID) != "" {
		params.User = openai.String(strings.TrimSpace(req.UserID))
	}
	resp, err := p.client.Images.Generate(ctx, params)
	if err != nil {
		return openai.ImagesResponse{}, err
	}
	if resp == nil {
		return openai.ImagesResponse{}, errors.New("openai image response is nil")
	}
	return *resp, nil
}

func (p *openAIProvider) editImage(ctx context.Context, req Request, references []string) (openai.ImagesResponse, error) {
	readers := make([]io.Reader, 0, len(references))
	files := make([]*os.File, 0, len(references))
	for _, path := range references {
		file, err := os.Open(path)
		if err != nil {
			for _, opened := range files {
				_ = opened.Close()
			}
			return openai.ImagesResponse{}, fmt.Errorf("open reference image %s failed: %w", path, err)
		}
		contentType, err := imageContentTypeForPath(path)
		if err != nil {
			_ = file.Close()
			for _, opened := range files {
				_ = opened.Close()
			}
			return openai.ImagesResponse{}, err
		}
		files = append(files, file)
		readers = append(readers, openai.File(file, filepath.Base(path), contentType))
	}
	defer func() {
		for _, file := range files {
			_ = file.Close()
		}
	}()

	params := openai.ImageEditParams{
		Image:  openai.ImageEditParamsImageUnion{OfFileArray: readers},
		Prompt: req.Prompt,
		Model:  p.model,
	}
	if strings.TrimSpace(req.UserID) != "" {
		params.User = openai.String(strings.TrimSpace(req.UserID))
	}
	resp, err := p.client.Images.Edit(ctx, params)
	if err != nil {
		return openai.ImagesResponse{}, err
	}
	if resp == nil {
		return openai.ImagesResponse{}, errors.New("openai image response is nil")
	}
	return *resp, nil
}

func imageContentTypeForPath(path string) (string, error) {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(path))) {
	case ".jpg", ".jpeg":
		return "image/jpeg", nil
	case ".png":
		return "image/png", nil
	case ".webp":
		return "image/webp", nil
	default:
		return "", fmt.Errorf("unsupported reference image format for %s", path)
	}
}

func newHTTPClient(proxyCfg openAIProxyConfig, timeout time.Duration) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	proxyFunc, err := buildProxyFunc(proxyCfg)
	if err != nil {
		return nil, err
	}
	transport.Proxy = proxyFunc
	transport.ForceAttemptHTTP2 = true
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}, nil
}

func buildProxyFunc(proxyCfg openAIProxyConfig) (func(*http.Request) (*url.URL, error), error) {
	proxyCfg = openAIProxyConfig{
		HTTPProxy:  strings.TrimSpace(proxyCfg.HTTPProxy),
		HTTPSProxy: strings.TrimSpace(proxyCfg.HTTPSProxy),
		ALLProxy:   strings.TrimSpace(proxyCfg.ALLProxy),
		NoProxy:    strings.TrimSpace(proxyCfg.NoProxy),
	}
	if proxyCfg.HTTPProxy == "" {
		proxyCfg.HTTPProxy = proxyCfg.ALLProxy
	}
	if proxyCfg.HTTPSProxy == "" {
		proxyCfg.HTTPSProxy = proxyCfg.ALLProxy
	}
	if proxyCfg.HTTPProxy == "" && proxyCfg.HTTPSProxy == "" && proxyCfg.NoProxy == "" {
		return nil, nil
	}
	cfg := &httpproxy.Config{
		HTTPProxy:  proxyCfg.HTTPProxy,
		HTTPSProxy: proxyCfg.HTTPSProxy,
		NoProxy:    proxyCfg.NoProxy,
	}
	proxyForURL := cfg.ProxyFunc()
	return func(req *http.Request) (*url.URL, error) {
		if req == nil || req.URL == nil {
			return nil, nil
		}
		return proxyForURL(req.URL)
	}, nil
}

func lookupEnv(env map[string]string, key string) string {
	if len(env) == 0 {
		return ""
	}
	return strings.TrimSpace(env[strings.ToUpper(strings.TrimSpace(key))])
}

func compactExistingPaths(paths []string, limit int) []string {
	if len(paths) == 0 {
		return nil
	}
	out := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, raw := range paths {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}
