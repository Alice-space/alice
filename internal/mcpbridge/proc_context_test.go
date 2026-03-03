package mcpbridge

import (
	"errors"
	"testing"
)

func TestMergeSessionContext(t *testing.T) {
	primary := SessionContext{
		ReceiveIDType: "chat_id",
		ReceiveID:     "",
	}
	fallback := SessionContext{
		ReceiveIDType:   "open_id",
		ReceiveID:       "ou_123",
		ResourceRoot:    "/tmp/root",
		SourceMessageID: "om_source",
		ActorUserID:     "ou_actor",
		ActorOpenID:     "ou_open",
		ChatType:        "group",
		SessionKey:      "chat_id:oc_chat|thread:omt_thread",
	}

	merged := MergeSessionContext(primary, fallback)
	if merged.ReceiveIDType != "chat_id" {
		t.Fatalf("expected primary receive id type, got %+v", merged)
	}
	if merged.ReceiveID != "ou_123" ||
		merged.ResourceRoot != "/tmp/root" ||
		merged.SourceMessageID != "om_source" ||
		merged.ActorUserID != "ou_actor" ||
		merged.ActorOpenID != "ou_open" ||
		merged.ChatType != "group" ||
		merged.SessionKey != "chat_id:oc_chat|thread:omt_thread" {
		t.Fatalf("unexpected merge result: %+v", merged)
	}
}

func TestSessionContextFromProcessTree(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		switch path {
		case "/proc/300/environ":
			return []byte("FOO=bar\x00"), nil
		case "/proc/300/status":
			return []byte("Name:\tbash\nPPid:\t200\n"), nil
		case "/proc/200/environ":
			return []byte(
				"ALICE_MCP_RECEIVE_ID_TYPE=chat_id\x00" +
					"ALICE_MCP_RECEIVE_ID=oc_chat\x00" +
					"ALICE_MCP_ACTOR_USER_ID=ou_actor\x00" +
					"ALICE_MCP_CHAT_TYPE=group\x00" +
					"ALICE_MCP_SESSION_KEY=chat_id:oc_chat|thread:omt_thread\x00",
			), nil
		case "/proc/200/status":
			return []byte("Name:\tcodex\nPPid:\t1\n"), nil
		default:
			return nil, errors.New("not found")
		}
	}

	ctx := SessionContextFromProcessTree(300, readFile, 8)
	if ctx.ReceiveIDType != "chat_id" ||
		ctx.ReceiveID != "oc_chat" ||
		ctx.ActorUserID != "ou_actor" ||
		ctx.ChatType != "group" ||
		ctx.SessionKey != "chat_id:oc_chat|thread:omt_thread" {
		t.Fatalf("unexpected process tree context: %+v", ctx)
	}
}
