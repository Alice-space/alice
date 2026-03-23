package imagegen

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/openai/openai-go/v3"
)

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

func (p *openAIProvider) buildGenerateParams(req Request) openai.ImageGenerateParams {
	params := openai.ImageGenerateParams{
		Prompt:         req.Prompt,
		Model:          p.model,
		Background:     openai.ImageGenerateParamsBackground(p.background),
		OutputFormat:   openai.ImageGenerateParamsOutputFormat(p.outputFormat),
		Quality:        openai.ImageGenerateParamsQuality(p.quality),
		Size:           openai.ImageGenerateParamsSize(p.size),
		Moderation:     openai.ImageGenerateParamsModeration(p.moderation),
		ResponseFormat: openai.ImageGenerateParamsResponseFormat(p.responseFormat),
		Style:          openai.ImageGenerateParamsStyle(p.style),
	}
	if p.n > 0 {
		params.N = openai.Int(int64(p.n))
	}
	if p.outputCompression >= 0 {
		params.OutputCompression = openai.Int(int64(p.outputCompression))
	}
	if p.partialImages >= 0 {
		params.PartialImages = openai.Int(int64(p.partialImages))
	}
	if strings.TrimSpace(req.UserID) != "" {
		params.User = openai.String(strings.TrimSpace(req.UserID))
	}
	return params
}

func (p *openAIProvider) buildEditParams(req Request, readers []io.Reader, mask io.Reader) openai.ImageEditParams {
	imageUnion := openai.ImageEditParamsImageUnion{}
	switch len(readers) {
	case 1:
		imageUnion.OfFile = readers[0]
	default:
		imageUnion.OfFileArray = readers
	}
	params := openai.ImageEditParams{
		Image:         imageUnion,
		Prompt:        req.Prompt,
		Model:         p.model,
		Background:    openai.ImageEditParamsBackground(p.background),
		InputFidelity: openai.ImageEditParamsInputFidelity(p.inputFidelity),
		OutputFormat:  openai.ImageEditParamsOutputFormat(p.outputFormat),
		Quality:       openai.ImageEditParamsQuality(p.quality),
		Size:          openai.ImageEditParamsSize(p.size),
	}
	if p.n > 0 {
		params.N = openai.Int(int64(p.n))
	}
	if p.outputCompression >= 0 {
		params.OutputCompression = openai.Int(int64(p.outputCompression))
	}
	if p.partialImages >= 0 {
		params.PartialImages = openai.Int(int64(p.partialImages))
	}
	if mask != nil {
		params.Mask = mask
	}
	if strings.TrimSpace(req.UserID) != "" {
		params.User = openai.String(strings.TrimSpace(req.UserID))
	}
	if strings.TrimSpace(p.moderation) != "" {
		params.SetExtraFields(map[string]any{"moderation": strings.TrimSpace(p.moderation)})
	}
	return params
}

func (p *openAIProvider) openMaskReader() (*os.File, io.Reader, error) {
	maskPath := strings.TrimSpace(p.maskPath)
	if maskPath == "" {
		return nil, nil, nil
	}
	resolvedPath, err := filepath.Abs(maskPath)
	if err != nil {
		return nil, nil, err
	}
	if strings.ToLower(filepath.Ext(resolvedPath)) != ".png" {
		return nil, nil, fmt.Errorf("mask image must be a PNG file: %s", resolvedPath)
	}
	maskFile, err := os.Open(resolvedPath)
	if err != nil {
		return nil, nil, err
	}
	return maskFile, openai.File(maskFile, filepath.Base(resolvedPath), "image/png"), nil
}

func mergeExtraFields(current map[string]any, extra map[string]any) map[string]any {
	if len(current) == 0 && len(extra) == 0 {
		return nil
	}
	merged := make(map[string]any, len(current)+len(extra))
	for key, value := range current {
		merged[key] = value
	}
	for key, value := range extra {
		merged[key] = value
	}
	return merged
}
