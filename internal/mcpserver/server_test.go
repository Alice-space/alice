package mcpserver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/Alice-space/alice/internal/mcpbridge"
	"github.com/Alice-space/alice/internal/runtimeapi"
)

type senderStub struct {
	sendTextCalls  int
	lastSendText   string
	sendImageCalls int
	lastImageKey   string
	sendFileCalls  int
	lastFileKey    string

	replyTextCalls     int
	lastReplyText      string
	lastReplyTextMsgID string

	replyImageCalls     int
	lastReplyImageKey   string
	lastReplyImageMsgID string

	replyFileCalls     int
	lastReplyFileKey   string
	lastReplyFileMsgID string

	uploadImageKey string
	uploadFileKey  string
	uploadImageErr error
	uploadFileErr  error
}

func (s *senderStub) SendText(_ context.Context, _, _ string, text string) error {
	s.sendTextCalls++
	s.lastSendText = strings.TrimSpace(text)
	return nil
}

func (s *senderStub) SendImage(_ context.Context, _, _ string, imageKey string) error {
	s.sendImageCalls++
	s.lastImageKey = strings.TrimSpace(imageKey)
	return nil
}

func (s *senderStub) SendFile(_ context.Context, _, _ string, fileKey string) error {
	s.sendFileCalls++
	s.lastFileKey = strings.TrimSpace(fileKey)
	return nil
}

func (s *senderStub) ReplyText(_ context.Context, sourceMessageID, text string) (string, error) {
	s.replyTextCalls++
	s.lastReplyText = strings.TrimSpace(text)
	s.lastReplyTextMsgID = strings.TrimSpace(sourceMessageID)
	return "om_reply_text", nil
}

func (s *senderStub) ReplyImage(_ context.Context, sourceMessageID, imageKey string) (string, error) {
	s.replyImageCalls++
	s.lastReplyImageKey = strings.TrimSpace(imageKey)
	s.lastReplyImageMsgID = strings.TrimSpace(sourceMessageID)
	return "om_reply_image", nil
}

func (s *senderStub) ReplyFile(_ context.Context, sourceMessageID, fileKey string) (string, error) {
	s.replyFileCalls++
	s.lastReplyFileKey = strings.TrimSpace(fileKey)
	s.lastReplyFileMsgID = strings.TrimSpace(sourceMessageID)
	return "om_reply_file", nil
}

func (s *senderStub) UploadImage(_ context.Context, _ string) (string, error) {
	if s.uploadImageErr != nil {
		return "", s.uploadImageErr
	}
	if strings.TrimSpace(s.uploadImageKey) != "" {
		return s.uploadImageKey, nil
	}
	return "img_uploaded", nil
}

func (s *senderStub) UploadFile(_ context.Context, _, _ string) (string, error) {
	if s.uploadFileErr != nil {
		return "", s.uploadFileErr
	}
	if strings.TrimSpace(s.uploadFileKey) != "" {
		return s.uploadFileKey, nil
	}
	return "file_uploaded", nil
}

func TestHandleSendImage_UsesImageKeyAndCaption(t *testing.T) {
	sender := &senderStub{}
	svc := &service{
		sender: sender,
		getenv: func(key string) string {
			switch key {
			case "ALICE_MCP_RECEIVE_ID_TYPE":
				return "chat_id"
			case "ALICE_MCP_RECEIVE_ID":
				return "oc_chat"
			default:
				return ""
			}
		},
	}

	result, err := svc.handleSendImage(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"image_key": "img_123",
			"caption":   "done",
		}},
	})
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}
	if result == nil || result.IsError {
		t.Fatalf("expected non-error result, got %#v", result)
	}
	if sender.sendImageCalls != 1 || sender.lastImageKey != "img_123" {
		t.Fatalf("unexpected send image state: %+v", sender)
	}
	if sender.sendTextCalls != 1 || sender.lastSendText != "done" {
		t.Fatalf("unexpected caption send state: %+v", sender)
	}
}

func TestHandleSendImage_UsesThreadReplyWhenSourceMessageInContext(t *testing.T) {
	sender := &senderStub{}
	svc := &service{
		sender: sender,
		getenv: func(key string) string {
			switch key {
			case mcpbridge.EnvReceiveIDType:
				return "chat_id"
			case mcpbridge.EnvReceiveID:
				return "oc_chat"
			case mcpbridge.EnvSourceMessageID:
				return "om_source"
			default:
				return ""
			}
		},
	}

	result, err := svc.handleSendImage(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"image_key": "img_123",
			"caption":   "done",
		}},
	})
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}
	if result == nil || result.IsError {
		t.Fatalf("expected non-error result, got %#v", result)
	}
	if sender.replyImageCalls != 1 || sender.lastReplyImageKey != "img_123" || sender.lastReplyImageMsgID != "om_source" {
		t.Fatalf("expected thread image reply, got %+v", sender)
	}
	if sender.replyTextCalls != 1 || sender.lastReplyText != "done" || sender.lastReplyTextMsgID != "om_source" {
		t.Fatalf("expected thread caption reply, got %+v", sender)
	}
	if sender.sendImageCalls != 0 || sender.sendTextCalls != 0 {
		t.Fatalf("should not use direct send when source message is available, got %+v", sender)
	}
}

