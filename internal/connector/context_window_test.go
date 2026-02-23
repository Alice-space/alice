package connector

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestApp_OnMessageReceive_GroupTextWithoutMentionCachedAndMergedOnMention(t *testing.T) {
	cfg := configForTest()
	cfg.FeishuBotOpenID = "ou_bot"
	app := NewApp(cfg, nil)
	base := time.Date(2026, 2, 23, 9, 0, 0, 0, time.UTC)
	now := base
	app.now = func() time.Time { return now }

	textEvent := newGroupTextReceiveEvent(
		t,
		"evt_text_cache",
		"om_text_cache",
		"ou_user_1",
		"oc_chat",
		"先记一下：这个需求要补充背景信息",
		nil,
		"",
		"",
	)
	if err := app.onMessageReceive(context.Background(), textEvent); err != nil {
		t.Fatalf("unexpected text event error: %v", err)
	}
	if got := len(app.queue); got != 0 {
		t.Fatalf("expected queue len 0, got %d", got)
	}

	windowKey := buildMediaWindowKey("oc_chat", "open_id:ou_user_1")
	app.mu.Lock()
	cached := app.mediaWindow[windowKey]
	app.mu.Unlock()
	if len(cached) != 1 {
		t.Fatalf("expected 1 cached text entry, got %d", len(cached))
	}
	if cached[0].MessageType != "text" {
		t.Fatalf("expected cached message type text, got %s", cached[0].MessageType)
	}

	now = base.Add(30 * time.Second)
	mentionEvent := newGroupTextReceiveEvent(
		t,
		"evt_text_mention",
		"om_text_mention",
		"ou_user_1",
		"oc_chat",
		"<at user_id=\"ou_bot\">Alice</at> 帮我继续处理",
		botMentionsForTest("ou_bot"),
		"",
		"",
	)
	if err := app.onMessageReceive(context.Background(), mentionEvent); err != nil {
		t.Fatalf("unexpected mention event error: %v", err)
	}
	if got := len(app.queue); got != 1 {
		t.Fatalf("expected queue len 1, got %d", got)
	}

	job := <-app.queue
	if !strings.Contains(job.Text, "先记一下：这个需求要补充背景信息") {
		t.Fatalf("expected cached text merged into prompt, got: %q", job.Text)
	}
	if !strings.Contains(job.Text, "最近消息内容") {
		t.Fatalf("expected cached context section in prompt, got: %q", job.Text)
	}
	if !strings.Contains(job.Text, "说话者：") {
		t.Fatalf("expected speaker metadata in merged context, got: %q", job.Text)
	}

	app.mu.Lock()
	remaining := len(app.mediaWindow[windowKey])
	app.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("expected window consumed after merge, remaining=%d", remaining)
	}
}

func TestApp_OnMessageReceive_GroupContextWindowSpeakerResolvedByChatMemberAPI(t *testing.T) {
	cfg := configForTest()
	cfg.FeishuBotOpenID = "ou_bot"
	sender := &senderStub{
		chatMemberNameByIdentity: map[string]string{
			"chat_id:oc_chat|open_id:ou_user_1": "李志昊",
		},
	}
	app := NewApp(
		cfg,
		NewProcessor(codexStub{}, sender, "Codex 暂时不可用，请稍后重试。", "正在思考中..."),
	)

	textEvent := newGroupTextReceiveEvent(
		t,
		"evt_speaker_name_cache",
		"om_speaker_name_cache",
		"ou_user_1",
		"oc_chat",
		"缓存这条消息用于说话者名称解析",
		nil,
		"",
		"",
	)
	if err := app.onMessageReceive(context.Background(), textEvent); err != nil {
		t.Fatalf("unexpected text event error: %v", err)
	}
	if got := len(app.queue); got != 0 {
		t.Fatalf("expected queue len 0, got %d", got)
	}

	windowKey := buildMediaWindowKey("oc_chat", "open_id:ou_user_1")
	app.mu.Lock()
	cached := app.mediaWindow[windowKey]
	app.mu.Unlock()
	if len(cached) != 1 {
		t.Fatalf("expected 1 cached text entry, got %d", len(cached))
	}
	if !strings.Contains(cached[0].Speaker, "李志昊") {
		t.Fatalf("expected cached speaker name from chat member api, got %q", cached[0].Speaker)
	}
	if !strings.Contains(cached[0].Speaker, "ou_user_1") {
		t.Fatalf("expected cached speaker id retained, got %q", cached[0].Speaker)
	}

	sender.mu.Lock()
	resolveCalls := sender.resolveChatMemberNameCalls
	sender.mu.Unlock()
	if resolveCalls == 0 {
		t.Fatal("expected chat member name resolver to be called")
	}
}

