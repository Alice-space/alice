package connector

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Alice-space/alice/internal/config"
	"github.com/Alice-space/alice/internal/imagegen"
	"github.com/Alice-space/alice/internal/logging"
	"github.com/Alice-space/alice/internal/messaging"
)

type roleplayEnvelope struct {
	ReplyWill    int
	HasReplyWill bool
	ActionLine   string
	Speech       string
	VisibleReply string
	Suppressed   bool
}

type imageUploader = messaging.ImageUploader
type imageReplySender = messaging.ReplyImageSender
type imageDirectReplySender = messaging.ReplyImageDirectSender
type imageSendSender = messaging.ImageSender

type imageResourceRootProvider interface {
	ResourceRootForScope(resourceScopeKey string) string
}

func (p *Processor) startImageGeneration(ctx context.Context, job Job, reply, fallbackReplyTarget string) {
	if p == nil {
		return
	}
	snapshot := p.runtimeSnapshot()
	if !snapshot.imageGeneration.Enabled || snapshot.imageProvider == nil {
		return
	}

	soulDoc := job.SoulDoc
	if !soulDoc.Loaded {
		soulText, _ := os.ReadFile(strings.TrimSpace(job.SoulPath))
		soulDoc = parseSoulDocument(string(soulText))
	}
	envelope := parseRoleplayEnvelope(reply, soulDoc.OutputContract, job.NoReplyToken)
	if envelope.Suppressed || strings.TrimSpace(envelope.ActionLine) == "" {
		return
	}
	if soulDoc.ImageGeneration.MinReplyWill > 0 &&
		envelope.HasReplyWill &&
		envelope.ReplyWill < soulDoc.ImageGeneration.MinReplyWill {
		return
	}

	go func() {
		runCtx := context.WithoutCancel(ctx)
		if err := p.generateAndSendImage(runCtx, snapshot, job, envelope, fallbackReplyTarget); err != nil {
			logging.Warnf("generate roleplay image failed event_id=%s: %v", job.EventID, err)
		}
	}()
}

func (p *Processor) generateAndSendImage(
	ctx context.Context,
	snapshot processorRuntimeSnapshot,
	job Job,
	envelope roleplayEnvelope,
	fallbackReplyTarget string,
) error {
	uploader, ok := p.sender.(imageUploader)
	if !ok {
		return fmt.Errorf("sender does not support image upload")
	}

	resourceRoot := imageOutputRoot(p.sender, job)
	if err := os.MkdirAll(resourceRoot, 0o755); err != nil {
		return fmt.Errorf("prepare image output root failed: %w", err)
	}
	outputPath := filepath.Join(resourceRoot, buildGeneratedImageFileName(snapshot.imageGeneration.OutputFormat, p.now()))

	soulDoc := job.SoulDoc
	if !soulDoc.Loaded {
		soulText, _ := os.ReadFile(strings.TrimSpace(job.SoulPath))
		soulDoc = parseSoulDocument(string(soulText))
	}
	prompt := buildRoleplayImagePrompt(job, envelope, soulDoc.Body)
	if strings.TrimSpace(prompt) == "" {
		return fmt.Errorf("image prompt is empty")
	}

	req := imagegen.Request{
		Prompt:          prompt,
		OutputPath:      outputPath,
		ReferenceImages: collectImageReferencePaths(job, snapshot.imageGeneration, strings.TrimSpace(job.SoulPath), soulDoc),
		UserID:          defaultIfEmpty(strings.TrimSpace(job.SenderUserID), strings.TrimSpace(job.SenderOpenID)),
	}
	result, err := snapshot.imageProvider.Generate(ctx, req)
	if err != nil {
		return err
	}
	localPaths := collectGeneratedImageLocalPaths(result)
	if len(localPaths) == 0 {
		return fmt.Errorf("generated image path is empty")
	}
	replyTarget := strings.TrimSpace(job.SourceMessageID)
	if replyTarget == "" {
		replyTarget = strings.TrimSpace(fallbackReplyTarget)
	}
	lastReplyMessageID := ""
	for _, localPath := range localPaths {
		imageKey, err := uploader.UploadImage(ctx, localPath)
		if err != nil {
			return fmt.Errorf("upload generated image failed: %w", err)
		}
		if replyTarget != "" {
			if messageID, err := p.replyGeneratedImage(ctx, job, replyTarget, imageKey); err == nil && strings.TrimSpace(messageID) != "" {
				lastReplyMessageID = messageID
				continue
			}
		}
		if sender, ok := p.sender.(imageSendSender); ok {
			if err := sender.SendImage(ctx, job.ReceiveIDType, job.ReceiveID, imageKey); err != nil {
				return err
			}
			continue
		}
		return fmt.Errorf("sender does not support image send")
	}
	if strings.TrimSpace(lastReplyMessageID) != "" {
		p.rememberReplySessionMessage(job, lastReplyMessageID)
	}
	return nil
}

