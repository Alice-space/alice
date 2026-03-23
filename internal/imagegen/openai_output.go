package imagegen

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/openai/openai-go/v3"
)

func extractImageOutputs(resp *openai.ImagesResponse) ([]imageOutput, string, error) {
	if resp == nil {
		return nil, "", errors.New("openai image response is nil")
	}
	if len(resp.Data) == 0 {
		return nil, "", errors.New("openai image response is empty")
	}
	outputs := make([]imageOutput, 0, len(resp.Data))
	revisedPrompt := ""
	for _, item := range resp.Data {
		output := imageOutput{
			B64JSON:       strings.TrimSpace(item.B64JSON),
			URL:           strings.TrimSpace(item.URL),
			RevisedPrompt: strings.TrimSpace(item.RevisedPrompt),
		}
		if output.B64JSON == "" && output.URL == "" {
			continue
		}
		if revisedPrompt == "" && output.RevisedPrompt != "" {
			revisedPrompt = output.RevisedPrompt
		}
		outputs = append(outputs, output)
	}
	if len(outputs) == 0 {
		return nil, "", errors.New("openai image response missing image data")
	}
	return outputs, revisedPrompt, nil
}

func (p *openAIProvider) writeImageOutputs(
	ctx context.Context,
	outputPath string,
	outputs []imageOutput,
) ([]string, error) {
	if len(outputs) == 0 {
		return nil, errors.New("openai image response is empty")
	}
	localPaths := make([]string, 0, len(outputs))
	for idx, output := range outputs {
		targetPath := indexedOutputPath(outputPath, idx)
		switch {
		case strings.TrimSpace(output.B64JSON) != "":
			if err := writeBase64Image(targetPath, output.B64JSON); err != nil {
				return nil, err
			}
		case strings.TrimSpace(output.URL) != "":
			if err := p.downloadImageURL(ctx, output.URL, targetPath); err != nil {
				return nil, err
			}
		default:
			return nil, errors.New("openai image response missing image data")
		}
		localPaths = append(localPaths, targetPath)
	}
	return localPaths, nil
}

func writeBase64Image(outputPath, imageData string) error {
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(imageData))
	if err != nil {
		return fmt.Errorf("decode openai image failed: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("prepare image output dir failed: %w", err)
	}
	if err := os.WriteFile(outputPath, decoded, 0o644); err != nil {
		return fmt.Errorf("write generated image failed: %w", err)
	}
	return nil
}

func (p *openAIProvider) downloadImageURL(ctx context.Context, rawURL, outputPath string) error {
	if p == nil || p.httpClient == nil {
		return errors.New("openai image download client is nil")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSpace(rawURL), nil)
	if err != nil {
		return err
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("download openai image url failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("download openai image url failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("prepare image output dir failed: %w", err)
	}
	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create generated image failed: %w", err)
	}
	defer file.Close()
	if _, err := io.Copy(file, resp.Body); err != nil {
		return fmt.Errorf("write generated image failed: %w", err)
	}
	return nil
}

func indexedOutputPath(outputPath string, idx int) string {
	if idx <= 0 {
		return outputPath
	}
	ext := filepath.Ext(outputPath)
	base := strings.TrimSuffix(outputPath, ext)
	return fmt.Sprintf("%s-%d%s", base, idx+1, ext)
}
