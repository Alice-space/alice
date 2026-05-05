package connector

import (
	"context"
	"strings"
	"testing"

	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/Alice-space/alice/internal/config"
)

func configForGroupScenesTest() config.Config {
	cfg := configForTest()
	cfg.LLMProvider = "codex"
	cfg.LLMProfiles = map[string]config.LLMProfileConfig{
		"chat": {
			Provider:        "codex",
			Model:           "gpt-5.4-mini",
			Profile:         "chat-cli",
			ReasoningEffort: "low",
			Personality:     "friendly",
		},
		"work": {
			Provider:        "codex",
			Model:           "gpt-5.4",
			Profile:         "work-cli",
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

func newGroupScenesApp(cfg config.Config, processor *Processor) *App {
	app := NewApp(cfg, processor)
	app.SetBotOpenID("ou_bot")
	return app
}

func TestStripBotMentionText_PreservesOtherMentions(t *testing.T) {
	got := stripBotMentionText("@Codex @Carlo 请让 @Codex 看这里", []*larkim.MentionEvent{
		{
			Key:  strPtr("@_bot"),
			Name: strPtr("Codex"),
			Id:   &larkim.UserId{OpenId: strPtr("ou_bot")},
		},
		{
			Key:  strPtr("@_carlo"),
			Name: strPtr("Carlo"),
			Id:   &larkim.UserId{OpenId: strPtr("ou_carlo")},
		},
	}, "ou_bot", "")
	if got != "@Carlo 请让 看这里" {
		t.Fatalf("unexpected normalized text: %q", got)
	}
}

func TestApp_WorkSessionCommandStripsLeadingBotMentionKey(t *testing.T) {
	cfg := configForGroupScenesTest()
	llmStub := &llmCallCountingStub{}
	sender := &senderStub{}
	processor := NewProcessor(llmStub, sender, "failed", "thinking")
	app := newGroupScenesApp(cfg, processor)

	sessionID := "019de8de-c0db-70c0-b52e-938ed0d21ce1"
	event := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_work_session_bootstrap"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_work_session_bootstrap"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"@_bot #work /session 019de8de-c0db-70c0-b52e-938ed0d21ce1"}`),
				ChatId:      strPtr("oc_chat"),
				ChatType:    strPtr("group"),
				Mentions: []*larkim.MentionEvent{
					{
						Key:  strPtr("@_bot"),
						Name: strPtr("Codex"),
						Id:   &larkim.UserId{OpenId: strPtr("ou_bot")},
					},
				},
			},
		},
	}

	job, err := BuildJob(event)
	if err != nil {
		t.Fatalf("build job failed: %v", err)
	}
	if !app.routeIncomingJob(job, event) {
		t.Fatal("expected work session command to be routed")
	}
	if job.Text != "/session "+sessionID {
		t.Fatalf("expected leading bot mention and work tag stripped, got %q", job.Text)
	}
	if job.Scene != jobSceneWork {
		t.Fatalf("unexpected scene: %q", job.Scene)
	}

	state := processor.ProcessJobState(context.Background(), *job)
	if state != JobProcessCompleted {
		t.Fatalf("expected completed state, got %s", state)
	}
	if llmStub.calls != 0 {
		t.Fatalf("expected /session binding to bypass llm, got %d calls", llmStub.calls)
	}
	if got := processor.getThreadID(job.SessionKey); got != sessionID {
		t.Fatalf("unexpected bound backend session id: %q", got)
	}
	if len(sender.replyCards) != 1 || !strings.Contains(sender.replyCards[0], "已绑定后端 session。") {
		t.Fatalf("unexpected session binding reply: %#v", sender.replyCards)
	}
}

func TestApp_OnMessageReceive_GroupChatSceneSharesSessionAcrossMessages(t *testing.T) {
	cfg := configForGroupScenesTest()
	app := newGroupScenesApp(cfg, nil)

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
		if job.SessionKey != "chat_id:oc_chat" {
			t.Fatalf("job %d unexpected session key: %q", idx+1, job.SessionKey)
		}
		if job.ResourceScopeKey != "chat_id:oc_chat" {
			t.Fatalf("job %d unexpected resource scope key: %q", idx+1, job.ResourceScopeKey)
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

func TestApp_RouteBuiltinStopToExistingWorkSession(t *testing.T) {
	cfg := configForGroupScenesTest()
	app := newGroupScenesApp(cfg, nil)

	sessionKey := buildWorkSessionKey("chat_id", "oc_chat", "om_work_root")
	app.state.latest[sessionKey] = 1

	event := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_work_stop"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_work_stop"),
				ParentId:    strPtr("om_work_root"),
				RootId:      strPtr("om_work_root"),
				ThreadId:    strPtr("omt_work"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"/stop"}`),
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

	job, err := BuildJob(event)
	if err != nil {
		t.Fatalf("build job failed: %v", err)
	}
	if !app.routeIncomingJob(job, event) {
		t.Fatal("expected builtin stop to be routed")
	}
	if job.Scene != jobSceneWork {
		t.Fatalf("unexpected scene: %q", job.Scene)
	}
	if job.SessionKey != sessionKey {
		t.Fatalf("unexpected session key: %q", job.SessionKey)
	}
	if job.ResourceScopeKey != buildWorkSessionResourceScopeKey(sessionKey) {
		t.Fatalf("unexpected resource scope key: %q", job.ResourceScopeKey)
	}
}

func TestApp_RouteBuiltinSessionToExistingWorkSessionWithoutMention(t *testing.T) {
	cfg := configForGroupScenesTest()
	app := newGroupScenesApp(cfg, nil)

	sessionKey := buildWorkSessionKey("chat_id", "oc_chat", "om_work_root")
	app.state.latest[sessionKey] = 1

	event := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_work_session"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_work_session"),
				ParentId:    strPtr("om_work_root"),
				RootId:      strPtr("om_work_root"),
				ThreadId:    strPtr("omt_work"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"/session sess_123"}`),
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

	job, err := BuildJob(event)
	if err != nil {
		t.Fatalf("build job failed: %v", err)
	}
	if !app.routeIncomingJob(job, event) {
		t.Fatal("expected builtin session to be routed")
	}
	if job.Scene != jobSceneWork {
		t.Fatalf("unexpected scene: %q", job.Scene)
	}
	if job.SessionKey != sessionKey {
		t.Fatalf("unexpected session key: %q", job.SessionKey)
	}
	if !job.CreateFeishuThread {
		t.Fatal("session command in work scene should keep thread replies enabled")
	}
}

func TestApp_RouteStatusToExistingWorkSessionWithoutMention(t *testing.T) {
	cfg := configForGroupScenesTest()
	app := newGroupScenesApp(cfg, nil)

	sessionKey := buildWorkSessionKey("chat_id", "oc_chat", "om_work_root")
	app.state.latest[sessionKey] = 1

	event := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_work_status"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_work_status"),
				ParentId:    strPtr("om_work_root"),
				RootId:      strPtr("om_work_root"),
				ThreadId:    strPtr("omt_work"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"/status"}`),
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

	job, err := BuildJob(event)
	if err != nil {
		t.Fatalf("build job failed: %v", err)
	}
	if !app.routeIncomingJob(job, event) {
		t.Fatal("expected builtin status to be routed")
	}
	if job.Scene != jobSceneWork {
		t.Fatalf("unexpected scene: %q", job.Scene)
	}
	if job.SessionKey != sessionKey {
		t.Fatalf("unexpected session key: %q", job.SessionKey)
	}
	if !job.CreateFeishuThread {
		t.Fatal("status command in work scene should keep thread replies enabled")
	}
}

