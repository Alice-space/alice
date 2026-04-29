package runtimeapi

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Alice-space/alice/internal/config"
	"github.com/Alice-space/alice/internal/sessionctx"
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
	uploadImagePaths      []string
	uploadFilePaths       []string
	uploadFileNames       []string
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

func (s *runtimeMessageSenderStub) UploadImage(_ context.Context, path string) (string, error) {
	s.uploadImagePaths = append(s.uploadImagePaths, path)
	return "img_uploaded", nil
}

func (s *runtimeMessageSenderStub) UploadFile(_ context.Context, path string, fileName string) (string, error) {
	s.uploadFilePaths = append(s.uploadFilePaths, path)
	s.uploadFileNames = append(s.uploadFileNames, fileName)
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

func TestRuntimeAPI_SendImagePathDoesNotRequireResourceRoot(t *testing.T) {
	sender := &runtimeMessageSenderStub{}
	server := NewServer("", "test-token", sender, nil, config.Config{})
	httpServer := httptest.NewServer(server.engine)
	defer httpServer.Close()
	client := NewClient(httpServer.URL, "test-token")
	path := filepath.Join(t.TempDir(), "image.png")

	result, err := client.SendImage(t.Context(), sessionctx.SessionContext{
		ReceiveIDType: "chat_id",
		ReceiveID:     "oc_chat",
		ChatType:      "group",
	}, ImageRequest{Path: path})
	if err != nil {
		t.Fatalf("send image failed: %v", err)
	}
	if result["image_key"] != "img_uploaded" {
		t.Fatalf("unexpected response: %#v", result)
	}
	if len(sender.uploadImagePaths) != 1 || sender.uploadImagePaths[0] != path {
		t.Fatalf("unexpected upload image paths: %#v", sender.uploadImagePaths)
	}
	if sender.sendImageCalls != 1 {
		t.Fatalf("expected image send, got %d", sender.sendImageCalls)
	}
}

func TestRuntimeAPI_SendFilePathDoesNotRequireResourceRoot(t *testing.T) {
	sender := &runtimeMessageSenderStub{}
	server := NewServer("", "test-token", sender, nil, config.Config{})
	httpServer := httptest.NewServer(server.engine)
	defer httpServer.Close()
	client := NewClient(httpServer.URL, "test-token")
	path := filepath.Join(t.TempDir(), "report.pdf")

	result, err := client.SendFile(t.Context(), sessionctx.SessionContext{
		ReceiveIDType: "chat_id",
		ReceiveID:     "oc_chat",
		ChatType:      "group",
	}, FileRequest{Path: path, FileName: "report.pdf"})
	if err != nil {
		t.Fatalf("send file failed: %v", err)
	}
	if result["file_key"] != "file_uploaded" {
		t.Fatalf("unexpected response: %#v", result)
	}
	if len(sender.uploadFilePaths) != 1 || sender.uploadFilePaths[0] != path {
		t.Fatalf("unexpected upload file paths: %#v", sender.uploadFilePaths)
	}
	if len(sender.uploadFileNames) != 1 || sender.uploadFileNames[0] != "report.pdf" {
		t.Fatalf("unexpected upload file names: %#v", sender.uploadFileNames)
	}
	if sender.sendFileCalls != 1 {
		t.Fatalf("expected file send, got %d", sender.sendFileCalls)
	}
}

func TestDispatchImage_ChatSceneRepliesDirectly(t *testing.T) {
	sender := &runtimeMessageSenderStub{}
	server := NewServer("", "", sender, nil, config.Config{
		GroupScenes: config.GroupScenesConfig{
			Chat: config.GroupSceneConfig{CreateFeishuThread: false},
			Work: config.GroupSceneConfig{CreateFeishuThread: true},
		},
	})

	err := server.dispatchImage(context.Background(), sessionctx.SessionContext{
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

	err := server.dispatchText(context.Background(), sessionctx.SessionContext{
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

	err := server.dispatchFile(context.Background(), sessionctx.SessionContext{
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

	err := server.dispatchImage(context.Background(), sessionctx.SessionContext{
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
