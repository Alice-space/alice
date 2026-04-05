package runtimeapi

import (
	"context"
	"testing"

	"github.com/Alice-space/alice/internal/config"
	"github.com/Alice-space/alice/internal/mcpbridge"
)

type runtimeMessageSenderStub struct {
	replyTextCalls        int
	replyTextDirectCalls  int
	replyImageCalls       int
	replyImageDirectCalls int
	replyFileCalls        int
	replyFileDirectCalls  int
	sendTextCalls         int
	sendImageCalls        int
	sendFileCalls         int
}

func (s *runtimeMessageSenderStub) SendText(context.Context, string, string, string) error {
	s.sendTextCalls++
	return nil
}

func (s *runtimeMessageSenderStub) SendImage(context.Context, string, string, string) error {
	s.sendImageCalls++
	return nil
}

func (s *runtimeMessageSenderStub) SendFile(context.Context, string, string, string) error {
	s.sendFileCalls++
	return nil
}

func (s *runtimeMessageSenderStub) UploadImage(context.Context, string) (string, error) {
	return "img_uploaded", nil
}

func (s *runtimeMessageSenderStub) UploadFile(context.Context, string, string) (string, error) {
	return "file_uploaded", nil
}

func (s *runtimeMessageSenderStub) ReplyText(context.Context, string, string) (string, error) {
	s.replyTextCalls++
	return "om_reply_text", nil
}

func (s *runtimeMessageSenderStub) ReplyTextDirect(context.Context, string, string) (string, error) {
	s.replyTextDirectCalls++
	return "om_reply_text_direct", nil
}

func (s *runtimeMessageSenderStub) ReplyImage(context.Context, string, string) (string, error) {
	s.replyImageCalls++
	return "om_reply_image", nil
}

func (s *runtimeMessageSenderStub) ReplyImageDirect(context.Context, string, string) (string, error) {
	s.replyImageDirectCalls++
	return "om_reply_image_direct", nil
}

func (s *runtimeMessageSenderStub) ReplyFile(context.Context, string, string) (string, error) {
	s.replyFileCalls++
	return "om_reply_file", nil
}

func (s *runtimeMessageSenderStub) ReplyFileDirect(context.Context, string, string) (string, error) {
	s.replyFileDirectCalls++
	return "om_reply_file_direct", nil
}

func TestDispatchImage_ChatSceneRepliesDirectly(t *testing.T) {
	sender := &runtimeMessageSenderStub{}
	server := NewServer("", "", sender, nil, config.Config{
		GroupScenes: config.GroupScenesConfig{
			Chat: config.GroupSceneConfig{CreateFeishuThread: false},
			Work: config.GroupSceneConfig{CreateFeishuThread: true},
		},
	})

	err := server.dispatchImage(context.Background(), mcpbridge.SessionContext{
		ReceiveIDType:   "chat_id",
		ReceiveID:       "oc_chat",
		ChatType:        "group",
		SourceMessageID: "om_src",
		SessionKey:      "chat_id:oc_chat|scene:chat",
	}, "img_123")
	if err != nil {
		t.Fatalf("dispatchImage failed: %v", err)
	}
	if sender.replyImageDirectCalls != 1 {
		t.Fatalf("expected direct image reply, got %d", sender.replyImageDirectCalls)
	}
	if sender.replyImageCalls != 0 {
		t.Fatalf("expected no threaded image reply, got %d", sender.replyImageCalls)
	}
	if sender.sendImageCalls != 0 {
		t.Fatalf("expected no image send fallback, got %d", sender.sendImageCalls)
	}
}

func TestDispatchText_ChatSceneRepliesDirectly(t *testing.T) {
	sender := &runtimeMessageSenderStub{}
	server := NewServer("", "", sender, nil, config.Config{
		GroupScenes: config.GroupScenesConfig{
			Chat: config.GroupSceneConfig{CreateFeishuThread: false},
		},
	})

	err := server.dispatchText(context.Background(), mcpbridge.SessionContext{
		ReceiveIDType:   "chat_id",
		ReceiveID:       "oc_chat",
		ChatType:        "group",
		SourceMessageID: "om_src",
		SessionKey:      "chat_id:oc_chat|scene:chat",
	}, "caption")
	if err != nil {
		t.Fatalf("dispatchText failed: %v", err)
	}
	if sender.replyTextDirectCalls != 1 {
		t.Fatalf("expected direct text reply, got %d", sender.replyTextDirectCalls)
	}
	if sender.replyTextCalls != 0 {
		t.Fatalf("expected no threaded text reply, got %d", sender.replyTextCalls)
	}
}

func TestDispatchFile_ChatSceneRepliesDirectly(t *testing.T) {
	sender := &runtimeMessageSenderStub{}
	server := NewServer("", "", sender, nil, config.Config{
		GroupScenes: config.GroupScenesConfig{
			Chat: config.GroupSceneConfig{CreateFeishuThread: false},
		},
	})

	err := server.dispatchFile(context.Background(), mcpbridge.SessionContext{
		ReceiveIDType:   "chat_id",
		ReceiveID:       "oc_chat",
		ChatType:        "group",
		SourceMessageID: "om_src",
		SessionKey:      "chat_id:oc_chat|scene:chat",
	}, "file_123")
	if err != nil {
		t.Fatalf("dispatchFile failed: %v", err)
	}
	if sender.replyFileDirectCalls != 1 {
		t.Fatalf("expected direct file reply, got %d", sender.replyFileDirectCalls)
	}
	if sender.replyFileCalls != 0 {
		t.Fatalf("expected no threaded file reply, got %d", sender.replyFileCalls)
	}
}

func TestDispatchImage_WorkSceneKeepsThreadReply(t *testing.T) {
	sender := &runtimeMessageSenderStub{}
	server := NewServer("", "", sender, nil, config.Config{
		GroupScenes: config.GroupScenesConfig{
			Chat: config.GroupSceneConfig{CreateFeishuThread: false},
			Work: config.GroupSceneConfig{CreateFeishuThread: true},
		},
	})

	err := server.dispatchImage(context.Background(), mcpbridge.SessionContext{
		ReceiveIDType:   "chat_id",
		ReceiveID:       "oc_chat",
		ChatType:        "group",
		SourceMessageID: "om_src",
		SessionKey:      "chat_id:oc_chat|scene:work|thread:omt_1",
	}, "img_123")
	if err != nil {
		t.Fatalf("dispatchImage failed: %v", err)
	}
	if sender.replyImageCalls != 1 {
		t.Fatalf("expected threaded image reply, got %d", sender.replyImageCalls)
	}
	if sender.replyImageDirectCalls != 0 {
		t.Fatalf("expected no direct image reply, got %d", sender.replyImageDirectCalls)
	}
}