func TestHandleSendImage_MissingSessionContext(t *testing.T) {
	sender := &senderStub{}
	svc := &service{sender: sender, getenv: func(string) string { return "" }}

	result, err := svc.handleSendImage(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{"image_key": "img_123"}},
	})
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("expected tool error result, got %#v", result)
	}
	if result.Content == nil || len(result.Content) == 0 {
		t.Fatalf("expected context requirement hint in error content, got %#v", result.Content)
	}
	firstText, ok := result.Content[0].(mcp.TextContent)
	if !ok || !strings.Contains(firstText.Text, "must run in connector session context") {
		t.Fatalf("expected context requirement hint in error, got %#v", result.Content)
	}
	if sender.sendImageCalls != 0 {
		t.Fatalf("should not send image on invalid context, got %d", sender.sendImageCalls)
	}
}

func TestHandleSendImage_LoadSessionContextFromParentProcessFallback(t *testing.T) {
	sender := &senderStub{}
	svc := &service{
		sender:  sender,
		getenv:  func(string) string { return "" },
		getppid: func() int { return 200 },
		readFile: func(path string) ([]byte, error) {
			switch path {
			case "/proc/200/environ":
				return []byte(
					"ALICE_MCP_RECEIVE_ID_TYPE=chat_id\x00" +
						"ALICE_MCP_RECEIVE_ID=oc_chat\x00" +
						"ALICE_MCP_SOURCE_MESSAGE_ID=om_source\x00",
				), nil
			case "/proc/200/status":
				return []byte("Name:\tcodex\nPPid:\t1\n"), nil
			default:
				return nil, errors.New("not found")
			}
		},
	}

	result, err := svc.handleSendImage(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"image_key": "img_123",
			"caption":   "done",
		}},
	})
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}
	if result == nil || result.IsError {
		t.Fatalf("expected non-error result, got %#v", result)
	}
	if sender.replyImageCalls != 1 || sender.lastReplyImageKey != "img_123" || sender.lastReplyImageMsgID != "om_source" {
		t.Fatalf("expected thread image reply from fallback context, got %+v", sender)
	}
	if sender.replyTextCalls != 1 || sender.lastReplyText != "done" || sender.lastReplyTextMsgID != "om_source" {
		t.Fatalf("expected thread caption reply from fallback context, got %+v", sender)
	}
}

