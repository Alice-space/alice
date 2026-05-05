package connector

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	llm "github.com/Alice-space/alice/internal/llm"
)

func TestApp_OnMessageReceive_WorkSceneRestoresSeedRouteAfterRestartWithMention(t *testing.T) {
	cfg := configForGroupScenesTest()
	statePath := filepath.Join(t.TempDir(), "session_state.json")

	processor := NewProcessor(codexStub{resp: "ok"}, nil, "", "")
	if err := processor.LoadSessionState(statePath); err != nil {
		t.Fatalf("init session state failed: %v", err)
	}
	app := NewApp(cfg, processor)
	app.SetBotOpenID("ou_bot")

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
	for i := 0; i < maxReplyMessageIDs+8; i++ {
		processor.bindReplyMessage(sessionKey, fmt.Sprintf("om_extra_%02d", i))
	}
	if containsString(processor.sessions[sessionKey].ReplyMessageIDs, "om_work_seed_root") {
		t.Fatalf("seed message id should not rely on reply message id storage, got ids=%q", processor.sessions[sessionKey].ReplyMessageIDs)
	}

	if err := processor.FlushSessionState(); err != nil {
		t.Fatalf("flush session state failed: %v", err)
	}

	processorAfterRestart := NewProcessor(codexStub{resp: "ok"}, nil, "", "")
	if err := processorAfterRestart.LoadSessionState(statePath); err != nil {
		t.Fatalf("reload session state failed: %v", err)
	}
	appAfterRestart := NewApp(cfg, processorAfterRestart)
	appAfterRestart.SetBotOpenID("ou_bot")

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

	sessionKey := buildWorkSessionKey("chat_id", "oc_chat", "om_root")
	threadAlias := "chat_id:oc_chat|thread:omt_work_1"
	processor.setWorkThreadID(sessionKey, "omt_work_1")
	for i := 0; i < maxReplyMessageIDs+16; i++ {
		processor.bindReplyMessage(sessionKey, fmt.Sprintf("om_extra_%02d", i))
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

	if resolved := processorAfterRestart.resolveSessionLookup(threadAlias); resolved != sessionKey {
		t.Fatalf("thread alias should resolve after restart, got %q want %q", resolved, sessionKey)
	}
}

func TestProcessor_LoadSessionState_PreservesRotatedChatSceneSession(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "session_state.json")

	processor := NewProcessor(codexStub{resp: "ok"}, nil, "", "")
	if err := processor.LoadSessionState(statePath); err != nil {
		t.Fatalf("init session state failed: %v", err)
	}

	baseSessionKey := restoreChatSceneKey("chat_id", "oc_chat")
	oldThreadID := "thread_old"
	processor.setThreadID(baseSessionKey, oldThreadID)
	oldKey, currentKey := processor.resetChatSceneSession("chat_id", "oc_chat")
	if oldKey == "" || currentKey == "" {
		t.Fatalf("expected chat session keys, got old=%q current=%q", oldKey, currentKey)
	}
	if currentKey != baseSessionKey {
		t.Fatalf("expected session key to stay the same, got %q", currentKey)
	}

	if err := processor.FlushSessionState(); err != nil {
		t.Fatalf("flush session state failed: %v", err)
	}

	processorAfterRestart := NewProcessor(codexStub{resp: "ok"}, nil, "", "")
	if err := processorAfterRestart.LoadSessionState(statePath); err != nil {
		t.Fatalf("reload session state failed: %v", err)
	}

	if resolved := processorAfterRestart.resolveSessionLookup(baseSessionKey); resolved != baseSessionKey {
		t.Fatalf("base chat key should resolve to same key after restart, got %q want %q", resolved, baseSessionKey)
	}
	if threadID := processorAfterRestart.getThreadID(baseSessionKey); threadID != "" {
		t.Fatalf("chat session should not retain old thread id after clear, got %q", threadID)
	}
}

func TestProcessor_LoadSessionState_PreservesUsageStats(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "session_state.json")

	processor := NewProcessor(codexStub{resp: "ok"}, nil, "", "")
	processor.SetStatusIdentity("alice", "Alice")
	if err := processor.LoadSessionState(statePath); err != nil {
		t.Fatalf("init session state failed: %v", err)
	}

	sessionKey := buildWorkSessionKey("chat_id", "oc_chat", "om_root")
	processor.recordSessionUsage(sessionKey, llm.Usage{
		InputTokens:       120,
		CachedInputTokens: 40,
		OutputTokens:      12,
	})

	if err := processor.FlushSessionState(); err != nil {
		t.Fatalf("flush session state failed: %v", err)
	}

	processorAfterRestart := NewProcessor(codexStub{resp: "ok"}, nil, "", "")
	if err := processorAfterRestart.LoadSessionState(statePath); err != nil {
		t.Fatalf("reload session state failed: %v", err)
	}

	state := processorAfterRestart.sessions[sessionKey]
	if state.ScopeKey != "chat_id:oc_chat" {
		t.Fatalf("unexpected scope key after reload: %q", state.ScopeKey)
	}
	if state.Usage.InputTokens != 120 {
		t.Fatalf("unexpected input tokens after reload: %d", state.Usage.InputTokens)
	}
	if state.Usage.CachedInputTokens != 40 {
		t.Fatalf("unexpected cached input tokens after reload: %d", state.Usage.CachedInputTokens)
	}
	if state.Usage.OutputTokens != 12 {
		t.Fatalf("unexpected output tokens after reload: %d", state.Usage.OutputTokens)
	}
	if state.Usage.TotalTokens() != 132 {
		t.Fatalf("unexpected total tokens after reload: %d", state.Usage.TotalTokens())
	}
	if state.Usage.Turns != 1 {
		t.Fatalf("unexpected turns after reload: %d", state.Usage.Turns)
	}
}
