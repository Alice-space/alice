package connector

import (
	"context"
	"testing"

	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/Alice-space/alice/internal/config"
)

func configForGroupScenesTest() config.Config {
	cfg := configForTest()
	cfg.LLMProvider = "codex"
	cfg.FeishuBotOpenID = "ou_bot"
	cfg.LLMProfiles = map[string]config.LLMProfileConfig{
		"chat": {
			Provider:        "codex",
			Model:           "gpt-5.4-mini",
			ReasoningEffort: "low",
			Personality:     "friendly",
		},
		"work": {
			Provider:        "codex",
			Model:           "gpt-5.4",
			ReasoningEffort: "xhigh",
			Personality:     "pragmatic",
		},
	}
	cfg.GroupScenes = config.GroupScenesConfig{
		Chat: config.GroupSceneConfig{
			Enabled:      true,
			SessionScope: config.GroupSceneSessionPerChat,
			LLMProfile:   "chat",
			NoReplyToken: "[[NO_REPLY]]",
		},
		Work: config.GroupSceneConfig{
			Enabled:            true,
			TriggerTag:         "#work",
			SessionScope:       config.GroupSceneSessionPerThread,
			LLMProfile:         "work",
			CreateFeishuThread: true,
		},
	}
	return cfg
}

func configForWorkOnlyGroupScenesTest() config.Config {
	cfg := configForGroupScenesTest()
	cfg.GroupScenes.Chat.Enabled = false
	return cfg
}

func TestApp_OnMessageReceive_GroupChatSceneSharesSessionAcrossMessages(t *testing.T) {
	cfg := configForGroupScenesTest()
	app := NewApp(cfg, nil)

	first := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_chat_1"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_chat_1"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"大家先随便聊聊"}`),
				ChatId:      strPtr("oc_chat"),
				ChatType:    strPtr("group"),
			},
		},
	}
	second := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_chat_2"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_chat_2"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"继续这个话题"}`),
				ChatId:      strPtr("oc_chat"),
				ChatType:    strPtr("group"),
			},
		},
	}

	if err := app.onMessageReceive(context.Background(), first); err != nil {
		t.Fatalf("unexpected first chat event error: %v", err)
	}
	if err := app.onMessageReceive(context.Background(), second); err != nil {
		t.Fatalf("unexpected second chat event error: %v", err)
	}

	if got := len(app.queue); got != 2 {
		t.Fatalf("expected queue len 2, got %d", got)
	}
	job1 := <-app.queue
	job2 := <-app.queue
	for idx, job := range []Job{job1, job2} {
		if job.Scene != jobSceneChat {
			t.Fatalf("job %d unexpected scene: %q", idx+1, job.Scene)
		}
		if job.ResponseMode != jobResponseModeReply {
			t.Fatalf("job %d unexpected response mode: %q", idx+1, job.ResponseMode)
		}
		if !job.DisableAck {
			t.Fatalf("job %d should disable ack", idx+1)
		}
		if job.SessionKey != "chat_id:oc_chat|scene:chat" {
			t.Fatalf("job %d unexpected session key: %q", idx+1, job.SessionKey)
		}
		if job.ResourceScopeKey != "chat_id:oc_chat|scene:chat" {
			t.Fatalf("job %d unexpected resource scope key: %q", idx+1, job.ResourceScopeKey)
		}
		if job.LLMModel != "gpt-5.4-mini" || job.LLMReasoningEffort != "low" || job.LLMPersonality != "friendly" {
			t.Fatalf("job %d unexpected llm profile: model=%q reasoning=%q personality=%q", idx+1, job.LLMModel, job.LLMReasoningEffort, job.LLMPersonality)
		}
		if job.NoReplyToken != "[[NO_REPLY]]" {
			t.Fatalf("job %d unexpected no-reply token: %q", idx+1, job.NoReplyToken)
		}
		if job.CreateFeishuThread {
			t.Fatalf("job %d chat scene should reply directly instead of creating thread", idx+1)
		}
	}
	if job1.SessionVersion != 1 {
		t.Fatalf("unexpected first session version: %d", job1.SessionVersion)
	}
	if job2.SessionVersion != 2 {
		t.Fatalf("unexpected second session version: %d", job2.SessionVersion)
	}
}

