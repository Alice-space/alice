package connector

import (
	"testing"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestShouldProcessIncomingMessage_GroupRequiresMention(t *testing.T) {
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				ChatType:    strPtr("group"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"大家好"}`),
				ChatId:      strPtr("oc_chat"),
			},
		},
	}

	if shouldProcessIncomingMessage(event, "at", "", "", "") {
		t.Fatal("group message without mention should be ignored")
	}
}

func TestShouldProcessIncomingMessage_GroupMentionWithBotOpenID(t *testing.T) {
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				ChatType: strPtr("group"),
				Content:  strPtr(`{"text":"<at user_id=\"ou_bot\">Alice</at> 你好"}`),
				Mentions: []*larkim.MentionEvent{
					{
						Id: &larkim.UserId{
							OpenId: strPtr("ou_bot"),
						},
					},
				},
			},
		},
	}

	if !shouldProcessIncomingMessage(event, "at", "", "ou_bot", "") {
		t.Fatal("group message that mentions bot open_id should be processed")
	}
}

func TestShouldProcessIncomingMessage_GroupMentionWithoutBotIDConfigIgnored(t *testing.T) {
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				ChatType: strPtr("group"),
				Content:  strPtr(`{"text":"<at user_id=\"ou_other\">Tom</at> 你好"}`),
				Mentions: []*larkim.MentionEvent{
					{
						Id: &larkim.UserId{
							OpenId: strPtr("ou_other"),
						},
					},
				},
			},
		},
	}

	if shouldProcessIncomingMessage(event, "at", "", "", "") {
		t.Fatal("group message should be ignored when bot IDs are not configured")
	}
}

func TestShouldProcessIncomingMessage_PrivateChatNoMention(t *testing.T) {
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				ChatType: strPtr("p2p"),
				Content:  strPtr(`{"text":"你好"}`),
			},
		},
	}

	if !shouldProcessIncomingMessage(event, "at", "", "", "") {
		t.Fatal("p2p message should be processed without mention")
	}
}

func TestShouldProcessIncomingMessage_GroupPrefixModeRequiresPrefix(t *testing.T) {
	withPrefix := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				ChatType:    strPtr("group"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"!alice 帮我总结一下"}`),
				ChatId:      strPtr("oc_chat"),
			},
		},
	}
	withoutPrefix := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				ChatType:    strPtr("group"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"帮我总结一下"}`),
				ChatId:      strPtr("oc_chat"),
			},
		},
	}

	if !shouldProcessIncomingMessage(withPrefix, "prefix", "!alice", "", "") {
		t.Fatal("group message should be processed in prefix mode when prefix matches")
	}
	if shouldProcessIncomingMessage(withoutPrefix, "prefix", "!alice", "ou_bot", "") {
		t.Fatal("group message without prefix should be ignored in prefix mode even if bot IDs exist")
	}
}

func TestShouldProcessIncomingMessage_BuiltinCommandBypassesGroupTrigger(t *testing.T) {
	for _, text := range []string{"/help", "/status", "/clear", "/stop", "/session sess_123"} {
		event := &larkim.P2MessageReceiveV1{
			Event: &larkim.P2MessageReceiveV1Data{
				Message: &larkim.EventMessage{
					ChatType:    strPtr("group"),
					MessageType: strPtr("text"),
					Content:     strPtr(`{"text":"` + text + `"}`),
					ChatId:      strPtr("oc_chat"),
				},
			},
		}

		if !shouldProcessIncomingMessage(event, "prefix", "!alice", "", "") {
			t.Fatalf("builtin slash command %q should be processed before normal group trigger matching", text)
		}
	}
}

func TestShouldProcessIncomingMessage_GroupAllModeAcceptsEverything(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"plain text without mention or prefix", `{"text":"大家好"}`},
		{"text with prefix", `{"text":"!alice 帮我总结"}`},
		{"unrelated mention", `{"text":"<at user_id=\"ou_other\">Tom</at> 你好"}`},
	}
	for _, tc := range cases {
		event := &larkim.P2MessageReceiveV1{
			Event: &larkim.P2MessageReceiveV1Data{
				Message: &larkim.EventMessage{
					ChatType:    strPtr("group"),
					MessageType: strPtr("text"),
					Content:     strPtr(tc.content),
					ChatId:      strPtr("oc_chat"),
				},
			},
		}
		if !shouldProcessIncomingMessage(event, "all", "", "ou_bot", "") {
			t.Fatalf("group message in all mode should be accepted: %s", tc.name)
		}
	}
}

func TestNormalizeIncomingGroupJobTextForTriggerMode_AllModeKeepsTextIntact(t *testing.T) {
	job := &Job{
		ChatType: "group",
		Text:     "!alice 帮我总结",
	}

	normalizeIncomingGroupJobTextForTriggerMode(job, "all", "!alice")

	if job.Text != "!alice 帮我总结" {
		t.Fatalf("all mode should not strip any prefix, got %q", job.Text)
	}
}

func TestNormalizeIncomingGroupJobTextForTriggerMode_PreservesBuiltinCommand(t *testing.T) {
	for _, text := range []string{"/help", "/status", "/clear", "/stop", "/session sess_123"} {
		job := &Job{
			ChatType: "group",
			Text:     text,
		}

		normalizeIncomingGroupJobTextForTriggerMode(job, "prefix", "/")

		if job.Text != text {
			t.Fatalf("expected builtin command text to stay intact, got %q", job.Text)
		}
	}
}
