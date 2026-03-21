package connector

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestApp_OnMessageReceive_WorkSceneRestoresSeedRouteAfterRestartWithMention(t *testing.T) {
	cfg := configForGroupScenesTest()
	statePath := filepath.Join(t.TempDir(), "session_state.json")

	processor := NewProcessor(codexStub{resp: "ok"}, nil, "", "")
	if err := processor.LoadSessionState(statePath); err != nil {
		t.Fatalf("init session state failed: %v", err)
	}
	app := NewApp(cfg, processor)

	start := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_work_seed_start"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_work_seed_root"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"<at user_id=\"ou_bot\">Alice</at> #work 先开一个工作线程"}`),
				ChatId:      strPtr("oc_chat"),
				ChatType:    strPtr("group"),
				Mentions: []*larkim.MentionEvent{
					{
						Id: &larkim.UserId{OpenId: strPtr("ou_bot")},
					},
				},
			},
		},
	}

	if err := app.onMessageReceive(context.Background(), start); err != nil {
		t.Fatalf("unexpected work start error: %v", err)
	}
	if got := len(app.queue); got != 1 {
		t.Fatalf("expected queue len 1, got %d", got)
	}
	startJob := <-app.queue
	sessionKey := startJob.SessionKey
	if sessionKey == "" {
		t.Fatal("expected work session key")
	}

	baseKey := buildSessionKey(startJob.ReceiveIDType, startJob.ReceiveID)
	if baseKey == "" {
		t.Fatal("expected base session key")
	}
	rootAlias := baseKey + "|message:om_work_seed_root"
	for i := 0; i < maxSessionAliases+8; i++ {
		processor.rememberSessionAliases(sessionKey, fmt.Sprintf("%s|message:om_extra_%02d", baseKey, i))
	}
	if containsSessionAlias(processor.sessions[sessionKey].Aliases, rootAlias) {
		t.Fatalf("seed alias should not rely on regular alias storage, got aliases=%q", processor.sessions[sessionKey].Aliases)
	}

	if err := processor.FlushSessionState(); err != nil {
		t.Fatalf("flush session state failed: %v", err)
	}

	processorAfterRestart := NewProcessor(codexStub{resp: "ok"}, nil, "", "")
	if err := processorAfterRestart.LoadSessionState(statePath); err != nil {
		t.Fatalf("reload session state failed: %v", err)
	}
	appAfterRestart := NewApp(cfg, processorAfterRestart)

	followup := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_work_seed_followup"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_work_seed_followup"),
				ThreadId:    strPtr("omt_work_seed_1"),
				RootId:      strPtr("om_work_seed_root"),
				ParentId:    strPtr("om_work_seed_root"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"<at user_id=\"ou_bot\">Alice</at> 重启后继续在原 thread 里回复"}`),
				ChatId:      strPtr("oc_chat"),
				ChatType:    strPtr("group"),
				Mentions: []*larkim.MentionEvent{
					{
						Id: &larkim.UserId{OpenId: strPtr("ou_bot")},
					},
				},
			},
		},
	}

	if err := appAfterRestart.onMessageReceive(context.Background(), followup); err != nil {
		t.Fatalf("unexpected work followup error: %v", err)
	}
	if got := len(appAfterRestart.queue); got != 1 {
		t.Fatalf("expected queue len 1 after restart, got %d", got)
	}
	got := <-appAfterRestart.queue
	if got.Scene != jobSceneWork {
		t.Fatalf("followup should stay in work scene, got %q", got.Scene)
	}
	if got.SessionKey != sessionKey {
		t.Fatalf("followup should reuse original session key, got %q want %q", got.SessionKey, sessionKey)
	}
}

func TestProcessor_LoadSessionState_PreservesWorkThreadID(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "session_state.json")

	processor := NewProcessor(codexStub{resp: "ok"}, nil, "", "")
	if err := processor.LoadSessionState(statePath); err != nil {
		t.Fatalf("init session state failed: %v", err)
	}

	sessionKey := buildWorkSceneSessionKey("chat_id", "oc_chat", "om_root")
	threadAlias := "chat_id:oc_chat|thread:omt_work_1"
	processor.setWorkThreadID(sessionKey, "omt_work_1")
	for i := 0; i < maxSessionAliases+16; i++ {
		processor.rememberSessionAliases(sessionKey, fmt.Sprintf("chat_id:oc_chat|message:om_extra_%02d", i))
	}

	state := processor.sessions[sessionKey]
	if state.WorkThreadID != "omt_work_1" {
		t.Fatalf("work thread id should be stored directly, got %q", state.WorkThreadID)
	}

	if err := processor.FlushSessionState(); err != nil {
		t.Fatalf("flush session state failed: %v", err)
	}

	processorAfterRestart := NewProcessor(codexStub{resp: "ok"}, nil, "", "")
	if err := processorAfterRestart.LoadSessionState(statePath); err != nil {
		t.Fatalf("reload session state failed: %v", err)
	}

	if resolved := processorAfterRestart.resolveCanonicalSessionKey(threadAlias); resolved != sessionKey {
		t.Fatalf("thread alias should resolve after restart, got %q want %q", resolved, sessionKey)
	}
}

func TestProcessor_LoadSessionState_PreservesRotatedChatSceneSession(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "session_state.json")

	processor := NewProcessor(codexStub{resp: "ok"}, nil, "", "")
	if err := processor.LoadSessionState(statePath); err != nil {
		t.Fatalf("init session state failed: %v", err)
	}

	baseSessionKey := buildChatSceneSessionKey("chat_id", "oc_chat")
	oldThreadID := "thread_old"
	processor.setThreadID(baseSessionKey, oldThreadID)
	_, rotatedSessionKey := processor.resetChatSceneSession("chat_id", "oc_chat")
	if rotatedSessionKey == "" || rotatedSessionKey == baseSessionKey {
		t.Fatalf("expected rotated chat session key, got %q", rotatedSessionKey)
	}

	if err := processor.FlushSessionState(); err != nil {
		t.Fatalf("flush session state failed: %v", err)
	}

	processorAfterRestart := NewProcessor(codexStub{resp: "ok"}, nil, "", "")
	if err := processorAfterRestart.LoadSessionState(statePath); err != nil {
		t.Fatalf("reload session state failed: %v", err)
	}

	if resolved := processorAfterRestart.resolveCanonicalSessionKey(baseSessionKey); resolved != rotatedSessionKey {
		t.Fatalf("base chat alias should resolve to rotated key after restart, got %q want %q", resolved, rotatedSessionKey)
	}
	if threadID := processorAfterRestart.getThreadID(rotatedSessionKey); threadID != "" {
		t.Fatalf("rotated chat session should not reuse old thread id after restart, got %q", threadID)
	}
}