func TestApp_OnMessageReceive_WorkSceneUsesDedicatedThreadSession(t *testing.T) {
	cfg := configForGroupScenesTest()
	processor := NewProcessor(codexStub{resp: "ok"}, nil, "", "")
	app := NewApp(cfg, processor)

	start := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_work_start"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_work_root"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"<at user_id=\"ou_bot\">Alice</at> #work 帮我排查一下"}`),
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
	followup := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_work_followup"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_work_followup"),
				ThreadId:    strPtr("omt_work_1"),
				RootId:      strPtr("om_work_root"),
				ParentId:    strPtr("om_work_root"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"<at user_id=\"ou_bot\">Alice</at> 继续看看日志"}`),
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
	if err := app.onMessageReceive(context.Background(), followup); err != nil {
		t.Fatalf("unexpected work followup error: %v", err)
	}

	if got := len(app.queue); got != 2 {
		t.Fatalf("expected queue len 2, got %d", got)
	}
	job1 := <-app.queue
	job2 := <-app.queue

	if job1.Scene != jobSceneWork || job2.Scene != jobSceneWork {
		t.Fatalf("unexpected work scenes: %q %q", job1.Scene, job2.Scene)
	}
	if job1.ResponseMode != jobResponseModeReply || job2.ResponseMode != jobResponseModeReply {
		t.Fatalf("unexpected work response modes: %q %q", job1.ResponseMode, job2.ResponseMode)
	}
	if job1.DisableAck || job2.DisableAck {
		t.Fatalf("work scene should keep immediate ack enabled")
	}
	if !job1.CreateFeishuThread || !job2.CreateFeishuThread {
		t.Fatalf("work scene should keep create_feishu_thread enabled")
	}
	if job1.SessionKey != "chat_id:oc_chat|scene:work|seed:om_work_root" {
		t.Fatalf("unexpected work start session key: %q", job1.SessionKey)
	}
	if job2.SessionKey != job1.SessionKey {
		t.Fatalf("work followup should reuse session key, got %q want %q", job2.SessionKey, job1.SessionKey)
	}
	if job1.ResourceScopeKey != "chat_id:oc_chat|scene:work|thread:om_work_root" {
		t.Fatalf("unexpected work start resource scope key: %q", job1.ResourceScopeKey)
	}
	if job2.ResourceScopeKey != job1.ResourceScopeKey {
		t.Fatalf("work followup should reuse resource scope key, got %q want %q", job2.ResourceScopeKey, job1.ResourceScopeKey)
	}
	if job1.Text != "帮我排查一下" {
		t.Fatalf("unexpected work start text: %q", job1.Text)
	}
	if job2.Text != "继续看看日志" {
		t.Fatalf("unexpected work followup text: %q", job2.Text)
	}
	if job1.LLMModel != "gpt-5.4" || job1.LLMReasoningEffort != "xhigh" || job1.LLMPersonality != "pragmatic" {
		t.Fatalf("unexpected work llm profile: model=%q reasoning=%q personality=%q", job1.LLMModel, job1.LLMReasoningEffort, job1.LLMPersonality)
	}
	if job2.SessionVersion != 2 {
		t.Fatalf("unexpected followup session version: %d", job2.SessionVersion)
	}
}

