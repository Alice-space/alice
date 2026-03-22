package connector

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Alice-space/alice/internal/config"
	"github.com/Alice-space/alice/internal/imagegen"
)

type fakeImageProvider struct {
	mu       sync.Mutex
	requests []imagegen.Request
	err      error
}

func (p *fakeImageProvider) Generate(_ context.Context, req imagegen.Request) (imagegen.Result, error) {
	p.mu.Lock()
	p.requests = append(p.requests, req)
	err := p.err
	p.mu.Unlock()
	if err != nil {
		return imagegen.Result{}, err
	}
	if err := os.MkdirAll(filepath.Dir(req.OutputPath), 0o755); err != nil {
		return imagegen.Result{}, err
	}
	if err := os.WriteFile(req.OutputPath, []byte("fake image"), 0o644); err != nil {
		return imagegen.Result{}, err
	}
	return imagegen.Result{LocalPath: req.OutputPath}, nil
}

func TestProcessor_GeneratesImageFromRoleplayReply(t *testing.T) {
	resourceRoot := t.TempDir()
	soulDir := t.TempDir()
	assetsDir := filepath.Join(soulDir, "assets")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		t.Fatalf("prepare assets dir failed: %v", err)
	}
	refImage := filepath.Join(assetsDir, "mea.png")
	if err := os.WriteFile(refImage, []byte("ref"), 0o644); err != nil {
		t.Fatalf("write ref image failed: %v", err)
	}
	soulPath := filepath.Join(soulDir, "SOUL.md")
	soulContent := "---\nimage_refs:\n  - assets/mea.png\nimage_generation:\n  min_reply_will: 50\noutput_contract:\n  hidden_tags:\n    - reply_will\n    - motion\n  reply_will_tag: reply_will\n  reply_will_field: reply_will\n  motion_tag: motion\n  suppress_token: \"[[NO_REPLY]]\"\n---\n作为猫娘，以下是你的基本信息。\n发色：浅米白色\n下面我会规范你的输出结果。"
	if err := os.WriteFile(soulPath, []byte(soulContent), 0o644); err != nil {
		t.Fatalf("write soul failed: %v", err)
	}

	sender := &senderStub{resourceRoot: resourceRoot}
	provider := &fakeImageProvider{}
	processor := NewProcessor(codexStub{
		resp: "<reply_will>\n快乐: 80%\n惊奇: 20%\n安全: 70%\n喜爱: 90%\n悲伤: 0%\n愤怒: 0%\nreply_will: 88%\n</reply_will>\n<motion>眼神微亮，嘴角轻轻翘起，手臂抬起指向前方</motion>\n来吧。",
	}, sender, "failed", "thinking")
	processor.newImageProvider = func(config.ImageGenerationConfig, map[string]string) (imagegen.Provider, error) {
		return provider, nil
	}
	if err := processor.SetImageGeneration(config.ImageGenerationConfig{
		Enabled:               true,
		Provider:              "openai",
		Model:                 "gpt-image-1.5",
		TimeoutSecs:           60,
		Size:                  "1024x1536",
		Quality:               "high",
		Background:            "auto",
		OutputFormat:          "png",
		InputFidelity:         "high",
		UseCurrentAttachments: true,
	}, map[string]string{"OPENAI_API_KEY": "test-key"}); err != nil {
		t.Fatalf("set image generation failed: %v", err)
	}

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:        "oc_chat",
		ReceiveIDType:    "chat_id",
		BotName:          "Mea",
		SoulPath:         soulPath,
		SenderUserID:     "ou_user",
		Text:             "hello",
		ResponseMode:     jobResponseModeSend,
		ResourceScopeKey: "chat_id:oc_chat",
	})

	waitForCondition(t, 2*time.Second, func() bool {
		sender.mu.Lock()
		defer sender.mu.Unlock()
		return sender.sendImageCalls == 1
	}, "expected generated image to be sent")
	provider.mu.Lock()
	if len(provider.requests) != 1 {
		provider.mu.Unlock()
		t.Fatalf("expected one image generation request, got %d", len(provider.requests))
	}
	req := provider.requests[0]
	provider.mu.Unlock()
	if len(req.ReferenceImages) != 1 || req.ReferenceImages[0] != refImage {
		t.Fatalf("unexpected reference images: %#v", req.ReferenceImages)
	}
	if req.UserID != "ou_user" {
		t.Fatalf("unexpected image user id: %q", req.UserID)
	}
	sender.mu.Lock()
	defer sender.mu.Unlock()
	if sender.sendImages[0] != "img_uploaded" {
		t.Fatalf("unexpected sent image key: %#v", sender.sendImages)
	}
}