func TestApp_RouteStatusSkipsWhenInThreadWithoutMatchingWorkSession(t *testing.T) {
	cfg := configForGroupScenesTest()
	app := newGroupScenesApp(cfg, nil)

	threadEvent := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_thread_status"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_thread_status"),
				ParentId:    strPtr("om_unrelated"),
				RootId:      strPtr("om_unrelated"),
				ThreadId:    strPtr("omt_unrelated"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"/status"}`),
				ChatId:      strPtr("oc_chat"),
				ChatType:    strPtr("group"),
			},
		},
	}

	job, err := BuildJob(threadEvent)
	if err != nil {
		t.Fatalf("build job failed: %v", err)
	}
	if app.routeIncomingJob(job, threadEvent) {
		t.Fatal("expected /status in unrelated thread to NOT be routed")
	}

	mainChatEvent := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_main_status"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_main_status"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"/status"}`),
				ChatId:      strPtr("oc_chat"),
				ChatType:    strPtr("group"),
			},
		},
	}

	job2, err := BuildJob(mainChatEvent)
	if err != nil {
		t.Fatalf("build job failed: %v", err)
	}
	if !app.routeIncomingJob(job2, mainChatEvent) {
		t.Fatal("expected /status in main group chat to be routed")
	}
}

func TestApp_OnMessageReceive_WorkSceneUsesDedicatedThreadSession(t *testing.T) {
	cfg := configForGroupScenesTest()
	processor := NewProcessor(codexStub{resp: "ok"}, nil, "", "")
	app := newGroupScenesApp(cfg, processor)

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
	if job1.SessionKey != "chat_id:oc_chat|work:om_work_root" {
		t.Fatalf("unexpected work start session key: %q", job1.SessionKey)
	}
	if job2.SessionKey != job1.SessionKey {
		t.Fatalf("work followup should reuse session key, got %q want %q", job2.SessionKey, job1.SessionKey)
	}
	if job1.ResourceScopeKey != "chat_id:oc_chat|thread:om_work_root" {
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
	if job1.LLMProvider != "codex" || job2.LLMProvider != "codex" {
		t.Fatalf("unexpected work llm providers: %q %q", job1.LLMProvider, job2.LLMProvider)
	}
	if job1.LLMProfile != "work" || job2.LLMProfile != "work" {
		t.Fatalf("unexpected work llm profile selectors: %q %q", job1.LLMProfile, job2.LLMProfile)
	}
	if job2.SessionVersion != 2 {
		t.Fatalf("unexpected followup session version: %d", job2.SessionVersion)
	}
}

func TestApp_OnMessageReceive_EmptyWorkSceneQueuesBootstrapJob(t *testing.T) {
	cfg := configForGroupScenesTest()
	processor := NewProcessor(codexStub{resp: "ok"}, nil, "", "")
	app := newGroupScenesApp(cfg, processor)

	start := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_work_empty_start"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_work_empty_root"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"<at user_id=\"ou_bot\">Alice</at> #work"}`),
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
	job := <-app.queue
	if job.Scene != jobSceneWork {
		t.Fatalf("unexpected scene: %q", job.Scene)
	}
	if job.SessionKey != "chat_id:oc_chat|work:om_work_empty_root" {
		t.Fatalf("unexpected session key: %q", job.SessionKey)
	}
	if job.Text != "" {
		t.Fatalf("expected empty work text after trigger trim, got %q", job.Text)
	}
	if !job.CreateFeishuThread {
		t.Fatal("empty work bootstrap should keep thread replies enabled")
	}
}

func TestApp_OnMessageReceive_GroupScenesUseDifferentProvidersPerScene(t *testing.T) {
	cfg := configForGroupScenesTest()
	cfg.LLMProvider = config.DefaultLLMProvider
	cfg.LLMProfiles["chat"] = config.LLMProfileConfig{
		Provider:        "codex",
		Model:           "gpt-5.4-mini",
		ReasoningEffort: "low",
		Personality:     "friendly",
	}
	cfg.LLMProfiles["work"] = config.LLMProfileConfig{
		Provider:        "claude",
		Model:           "claude-sonnet-4-20250514",
		ReasoningEffort: "high",
		Personality:     "pragmatic",
	}

	processor := NewProcessor(codexStub{resp: "ok"}, nil, "", "")
	app := newGroupScenesApp(cfg, processor)

	chatEvent := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_chat_provider"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_chat_provider"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"大家先随便聊聊"}`),
				ChatId:      strPtr("oc_chat"),
				ChatType:    strPtr("group"),
			},
		},
	}
	workEvent := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_work_provider"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_work_provider"),
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

	if err := app.onMessageReceive(context.Background(), chatEvent); err != nil {
		t.Fatalf("unexpected chat event error: %v", err)
	}
	if err := app.onMessageReceive(context.Background(), workEvent); err != nil {
		t.Fatalf("unexpected work event error: %v", err)
	}

	<-app.queue // drain chat job
	workJob := <-app.queue
	if workJob.LLMProvider != "claude" {
		t.Fatalf("unexpected work provider: %q", workJob.LLMProvider)
	}
}

