package imagegen

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"

	"github.com/Alice-space/alice/internal/config"
)

type openAIProvider struct {
	httpClient        *http.Client
	client            openai.Client
	apiKey            string
	baseURL           string
	model             openai.ImageModel
	moderation        string
	n                 int
	outputCompression int
	responseFormat    string
	size              string
	quality           string
	background        string
	outputFormat      string
	partialImages     int
	stream            bool
	style             string
	inputFidelity     string
	maskPath          string
}

type openAIProxyConfig struct {
	HTTPProxy  string
	HTTPSProxy string
	ALLProxy   string
	NoProxy    string
}

type imageOutput struct {
	B64JSON       string
	URL           string
	RevisedPrompt string
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
	apiKey := strings.TrimSpace(lookupEnv(env, "OPENAI_API_KEY"))
	if apiKey := lookupEnv(env, "OPENAI_API_KEY"); apiKey != "" {
		opts = append(opts, option.WithAPIKey(apiKey))
	}

	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		baseURL = lookupEnv(env, "OPENAI_BASE_URL")
	}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	} else {
		baseURL = "https://api.openai.com/v1"
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")

	client := openai.NewClient(opts...)
	return &openAIProvider{
		httpClient:        httpClient,
		client:            client,
		apiKey:            apiKey,
		baseURL:           baseURL,
		model:             openai.ImageModel(strings.TrimSpace(cfg.Model)),
		moderation:        strings.TrimSpace(cfg.Moderation),
		n:                 cfg.N,
		outputCompression: cfg.OutputCompression,
		responseFormat:    strings.TrimSpace(cfg.ResponseFormat),
		size:              strings.TrimSpace(cfg.Size),
		quality:           strings.TrimSpace(cfg.Quality),
		background:        strings.TrimSpace(cfg.Background),
		outputFormat:      strings.TrimSpace(cfg.OutputFormat),
		partialImages:     cfg.PartialImages,
		stream:            cfg.Stream,
		style:             strings.TrimSpace(cfg.Style),
		inputFidelity:     strings.TrimSpace(cfg.InputFidelity),
		maskPath:          strings.TrimSpace(cfg.MaskPath),
	}, nil
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
		payloads      []imageOutput
		revisedPrompt string
		err           error
	)
	references := compactExistingPaths(req.ReferenceImages, 16)
	if len(references) > 0 {
		payloads, revisedPrompt, err = p.editImage(ctx, req, references)
	} else {
		payloads, revisedPrompt, err = p.generateImage(ctx, req)
	}
	if err != nil {
		return Result{}, err
	}
	localPaths, err := p.writeImageOutputs(ctx, req.OutputPath, payloads)
	if err != nil {
		return Result{}, err
	}
	if len(localPaths) == 0 {
		return Result{}, errors.New("openai image response is empty")
	}
	return Result{
		LocalPath:     localPaths[0],
		LocalPaths:    localPaths,
		RevisedPrompt: revisedPrompt,
	}, nil
}