func collectGeneratedImageLocalPaths(result imagegen.Result) []string {
	paths := make([]string, 0, len(result.LocalPaths)+1)
	for _, path := range result.LocalPaths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		paths = append(paths, path)
	}
	if len(paths) == 0 {
		if path := strings.TrimSpace(result.LocalPath); path != "" {
			paths = append(paths, path)
		}
	}
	return paths
}

func (p *Processor) replyGeneratedImage(
	ctx context.Context,
	job Job,
	replyTarget string,
	imageKey string,
) (string, error) {
	if p == nil || p.sender == nil {
		return "", fmt.Errorf("sender does not support image reply")
	}
	replyTarget = strings.TrimSpace(replyTarget)
	imageKey = strings.TrimSpace(imageKey)
	if replyTarget == "" || imageKey == "" {
		return "", fmt.Errorf("reply target or image key is empty")
	}
	if !jobPrefersThreadReply(job) {
		if replier, ok := p.sender.(imageDirectReplySender); ok {
			return replier.ReplyImageDirect(ctx, replyTarget, imageKey)
		}
	}
	if replier, ok := p.sender.(imageReplySender); ok {
		return replier.ReplyImage(ctx, replyTarget, imageKey)
	}
	return "", fmt.Errorf("sender does not support image reply")
}

func parseRoleplayEnvelope(reply string, contract outputContract, noReplyToken string) roleplayEnvelope {
	envelope := roleplayEnvelope{
		VisibleReply: stripHiddenReplyMetadata(reply, contract),
	}
	trimmedReply := strings.TrimSpace(reply)
	if trimmedReply == "" {
		return envelope
	}
	if score, ok := extractReplyWillScore(trimmedReply, contract); ok {
		envelope.ReplyWill = score
		envelope.HasReplyWill = true
	}
	envelope.ActionLine = extractTaggedBlockContent(trimmedReply, contract.MotionTag)

	visible := strings.TrimSpace(envelope.VisibleReply)
	token := contract.effectiveSuppressToken(noReplyToken)
	if token != "" && visible == token {
		envelope.Suppressed = true
		return envelope
	}
	if visible == "" {
		return envelope
	}
	envelope.Speech = visible
	return envelope
}

func extractReplyWillScore(reply string, contract outputContract) (int, bool) {
	tag := strings.TrimSpace(contract.ReplyWillTag)
	field := strings.TrimSpace(contract.ReplyWillField)
	if tag == "" || field == "" {
		return 0, false
	}
	content := extractTaggedBlockContent(reply, tag)
	if content == "" {
		return 0, false
	}
	pattern := regexp.MustCompile(fmt.Sprintf(`(?im)^\s*%s\s*:\s*(\d{1,3})\s*%%?\s*$`, regexp.QuoteMeta(field)))
	matches := pattern.FindStringSubmatch(content)
	if len(matches) < 2 {
		return 0, false
	}
	value, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, false
	}
	if value < 0 {
		value = 0
	}
	if value > 100 {
		value = 100
	}
	return value, true
}

func buildRoleplayImagePrompt(job Job, envelope roleplayEnvelope, soulText string) string {
	profile := extractSoulImageProfile(soulText)
	if profile == "" {
		profile = clipText(strings.TrimSpace(soulText), 1600)
	}

	parts := []string{
		fmt.Sprintf("Generate a polished anime illustration of %s.", defaultIfEmpty(strings.TrimSpace(job.BotName), "the character")),
		"Keep the character identity consistent with the role profile and attached reference images.",
	}
	if strings.TrimSpace(profile) != "" {
		parts = append(parts, "Character profile:\n"+profile)
	}
	if strings.TrimSpace(envelope.ActionLine) != "" {
		parts = append(parts, "Current action and expression:\n"+strings.TrimSpace(envelope.ActionLine))
	}
	if strings.TrimSpace(envelope.Speech) != "" {
		parts = append(parts, "Dialogue mood:\n"+clipText(strings.TrimSpace(envelope.Speech), 300))
	}
	parts = append(parts,
		"Composition: single character, medium shot or upper-body key visual, readable face and body language.",
		"Focus on eyes, eyebrows, mouth, waist, arms, thighs, clothing folds, accessories, and hair motion.",
		"Style: high-quality anime illustration, expressive pose, clean lineart, detailed face, coherent hands.",
		"Negative cues: text, watermark, logo, extra fingers, extra limbs, deformed hands, low quality, blurry.",
	)
	return strings.Join(parts, "\n\n")
}