func TestApp_OnMessageReceive_WorkOnlySceneIgnoresMentionWithoutTriggerTag(t *testing.T) {
	cfg := configForWorkOnlyGroupScenesTest()
	app := newGroupScenesApp(cfg, nil)

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
		t.Fatalf("expected queue len 0 (message without #work tag in work-only mode), got %d", got)
	}
}

func TestApp_OnMessageReceive_WorkSceneThreadFollowupRequiresMention(t *testing.T) {
	cfg := configForGroupScenesTest()
	processor := NewProcessor(codexStub{resp: "ok"}, nil, "", "")
	app := newGroupScenesApp(cfg, processor)

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
	if job.SessionKey != "chat_id:oc_chat|work:om_work_followup_requires_mention_root" {
		t.Fatalf("unexpected work start session key: %q", job.SessionKey)
	}
}

func TestApp_OnMessageReceive_WorkSceneThreadFileFollowupWithoutMention(t *testing.T) {
	cfg := configForGroupScenesTest()
	processor := NewProcessor(codexStub{resp: "ok"}, nil, "", "")
	app := newGroupScenesApp(cfg, processor)

	start := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_work_file_followup_start"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_work_file_followup_root"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"<at user_id=\"ou_bot\">Alice</at> #work 请看我接下来上传的文件"}`),
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
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_work_file_followup_file"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_work_file_followup_file"),
				ThreadId:    strPtr("omt_work_file_followup"),
				RootId:      strPtr("om_work_file_followup_root"),
				ParentId:    strPtr("om_work_file_followup_root"),
				MessageType: strPtr("file"),
				Content:     strPtr(`{"file_key":"file_doc_123","file_name":"四（四）（五）.docx"}`),
				ChatId:      strPtr("oc_chat"),
				ChatType:    strPtr("group"),
			},
		},
	}

	if err := app.onMessageReceive(context.Background(), start); err != nil {
		t.Fatalf("unexpected work start error: %v", err)
	}
	if err := app.onMessageReceive(context.Background(), followup); err != nil {
		t.Fatalf("unexpected work file followup error: %v", err)
	}

	if got := len(app.queue); got != 2 {
		t.Fatalf("expected queue len 2, got %d", got)
	}
	job1 := <-app.queue
	job2 := <-app.queue

	if job2.Scene != jobSceneWork {
		t.Fatalf("unexpected work file followup scene: %q", job2.Scene)
	}
	if job2.SessionKey != job1.SessionKey {
		t.Fatalf("file followup should reuse session key, got %q want %q", job2.SessionKey, job1.SessionKey)
	}
	if job2.ResourceScopeKey != job1.ResourceScopeKey {
		t.Fatalf("file followup should reuse resource scope key, got %q want %q", job2.ResourceScopeKey, job1.ResourceScopeKey)
	}
	if job2.MessageType != "file" {
		t.Fatalf("unexpected file followup message type: %q", job2.MessageType)
	}
	if job2.Text != "用户发送了一个文件：四（四）（五）.docx" {
		t.Fatalf("unexpected file followup text: %q", job2.Text)
	}
	if job2.SessionVersion != 2 {
		t.Fatalf("unexpected file followup session version: %d", job2.SessionVersion)
	}
	if len(job2.Attachments) != 1 {
		t.Fatalf("expected one attachment, got %d", len(job2.Attachments))
	}
	if job2.Attachments[0].FileKey != "file_doc_123" {
		t.Fatalf("unexpected file attachment key: %q", job2.Attachments[0].FileKey)
	}
	if job2.Attachments[0].FileName != "四（四）（五）.docx" {
		t.Fatalf("unexpected file attachment name: %q", job2.Attachments[0].FileName)
	}
}

func TestApp_OnMessageReceive_GroupChatSceneUsesRotatedSessionAfterClear(t *testing.T) {
	cfg := configForGroupScenesTest()
	processor := NewProcessor(codexStub{resp: "ok"}, nil, "", "")
	app := newGroupScenesApp(cfg, processor)

	baseSessionKey := restoreChatSceneKey("chat_id", "oc_chat")
	oldThreadID := "thread_old"
	processor.setThreadID(baseSessionKey, oldThreadID)
	_, currentKey := processor.resetChatSceneSession("chat_id", "oc_chat")
	if currentKey != baseSessionKey {
		t.Fatalf("expected chat session key to stay the same after clear, got %q", currentKey)
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
	if job.SessionKey != baseSessionKey {
		t.Fatalf("expected session key %q, got %q", baseSessionKey, job.SessionKey)
	}
	if job.ResourceScopeKey != baseSessionKey {
		t.Fatalf("expected resource scope key %q, got %q", baseSessionKey, job.ResourceScopeKey)
	}
	if threadID := processor.getThreadID(job.SessionKey); threadID != "" {
		t.Fatalf("expected cleared chat session to have no thread id, got %q", threadID)
	}
}

func TestApp_OnMessageReceive_WorkSceneParentOnlyFollowupReusesSessionWithMention(t *testing.T) {
	cfg := configForGroupScenesTest()
	processor := NewProcessor(codexStub{resp: "ok"}, nil, "", "")
	app := newGroupScenesApp(cfg, processor)

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

func TestApp_OnMessageReceive_WorkTagOnlyRoutedToMentionedBot(t *testing.T) {
	cfg := configForWorkOnlyGroupScenesTest()
	app1 := newGroupScenesApp(cfg, nil)
	app1.SetBotOpenID("ou_bot1")
	app2 := newGroupScenesApp(cfg, nil)
	app2.SetBotOpenID("ou_bot2")

	event := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_work_tag_multi_bot"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_work_tag_multi"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"<at user_id=\"ou_bot1\">Bot1</at> #work 处理这个任务"}`),
				ChatId:      strPtr("oc_chat"),
				ChatType:    strPtr("group"),
				Mentions: []*larkim.MentionEvent{
					{Id: &larkim.UserId{OpenId: strPtr("ou_bot1")}},
				},
			},
		},
	}

	job1, err := BuildJob(event)
	if err != nil {
		t.Fatalf("build job failed: %v", err)
	}
	if !app1.routeIncomingJob(job1, event) {
		t.Fatal("expected bot1 to route work-tagged message")
	}
	if job1.Scene != jobSceneWork {
		t.Fatalf("expected work scene for bot1, got %q", job1.Scene)
	}

	job2, err := BuildJob(event)
	if err != nil {
		t.Fatalf("build job failed: %v", err)
	}
	if app2.routeIncomingJob(job2, event) {
		t.Fatal("expected bot2 to NOT route work-tagged message targeting bot1")
	}
}

