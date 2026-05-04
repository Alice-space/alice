package runtimeapi

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/config"
	"github.com/Alice-space/alice/internal/sessionctx"
)

func TestRuntimeAPI_MessagePermissionDenied(t *testing.T) {
	enabled := false
	server := NewServer("", "test-token", nil, nil, config.Config{
		Permissions: config.BotPermissionsConfig{
			RuntimeMessage: &enabled,
		},
	})
	httpServer := httptest.NewServer(server.engine)
	defer httpServer.Close()
	client := NewClient(httpServer.URL, "test-token")

	_, err := client.SendImage(t.Context(), sessionctx.SessionContext{
		ReceiveIDType: "chat_id",
		ReceiveID:     "oc_chat",
		ChatType:      "group",
	}, ImageRequest{ImageKey: "img_123"})
	if err == nil || !strings.Contains(err.Error(), "runtime message is disabled for this bot") {
		t.Fatalf("expected runtime message forbidden error, got %v", err)
	}
}

func TestRuntimeAPI_AutomationPermissionDenied(t *testing.T) {
	enabled := false
	server := NewServer("", "test-token", nil, automation.NewStore(t.TempDir()+"/automation.db"), config.Config{
		Permissions: config.BotPermissionsConfig{
			RuntimeAutomation: &enabled,
		},
	})
	httpServer := httptest.NewServer(server.engine)
	defer httpServer.Close()
	client := NewClient(httpServer.URL, "test-token")

	_, err := client.CreateTask(t.Context(), sessionctx.SessionContext{
		ReceiveIDType: "chat_id",
		ReceiveID:     "oc_chat",
		ActorUserID:   "ou_user",
		ChatType:      "group",
		SessionKey:    "chat_id:oc_chat|scene:chat",
	}, CreateTaskRequest{
		Prompt:       "hello",
		EverySeconds: 60,
	})
	if err == nil || !strings.Contains(err.Error(), "runtime automation is disabled for this bot") {
		t.Fatalf("expected runtime automation forbidden error, got %v", err)
	}
}