func TestProcessor_SetImageGenerationPreservesOpenAIParams(t *testing.T) {
	sender := &senderStub{resourceRoot: t.TempDir()}
	processor := NewProcessor(codexStub{}, sender, "failed", "thinking")

	var captured config.ImageGenerationConfig
	processor.newImageProvider = func(cfg config.ImageGenerationConfig, _ map[string]string) (imagegen.Provider, error) {
		captured = cfg
		return &fakeImageProvider{}, nil
	}

	if err := processor.SetImageGeneration(config.ImageGenerationConfig{
		Enabled:               true,
		Provider:              " openai ",
		Model:                 " gpt-image-1-mini ",
		BaseURL:               " https://example.invalid/v1 ",
		TimeoutSecs:           60,
		Moderation:            " low ",
		N:                     1,
		OutputCompression:     100,
		ResponseFormat:        " b64_json ",
		Size:                  " 1024x1024 ",
		Quality:               " low ",
		Background:            " auto ",
		OutputFormat:          " png ",
		PartialImages:         2,
		Stream:                true,
		Style:                 " vivid ",
		InputFidelity:         " low ",
		MaskPath:              " /tmp/mask.png ",
		UseCurrentAttachments: false,
	}, map[string]string{"OPENAI_API_KEY": "test-key"}); err != nil {
		t.Fatalf("set image generation failed: %v", err)
	}

	if captured.Moderation != "low" {
		t.Fatalf("unexpected moderation: %q", captured.Moderation)
	}
	if captured.N != 1 {
		t.Fatalf("unexpected n: %d", captured.N)
	}
	if captured.OutputCompression != 100 {
		t.Fatalf("unexpected output_compression: %d", captured.OutputCompression)
	}
	if captured.ResponseFormat != "b64_json" {
		t.Fatalf("unexpected response_format: %q", captured.ResponseFormat)
	}
	if captured.PartialImages != 2 {
		t.Fatalf("unexpected partial_images: %d", captured.PartialImages)
	}
	if !captured.Stream {
		t.Fatalf("expected stream to stay enabled")
	}
	if captured.Style != "vivid" {
		t.Fatalf("unexpected style: %q", captured.Style)
	}
	if captured.MaskPath != "/tmp/mask.png" {
		t.Fatalf("unexpected mask_path: %q", captured.MaskPath)
	}

	snapshot := processor.runtimeSnapshot()
	if snapshot.imageGeneration.OutputCompression != 100 {
		t.Fatalf("unexpected runtime output_compression: %d", snapshot.imageGeneration.OutputCompression)
	}
	if snapshot.imageGeneration.Moderation != "low" {
		t.Fatalf("unexpected runtime moderation: %q", snapshot.imageGeneration.Moderation)
	}
}

func TestProcessor_GeneratedImageRepliesDirectlyInChatScene(t *testing.T) {
	sender := &senderStub{resourceRoot: t.TempDir()}
	provider := &fakeImageProvider{}
	processor := NewProcessor(codexStub{}, sender, "failed", "thinking")

	err := processor.generateAndSendImage(
		context.Background(),
		processorRuntimeSnapshot{
			imageGeneration: config.ImageGenerationConfig{
				OutputFormat: "png",
			},
			imageProvider: provider,
		},
		Job{
			ReceiveID:          "oc_chat",
			ReceiveIDType:      "chat_id",
			SourceMessageID:    "om_src",
			Scene:              jobSceneChat,
			CreateFeishuThread: false,
			ResourceScopeKey:   "chat_id:oc_chat|scene:chat",
			BotName:            "Mea",
		},
		roleplayEnvelope{
			ActionLine: "抬手打招呼",
			Speech:     "你好呀",
		},
		"",
	)
	if err != nil {
		t.Fatalf("generateAndSendImage failed: %v", err)
	}

	sender.mu.Lock()
	defer sender.mu.Unlock()
	if sender.replyImageCalls != 1 {
		t.Fatalf("expected one image reply, got %d", sender.replyImageCalls)
	}
	if sender.replyImageDirectCalls != 1 {
		t.Fatalf("expected direct image reply in chat scene, got %d", sender.replyImageDirectCalls)
	}
	if len(sender.replyTargets) != 1 || sender.replyTargets[0] != "om_src" {
		t.Fatalf("unexpected reply targets: %#v", sender.replyTargets)
	}
	if sender.sendImageCalls != 0 {
		t.Fatalf("expected no image send fallback, got %d", sender.sendImageCalls)
	}
}