func TestApp_OnMessageReceive_SlashCommandInThreadOnlyRoutedToSessionOwner(t *testing.T) {
	cfg := configForGroupScenesTest()
	app1 := newGroupScenesApp(cfg, nil)
	app1.SetBotOpenID("ou_bot1")
	sessionKey := buildWorkSessionKey("chat_id", "oc_chat", "om_work_root")
	app1.state.latest[sessionKey] = 1

	app2 := newGroupScenesApp(cfg, nil)
	app2.SetBotOpenID("ou_bot2")

	slashInThread := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_slash_in_thread"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_slash_thread"),
				ThreadId:    strPtr("omt_work"),
				RootId:      strPtr("om_work_root"),
				ParentId:    strPtr("om_work_root"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"/pwd"}`),
				ChatId:      strPtr("oc_chat"),
				ChatType:    strPtr("group"),
			},
		},
	}

	job1, err := BuildJob(slashInThread)
	if err != nil {
		t.Fatalf("build job failed: %v", err)
	}
	if !app1.routeIncomingJob(job1, slashInThread) {
		t.Fatal("expected bot1 (session owner) to route /pwd in its work thread")
	}
	if job1.Scene != jobSceneWork {
		t.Fatalf("expected work scene for bot1, got %q", job1.Scene)
	}

	job2, err := BuildJob(slashInThread)
	if err != nil {
		t.Fatalf("build job failed: %v", err)
	}
	if app2.routeIncomingJob(job2, slashInThread) {
		t.Fatal("expected bot2 (non-owner) to NOT route /pwd in bot1's work thread")
	}
}

func TestApp_OnMessageReceive_SlashCommandInMainChatRoutedForAll(t *testing.T) {
	cfg := configForGroupScenesTest()
	app1 := newGroupScenesApp(cfg, nil)
	app1.SetBotOpenID("ou_bot1")
	app2 := newGroupScenesApp(cfg, nil)
	app2.SetBotOpenID("ou_bot2")

	slashInMain := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_slash_main"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_slash_main"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"/help"}`),
				ChatId:      strPtr("oc_chat"),
				ChatType:    strPtr("group"),
			},
		},
	}

	job1, err := BuildJob(slashInMain)
	if err != nil {
		t.Fatalf("build job failed: %v", err)
	}
	if !app1.routeIncomingJob(job1, slashInMain) {
		t.Fatal("expected bot1 to route /help in main chat")
	}

	job2, err := BuildJob(slashInMain)
	if err != nil {
		t.Fatalf("build job failed: %v", err)
	}
	if !app2.routeIncomingJob(job2, slashInMain) {
		t.Fatal("expected bot2 to route /help in main chat")
	}
}