func TestApp_OnMessageReceive_GroupThreadContextWindowIsolatedByThread(t *testing.T) {
	cfg := configForTest()
	cfg.FeishuBotOpenID = "ou_bot"
	app := NewApp(cfg, nil)
	base := time.Date(2026, 2, 23, 10, 0, 0, 0, time.UTC)
	now := base
	app.now = func() time.Time { return now }

	threadTextEvent := newGroupTextReceiveEvent(
		t,
		"evt_thread_text",
		"om_thread_text",
		"ou_user_1",
		"oc_chat",
		"thread-a 的历史上下文",
		nil,
		"thread_a",
		"root_a",
	)
	if err := app.onMessageReceive(context.Background(), threadTextEvent); err != nil {
		t.Fatalf("unexpected thread text event error: %v", err)
	}

	now = base.Add(30 * time.Second)
	mentionOtherThread := newGroupTextReceiveEvent(
		t,
		"evt_thread_b_mention",
		"om_thread_b_mention",
		"ou_user_1",
		"oc_chat",
		"<at user_id=\"ou_bot\">Alice</at> thread-b 触发",
		botMentionsForTest("ou_bot"),
		"thread_b",
		"root_b",
	)
	if err := app.onMessageReceive(context.Background(), mentionOtherThread); err != nil {
		t.Fatalf("unexpected thread-b mention event error: %v", err)
	}
	if got := len(app.queue); got != 1 {
		t.Fatalf("expected queue len 1, got %d", got)
	}
	threadBJob := <-app.queue
	if strings.Contains(threadBJob.Text, "thread-a 的历史上下文") {
		t.Fatalf("unexpected cross-thread context leakage: %q", threadBJob.Text)
	}

	now = base.Add(1 * time.Minute)
	mentionSameThread := newGroupTextReceiveEvent(
		t,
		"evt_thread_a_mention",
		"om_thread_a_mention",
		"ou_user_1",
		"oc_chat",
		"<at user_id=\"ou_bot\">Alice</at> thread-a 触发",
		botMentionsForTest("ou_bot"),
		"thread_a",
		"root_a",
	)
	if err := app.onMessageReceive(context.Background(), mentionSameThread); err != nil {
		t.Fatalf("unexpected thread-a mention event error: %v", err)
	}
	if got := len(app.queue); got != 1 {
		t.Fatalf("expected queue len 1, got %d", got)
	}
	threadAJob := <-app.queue
	if !strings.Contains(threadAJob.Text, "thread-a 的历史上下文") {
		t.Fatalf("expected same-thread cached context merged, got: %q", threadAJob.Text)
	}
}

func TestApp_OnMessageReceive_GroupContextWindowHonorsConfiguredTTL(t *testing.T) {
	cfg := configForTest()
	cfg.FeishuBotOpenID = "ou_bot"
	cfg.GroupContextWindowTTL = 1 * time.Minute
	app := NewApp(cfg, nil)
	base := time.Date(2026, 2, 23, 11, 0, 0, 0, time.UTC)
	now := base
	app.now = func() time.Time { return now }

	textEvent := newGroupTextReceiveEvent(
		t,
		"evt_short_ttl_text",
		"om_short_ttl_text",
		"ou_user_1",
		"oc_chat",
		"这条文本会过期",
		nil,
		"",
		"",
	)
	if err := app.onMessageReceive(context.Background(), textEvent); err != nil {
		t.Fatalf("unexpected text event error: %v", err)
	}

	now = base.Add(2 * time.Minute)
	mentionEvent := newGroupTextReceiveEvent(
		t,
		"evt_short_ttl_mention",
		"om_short_ttl_mention",
		"ou_user_1",
		"oc_chat",
		"<at user_id=\"ou_bot\">Alice</at> 只看当前消息",
		botMentionsForTest("ou_bot"),
		"",
		"",
	)
	if err := app.onMessageReceive(context.Background(), mentionEvent); err != nil {
		t.Fatalf("unexpected mention event error: %v", err)
	}
	if got := len(app.queue); got != 1 {
		t.Fatalf("expected queue len 1, got %d", got)
	}
	job := <-app.queue
	if job.Text != "只看当前消息" {
		t.Fatalf("expected expired cache not merged, got: %q", job.Text)
	}
}

func botMentionsForTest(botOpenID string) []*larkim.MentionEvent {
	return []*larkim.MentionEvent{
		{
			Id: &larkim.UserId{
				OpenId: strPtr(botOpenID),
			},
		},
	}
}

func newGroupTextReceiveEvent(
	t *testing.T,
	eventID string,
	messageID string,
	senderOpenID string,
	chatID string,
	text string,
	mentions []*larkim.MentionEvent,
	threadID string,
	rootID string,
) *larkim.P2MessageReceiveV1 {
	t.Helper()
	content, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		t.Fatalf("marshal text content failed: %v", err)
	}

	return &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: eventID}},
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{
					OpenId: strPtr(senderOpenID),
				},
			},
			Message: &larkim.EventMessage{
				MessageId:   strPtr(messageID),
				MessageType: strPtr("text"),
				Content:     strPtr(string(content)),
				ChatId:      strPtr(chatID),
				ChatType:    strPtr("group"),
				ThreadId:    strPtr(threadID),
				RootId:      strPtr(rootID),
				Mentions:    mentions,
			},
		},
	}
}