func TestApp_OnMessageReceive_WorkOnlySceneIgnoresMentionWithoutTriggerTag(t *testing.T) {
	cfg := configForWorkOnlyGroupScenesTest()
	app := NewApp(cfg, nil)

	event := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_work_only_without_tag"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_work_only_without_tag"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"<at user_id=\"ou_bot\">Alice</at> 只艾特，不带标签"}`),
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

	if err := app.onMessageReceive(context.Background(), event); err != nil {
		t.Fatalf("unexpected work-only event error: %v", err)
	}
	if got := len(app.queue); got != 0 {
		t.Fatalf("expected queue len 0, got %d", got)
	}
}

func TestApp_OnMessageReceive_WorkSceneThreadFollowupRequiresMention(t *testing.T) {
	cfg := configForGroupScenesTest()
	processor := NewProcessor(codexStub{resp: "ok"}, nil, "", "")
	app := NewApp(cfg, processor)

	start := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_work_followup_requires_mention_start"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_work_followup_requires_mention_root"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"<at user_id=\"ou_bot\">Alice</at> #work 帮我继续这个任务"}`),
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
	followup := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_work_followup_requires_mention_followup"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_work_followup_requires_mention_followup"),
				ThreadId:    strPtr("omt_work_requires_mention"),
				RootId:      strPtr("om_work_followup_requires_mention_root"),
				ParentId:    strPtr("om_work_followup_requires_mention_root"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"这里继续，但不再艾特"}`),
				ChatId:      strPtr("oc_chat"),
				ChatType:    strPtr("group"),
			},
		},
	}

	if err := app.onMessageReceive(context.Background(), start); err != nil {
		t.Fatalf("unexpected work start error: %v", err)
	}
	if err := app.onMessageReceive(context.Background(), followup); err != nil {
		t.Fatalf("unexpected work followup error: %v", err)
	}

	if got := len(app.queue); got != 1 {
		t.Fatalf("expected queue len 1, got %d", got)
	}
	job := <-app.queue
	if job.Scene != jobSceneWork {
		t.Fatalf("unexpected work start scene: %q", job.Scene)
	}
	if job.SessionKey != "chat_id:oc_chat|scene:work|seed:om_work_followup_requires_mention_root" {
		t.Fatalf("unexpected work start session key: %q", job.SessionKey)
	}
}

func TestApp_OnMessageReceive_GroupChatSceneUsesRotatedSessionAfterClear(t *testing.T) {
	cfg := configForGroupScenesTest()
	processor := NewProcessor(codexStub{resp: "ok"}, nil, "", "")
	app := NewApp(cfg, processor)

	baseSessionKey := buildChatSceneSessionKey("chat_id", "oc_chat")
	oldThreadID := "thread_old"
	processor.setThreadID(baseSessionKey, oldThreadID)
	_, rotatedSessionKey := processor.resetChatSceneSession("chat_id", "oc_chat")
	if rotatedSessionKey == "" || rotatedSessionKey == baseSessionKey {
		t.Fatalf("expected rotated chat session key, got %q", rotatedSessionKey)
	}

	event := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_chat_after_clear"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_chat_after_clear"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"继续聊新的上下文"}`),
				ChatId:      strPtr("oc_chat"),
				ChatType:    strPtr("group"),
			},
		},
	}

	if err := app.onMessageReceive(context.Background(), event); err != nil {
		t.Fatalf("unexpected chat event error: %v", err)
	}
	if got := len(app.queue); got != 1 {
		t.Fatalf("expected queue len 1, got %d", got)
	}
	job := <-app.queue
	if job.Scene != jobSceneChat {
		t.Fatalf("unexpected chat scene: %q", job.Scene)
	}
	if job.SessionKey != rotatedSessionKey {
		t.Fatalf("expected rotated session key %q, got %q", rotatedSessionKey, job.SessionKey)
	}
	if job.ResourceScopeKey != rotatedSessionKey {
		t.Fatalf("expected rotated resource scope key %q, got %q", rotatedSessionKey, job.ResourceScopeKey)
	}
	if threadID := processor.getThreadID(job.SessionKey); threadID != "" {
		t.Fatalf("expected cleared chat session to have no thread id, got %q", threadID)
	}
}

func TestApp_OnMessageReceive_WorkSceneParentOnlyFollowupReusesSessionWithMention(t *testing.T) {
	cfg := configForGroupScenesTest()
	processor := NewProcessor(codexStub{resp: "ok"}, nil, "", "")
	app := NewApp(cfg, processor)

	start := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_work_parent_only_with_mention_start"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_work_parent_only_with_mention_root"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"<at user_id=\"ou_bot\">Alice</at> #work 帮我继续这个任务"}`),
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
	followup := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_work_parent_only_with_mention_followup"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_work_parent_only_with_mention_followup"),
				ParentId:    strPtr("om_work_parent_only_with_mention_root"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"<at user_id=\"ou_bot\">Alice</at> 这里继续，不再重复 #work"}`),
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
	if err := app.onMessageReceive(context.Background(), followup); err != nil {
		t.Fatalf("unexpected work followup error: %v", err)
	}

	if got := len(app.queue); got != 2 {
		t.Fatalf("expected queue len 2, got %d", got)
	}
	job1 := <-app.queue
	job2 := <-app.queue

	if job1.Scene != jobSceneWork || job2.Scene != jobSceneWork {
		t.Fatalf("unexpected work scenes: %q %q", job1.Scene, job2.Scene)
	}
	if job2.SessionKey != job1.SessionKey {
		t.Fatalf("parent-only followup with mention should reuse session key, got %q want %q", job2.SessionKey, job1.SessionKey)
	}
	if job2.ResourceScopeKey != job1.ResourceScopeKey {
		t.Fatalf("parent-only followup with mention should reuse resource scope key, got %q want %q", job2.ResourceScopeKey, job1.ResourceScopeKey)
	}
	if job2.Text != "这里继续，不再重复 #work" {
		t.Fatalf("unexpected work followup text: %q", job2.Text)
	}
	if job2.SessionVersion != 2 {
		t.Fatalf("unexpected followup session version: %d", job2.SessionVersion)
	}
}
