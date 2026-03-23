package imagegen

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/ssestream"
)

func (p *openAIProvider) generateImage(ctx context.Context, req Request) ([]imageOutput, string, error) {
	params := p.buildGenerateParams(req)
	if p.stream {
		return p.generateImageStreaming(ctx, params)
	}
	resp, err := p.client.Images.Generate(ctx, params)
	if err != nil {
		return nil, "", err
	}
	return extractImageOutputs(resp)
}

func (p *openAIProvider) editImage(ctx context.Context, req Request, references []string) ([]imageOutput, string, error) {
	readers := make([]io.Reader, 0, len(references))
	files := make([]*os.File, 0, len(references))
	for _, path := range references {
		file, err := os.Open(path)
		if err != nil {
			for _, opened := range files {
				_ = opened.Close()
			}
			return nil, "", fmt.Errorf("open reference image %s failed: %w", path, err)
		}
		contentType, err := imageContentTypeForPath(path)
		if err != nil {
			_ = file.Close()
			for _, opened := range files {
				_ = opened.Close()
			}
			return nil, "", err
		}
		files = append(files, file)
		readers = append(readers, openai.File(file, filepath.Base(path), contentType))
	}
	maskFile, maskReader, err := p.openMaskReader()
	if err != nil {
		for _, file := range files {
			_ = file.Close()
		}
		return nil, "", err
	}
	if maskFile != nil {
		files = append(files, maskFile)
	}
	defer func() {
		for _, file := range files {
			_ = file.Close()
		}
	}()

	params := p.buildEditParams(req, readers, maskReader)
	if p.stream {
		return p.editImageStreaming(ctx, params)
	}
	resp, err := p.client.Images.Edit(ctx, params)
	if err != nil {
		return nil, "", err
	}
	return extractImageOutputs(resp)
}

func (p *openAIProvider) generateImageStreaming(
	ctx context.Context,
	params openai.ImageGenerateParams,
) ([]imageOutput, string, error) {
	stream := p.client.Images.GenerateStreaming(ctx, params)
	if stream == nil {
		return nil, "", errors.New("openai image generation stream is nil")
	}
	var completed *openai.ImageGenCompletedEvent
	for stream.Next() {
		switch event := stream.Current().AsAny().(type) {
		case openai.ImageGenCompletedEvent:
			evt := event
			completed = &evt
		}
	}
	if err := stream.Err(); err != nil {
		return nil, "", err
	}
	if completed == nil || strings.TrimSpace(completed.B64JSON) == "" {
		return nil, "", errors.New("openai image generation stream missing completed image")
	}
	return []imageOutput{{B64JSON: strings.TrimSpace(completed.B64JSON)}}, "", nil
}

func (p *openAIProvider) editImageStreaming(
	ctx context.Context,
	params openai.ImageEditParams,
) ([]imageOutput, string, error) {
	params.SetExtraFields(mergeExtraFields(params.ExtraFields(), map[string]any{"stream": "true"}))
	body, contentType, err := params.MarshalMultipart()
	if err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/images/edits", bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "text/event-stream")
	if strings.TrimSpace(p.apiKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(p.apiKey))
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("openai image edit streaming failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	stream := ssestream.NewStream[openai.ImageEditStreamEventUnion](ssestream.NewDecoder(resp), nil)
	if stream == nil {
		_ = resp.Body.Close()
		return nil, "", errors.New("openai image edit stream is nil")
	}
	var completed *openai.ImageEditCompletedEvent
	for stream.Next() {
		switch event := stream.Current().AsAny().(type) {
		case openai.ImageEditCompletedEvent:
			evt := event
			completed = &evt
		}
	}
	err = stream.Err()
	_ = resp.Body.Close()
	if err != nil {
		return nil, "", err
	}
	if completed == nil || strings.TrimSpace(completed.B64JSON) == "" {
		return nil, "", errors.New("openai image edit stream missing completed image")
	}
	return []imageOutput{{B64JSON: strings.TrimSpace(completed.B64JSON)}}, "", nil
}