func extractSoulImageProfile(soulText string) string {
	soulText = strings.TrimSpace(soulText)
	if soulText == "" {
		return ""
	}
	startMarker := "以下是你的基本信息。"
	endMarker := "下面我会规范"

	start := strings.Index(soulText, startMarker)
	if start >= 0 {
		soulText = strings.TrimSpace(soulText[start+len(startMarker):])
	}
	end := strings.Index(soulText, endMarker)
	if end >= 0 {
		soulText = strings.TrimSpace(soulText[:end])
	}
	return clipText(strings.TrimSpace(soulText), 2200)
}

func collectImageReferencePaths(job Job, cfg config.ImageGenerationConfig, soulPath string, soulDoc soulDocument) []string {
	candidates := make([]string, 0, 8)
	candidates = append(candidates, collectStaticSoulReferenceImages(soulPath, soulDoc)...)
	if cfg.UseCurrentAttachments {
		candidates = append(candidates, collectAttachmentReferenceImages(job.Attachments)...)
	}
	if len(candidates) == 0 {
		return nil
	}
	out := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		path := strings.TrimSpace(candidate)
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
	}
	sort.Strings(out)
	return out
}

func collectStaticSoulReferenceImages(soulPath string, soulDoc soulDocument) []string {
	soulPath = strings.TrimSpace(soulPath)
	if soulPath == "" {
		return nil
	}
	if len(soulDoc.ImageRefs) > 0 {
		paths := make([]string, 0, len(soulDoc.ImageRefs))
		for _, ref := range soulDoc.ImageRefs {
			path := strings.TrimSpace(ref)
			if path == "" {
				continue
			}
			if !filepath.IsAbs(path) {
				path = filepath.Join(filepath.Dir(soulPath), path)
			}
			paths = append(paths, filepath.Clean(path))
		}
		return paths
	}
	refsDir := filepath.Join(filepath.Dir(soulPath), "refs")
	entries, err := os.ReadDir(refsDir)
	if err != nil {
		return nil
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(entry.Name()))
		switch filepath.Ext(name) {
		case ".png", ".jpg", ".jpeg", ".webp":
			paths = append(paths, filepath.Join(refsDir, entry.Name()))
		}
	}
	sort.Strings(paths)
	return paths
}

func collectAttachmentReferenceImages(attachments []Attachment) []string {
	if len(attachments) == 0 {
		return nil
	}
	paths := make([]string, 0, len(attachments))
	for _, attachment := range attachments {
		localPath := strings.TrimSpace(attachment.LocalPath)
		if localPath == "" {
			continue
		}
		kind := strings.ToLower(strings.TrimSpace(attachment.Kind))
		ext := strings.ToLower(strings.TrimSpace(filepath.Ext(localPath)))
		if kind == "image" || attachment.ImageKey != "" || ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".webp" {
			paths = append(paths, localPath)
		}
	}
	return paths
}

func imageOutputRoot(sender any, job Job) string {
	scopeKey := resourceScopeKeyForJob(job)
	if provider, ok := sender.(imageResourceRootProvider); ok {
		root := strings.TrimSpace(provider.ResourceRootForScope(scopeKey))
		if root != "" {
			return filepath.Join(root, "generated")
		}
	}
	return filepath.Join(os.TempDir(), "alice-generated-images")
}

func buildGeneratedImageFileName(outputFormat string, now time.Time) string {
	ext := strings.ToLower(strings.TrimSpace(outputFormat))
	switch ext {
	case "jpeg":
		ext = "jpg"
	case "png", "jpg", "webp":
	default:
		ext = "png"
	}
	return fmt.Sprintf("roleplay-%d.%s", now.UnixNano(), ext)
}