func TestHandleSendImage_UsesRuntimeAPIWhenConfigured(t *testing.T) {
	requestCount := 0
	runtimeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if got := r.Header.Get("Authorization"); got != "Bearer token-123" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		if got := r.Header.Get(runtimeapi.HeaderReceiveID); got != "oc_chat" {
			t.Fatalf("unexpected receive id header: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","type":"image","image_key":"img_runtime"}`))
	}))
	defer runtimeServer.Close()

	sender := &senderStub{}
	svc := &service{
		sender:        sender,
		runtimeClient: runtimeapi.NewClient(runtimeServer.URL, "token-123"),
		getenv: func(key string) string {
			switch key {
			case mcpbridge.EnvReceiveIDType:
				return "chat_id"
			case mcpbridge.EnvReceiveID:
				return "oc_chat"
			default:
				return ""
			}
		},
	}

	result, err := svc.handleSendImage(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"image_key": "img_runtime",
		}},
	})
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}
	if result == nil || result.IsError {
		t.Fatalf("expected non-error result, got %#v", result)
	}
	if requestCount != 1 {
		t.Fatalf("expected runtime api to be called once, got %d", requestCount)
	}
	if sender.sendImageCalls != 0 {
		t.Fatalf("expected direct sender to be skipped, got %+v", sender)
	}
}

func TestHandleSendImage_DoesNotAllowTargetOverrideArguments(t *testing.T) {
	sender := &senderStub{}
	svc := &service{sender: sender, getenv: func(string) string { return "" }}

	result, err := svc.handleSendImage(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"image_key":         "img_123",
			"receive_id_type":   "chat_id",
			"receive_id":        "oc_chat",
			"source_message_id": "om_source",
			"caption":           "done",
		}},
	})
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("expected tool error result, got %#v", result)
	}
	if sender.sendImageCalls != 0 || sender.replyImageCalls != 0 {
		t.Fatalf("should not send image when context is missing, got %+v", sender)
	}
}

func TestHandleSendFile_UploadFromPath(t *testing.T) {
	sender := &senderStub{uploadFileKey: "file_abc"}
	svc := &service{
		sender: sender,
		getenv: func(key string) string {
			switch key {
			case "ALICE_MCP_RECEIVE_ID_TYPE":
				return "chat_id"
			case "ALICE_MCP_RECEIVE_ID":
				return "oc_chat"
			case "ALICE_MCP_RESOURCE_ROOT":
				return "/tmp/root"
			default:
				return ""
			}
		},
	}

	result, err := svc.handleSendFile(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"path": "/tmp/root/a.txt",
		}},
	})
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}
	if result == nil || result.IsError {
		t.Fatalf("expected non-error result, got %#v", result)
	}
	if sender.sendFileCalls != 1 || sender.lastFileKey != "file_abc" {
		t.Fatalf("unexpected send file state: %+v", sender)
	}
}

func TestHandleSendFile_UsesThreadReplyWhenSourceMessageInContext(t *testing.T) {
	sender := &senderStub{}
	svc := &service{
		sender: sender,
		getenv: func(key string) string {
			switch key {
			case mcpbridge.EnvReceiveIDType:
				return "chat_id"
			case mcpbridge.EnvReceiveID:
				return "oc_chat"
			case mcpbridge.EnvSourceMessageID:
				return "om_source"
			default:
				return ""
			}
		},
	}

	result, err := svc.handleSendFile(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"file_key": "file_123",
			"caption":  "done",
		}},
	})
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}
	if result == nil || result.IsError {
		t.Fatalf("expected non-error result, got %#v", result)
	}
	if sender.replyFileCalls != 1 || sender.lastReplyFileKey != "file_123" || sender.lastReplyFileMsgID != "om_source" {
		t.Fatalf("expected thread file reply, got %+v", sender)
	}
	if sender.replyTextCalls != 1 || sender.lastReplyText != "done" || sender.lastReplyTextMsgID != "om_source" {
		t.Fatalf("expected thread caption reply, got %+v", sender)
	}
	if sender.sendFileCalls != 0 || sender.sendTextCalls != 0 {
		t.Fatalf("should not use direct send when source message is available, got %+v", sender)
	}
}

func TestHandleSendFile_DoesNotAllowTargetOverrideArguments(t *testing.T) {
	sender := &senderStub{}
	svc := &service{sender: sender, getenv: func(string) string { return "" }}

	result, err := svc.handleSendFile(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"file_key":          "file_123",
			"receive_id_type":   "chat_id",
			"receive_id":        "oc_chat",
			"source_message_id": "om_source",
			"caption":           "done",
		}},
	})
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("expected tool error result, got %#v", result)
	}
	if sender.sendFileCalls != 0 || sender.replyFileCalls != 0 {
		t.Fatalf("should not send file when context is missing, got %+v", sender)
	}
}

func TestHandleSendFile_RejectPathOutsideRoot(t *testing.T) {
	sender := &senderStub{}
	svc := &service{
		sender: sender,
		getenv: func(key string) string {
			switch key {
			case "ALICE_MCP_RECEIVE_ID_TYPE":
				return "chat_id"
			case "ALICE_MCP_RECEIVE_ID":
				return "oc_chat"
			case "ALICE_MCP_RESOURCE_ROOT":
				return "/tmp/root"
			default:
				return ""
			}
		},
	}

	result, err := svc.handleSendFile(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"path": "/tmp/outside.txt",
		}},
	})
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("expected tool error result, got %#v", result)
	}
	if sender.sendFileCalls != 0 {
		t.Fatalf("should not send file on invalid path, got %d", sender.sendFileCalls)
	}
}

func TestHandleSendImage_UploadFailureReturnedAsToolError(t *testing.T) {
	sender := &senderStub{uploadImageErr: errors.New("boom")}
	svc := &service{
		sender: sender,
		getenv: func(key string) string {
			switch key {
			case "ALICE_MCP_RECEIVE_ID_TYPE":
				return "chat_id"
			case "ALICE_MCP_RECEIVE_ID":
				return "oc_chat"
			case "ALICE_MCP_RESOURCE_ROOT":
				return "/tmp/root"
			default:
				return ""
			}
		},
	}

	result, err := svc.handleSendImage(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{"path": "/tmp/root/a.png"}},
	})
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("expected upload failure as tool error result, got %#v", result)
	}
}

func TestValidatePathUnderRoot(t *testing.T) {
	if err := validatePathUnderRoot("/tmp/root/a.txt", "/tmp/root"); err != nil {
		t.Fatalf("expected allowed path, got %v", err)
	}
	if err := validatePathUnderRoot("/tmp/other/a.txt", "/tmp/root"); err == nil {
		t.Fatal("expected root validation error")
	}
}