func TestParseSoulDocument_UsesFrontmatterImageRefs(t *testing.T) {
	doc := parseSoulDocument("---\nimage_refs:\n  - refs/base.png\n  - refs/face.png\nimage_generation:\n  min_reply_will: 60\noutput_contract:\n  hidden_tags:\n    - reply_will\n    - motion\n  reply_will_tag: reply_will\n  reply_will_field: reply_will\n  motion_tag: motion\n  suppress_token: \"[[NO_REPLY]]\"\n---\n角色正文")
	if doc.Body != "角色正文" {
		t.Fatalf("unexpected soul body: %q", doc.Body)
	}
	if len(doc.ImageRefs) != 2 || doc.ImageRefs[0] != "refs/base.png" || doc.ImageRefs[1] != "refs/face.png" {
		t.Fatalf("unexpected image refs: %#v", doc.ImageRefs)
	}
	if doc.OutputContract.ReplyWillTag != "reply_will" || doc.OutputContract.ReplyWillField != "reply_will" {
		t.Fatalf("unexpected output contract: %#v", doc.OutputContract)
	}
	if doc.OutputContract.MotionTag != "motion" {
		t.Fatalf("unexpected motion tag: %#v", doc.OutputContract)
	}
	if doc.OutputContract.SuppressToken != "[[NO_REPLY]]" {
		t.Fatalf("unexpected suppress token: %#v", doc.OutputContract)
	}
	if doc.ImageGeneration.MinReplyWill != 60 {
		t.Fatalf("unexpected image_generation.min_reply_will: %#v", doc.ImageGeneration)
	}
}

func TestProcessor_SkipsImageGenerationForNoReplyToken(t *testing.T) {
	sender := &senderStub{resourceRoot: t.TempDir()}
	provider := &fakeImageProvider{}
	processor := NewProcessor(codexStub{
		resp: "<reply_will>\n快乐: 10%\n惊奇: 0%\n安全: 20%\n喜爱: 0%\n悲伤: 10%\n愤怒: 0%\nreply_will: 20%\n</reply_will>\n[[NO_REPLY]]",
	}, sender, "failed", "thinking")
	processor.newImageProvider = func(config.ImageGenerationConfig, map[string]string) (imagegen.Provider, error) {
		return provider, nil
	}
	if err := processor.SetImageGeneration(config.ImageGenerationConfig{
		Enabled:     true,
		Provider:    "openai",
		Model:       "gpt-image-1.5",
		TimeoutSecs: 60,
	}, nil); err != nil {
		t.Fatalf("set image generation failed: %v", err)
	}

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:     "oc_chat",
		ReceiveIDType: "chat_id",
		Text:          "hello",
		NoReplyToken:  "[[NO_REPLY]]",
		SoulDoc: soulDocument{
			ImageGeneration: soulImageGenerationConfig{
				MinReplyWill: 50,
			},
			OutputContract: outputContract{
				ReplyWillTag:   "reply_will",
				ReplyWillField: "reply_will",
				MotionTag:      "motion",
				SuppressToken:  "[[NO_REPLY]]",
			},
		},
		ResponseMode: jobResponseModeSend,
	})

	provider.mu.Lock()
	defer provider.mu.Unlock()
	if len(provider.requests) != 0 {
		t.Fatalf("expected no image request, got %#v", provider.requests)
	}
	sender.mu.Lock()
	defer sender.mu.Unlock()
	if sender.sendImageCalls != 0 {
		t.Fatalf("expected no image send, got %d", sender.sendImageCalls)
	}
}