func TestApp_OnMessageReceive_SlashCommandsWorkInWorkOnlyMainChat(t *testing.T) {
	cfg := configForWorkOnlyGroupScenesTest()
	app := newGroupScenesApp(cfg, nil)
	app.SetBotOpenID("ou_bot")

	commands := []string{"/help", "/status", "/cd"}
	for _, cmd := range commands {
		event := &larkim.P2MessageReceiveV1{
			EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_" + cmd[1:]}},
			Event: &larkim.P2MessageReceiveV1Data{
				Message: &larkim.EventMessage{
					MessageId:   strPtr("om_" + cmd[1:]),
					MessageType: strPtr("text"),
					Content:     strPtr(`{"text":"` + cmd + `"}`),
					ChatId:      strPtr("oc_chat"),
					ChatType:    strPtr("group"),
				},
			},
		}
		job, err := BuildJob(event)
		if err != nil {
			t.Fatalf("build job for %s failed: %v", cmd, err)
		}
		if !app.routeIncomingJob(job, event) {
			t.Fatalf("expected %s to be routed in work-only main chat", cmd)
		}
	}
}

func TestApp_OnMessageReceive_SlashCommandInWorkSession(t *testing.T) {
	cfg := configForGroupScenesTest()
	processor := NewProcessor(codexStub{resp: "ok"}, &senderStub{}, "", "")
	app := newGroupScenesApp(cfg, processor)
	app.SetBotOpenID("ou_bot")

	sessionKey := buildWorkSessionKey("chat_id", "oc_chat", "om_work_seed")
	processor.setThreadID(sessionKey, "thread_backend")
	processor.setWorkThreadID(sessionKey, "omt_work_feishu")

	slashEvent := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_work_slash"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_work_slash"),
				ThreadId:    strPtr("omt_work_feishu"),
				RootId:      strPtr("om_work_seed"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"/pwd"}`),
				ChatId:      strPtr("oc_chat"),
				ChatType:    strPtr("group"),
			},
		},
	}

	job, err := BuildJob(slashEvent)
	if err != nil {
		t.Fatalf("build job failed: %v", err)
	}
	if !app.routeIncomingJob(job, slashEvent) {
		t.Fatal("expected /pwd in work session to be routed")
	}
	if job.Scene != jobSceneWork {
		t.Fatalf("expected work scene, got %q", job.Scene)
	}
}

func TestApp_OnMessageReceive_WorkSessionFollowupViaReplyMessageBinding(t *testing.T) {
	cfg := configForGroupScenesTest()
	processor := NewProcessor(codexStub{resp: "ok"}, &senderStub{}, "", "")
	app := newGroupScenesApp(cfg, processor)
	app.SetBotOpenID("ou_bot")

	sessionKey := buildWorkSessionKey("chat_id", "oc_chat", "om_seed_msg")
	processor.setThreadID(sessionKey, "thread_backend")
	processor.bindReplyMessage(sessionKey, "om_reply_card")

	followupViaBotReply := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_work_followup_via_reply"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_followup"),
				ThreadId:    strPtr("omt_thread_xyz"),
				RootId:      strPtr("om_reply_card"),
				ParentId:    strPtr("om_reply_card"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"<at user_id=\"ou_bot\">Alice</at> /pwd"}`),
				ChatId:      strPtr("oc_chat"),
				ChatType:    strPtr("group"),
				Mentions: []*larkim.MentionEvent{
					{Id: &larkim.UserId{OpenId: strPtr("ou_bot")}},
				},
			},
		},
	}

	job, err := BuildJob(followupViaBotReply)
	if err != nil {
		t.Fatalf("build job failed: %v", err)
	}
	if !app.routeIncomingJob(job, followupViaBotReply) {
		t.Fatal("expected followup via bot reply card to be routed to work session")
	}
	if job.Scene != jobSceneWork {
		t.Fatalf("expected work scene for followup via bot reply message binding, got %q", job.Scene)
	}
	if job.SessionKey != sessionKey {
		t.Fatalf("expected session key %q, got %q", sessionKey, job.SessionKey)
	}
}
