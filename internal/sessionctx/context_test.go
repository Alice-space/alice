package sessionctx

import "testing"

func TestSessionContext_ToEnv(t *testing.T) {
	ctx := SessionContext{
		ReceiveIDType:   "chat_id",
		ReceiveID:       "oc_chat",
		SourceMessageID: "om_source",
		ActorUserID:     "ou_actor",
		ActorOpenID:     "ou_open",
		ChatType:        "group",
		SessionKey:      "chat_id:oc_chat|thread:omt_thread",
	}
	env := ctx.ToEnv()
	if env[EnvReceiveIDType] != "chat_id" {
		t.Fatalf("unexpected receive id type env: %#v", env)
	}
	if env[EnvReceiveID] != "oc_chat" {
		t.Fatalf("unexpected receive id env: %#v", env)
	}
	if env[EnvSourceMessageID] != "om_source" {
		t.Fatalf("unexpected source message env: %#v", env)
	}
	if env[EnvActorUserID] != "ou_actor" {
		t.Fatalf("unexpected actor user id env: %#v", env)
	}
	if env[EnvActorOpenID] != "ou_open" {
		t.Fatalf("unexpected actor open id env: %#v", env)
	}
	if env[EnvChatType] != "group" {
		t.Fatalf("unexpected chat type env: %#v", env)
	}
	if env[EnvSessionKey] != "chat_id:oc_chat|thread:omt_thread" {
		t.Fatalf("unexpected session key env: %#v", env)
	}
}

func TestSessionContextFromEnv(t *testing.T) {
	ctx := SessionContextFromEnv(func(key string) string {
		switch key {
		case EnvReceiveIDType:
			return "chat_id"
		case EnvReceiveID:
			return "oc_chat"
		case EnvSourceMessageID:
			return "om_source"
		case EnvActorUserID:
			return "ou_actor"
		case EnvActorOpenID:
			return "ou_open"
		case EnvChatType:
			return "group"
		case EnvSessionKey:
			return "chat_id:oc_chat|thread:omt_thread"
		default:
			return ""
		}
	})
	if ctx.ReceiveIDType != "chat_id" ||
		ctx.ReceiveID != "oc_chat" ||
		ctx.SourceMessageID != "om_source" ||
		ctx.ActorUserID != "ou_actor" ||
		ctx.ActorOpenID != "ou_open" ||
		ctx.ChatType != "group" ||
		ctx.SessionKey != "chat_id:oc_chat|thread:omt_thread" {
		t.Fatalf("unexpected context: %+v", ctx)
	}
}

func TestSessionContextValidate(t *testing.T) {
	if err := (SessionContext{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"}).Validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
	if err := (SessionContext{ReceiveIDType: "", ReceiveID: "oc_chat"}).Validate(); err == nil {
		t.Fatal("expected missing receive id type validation error")
	}
	if err := (SessionContext{ReceiveIDType: "chat_id", ReceiveID: ""}).Validate(); err == nil {
		t.Fatal("expected missing receive id validation error")
	}
}
