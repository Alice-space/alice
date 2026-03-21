package connector

import "testing"

func TestNormalizeOutgoingReplyWithMentions_ConvertsNameAndID(t *testing.T) {
	converted, changed := normalizeOutgoingReplyWithMentions(
		"@Xiang Shi 请看，@ou_776ddbea0c07fd88caaf8fce1b413a41 也看看。",
		Job{
			SenderName:   "Xiang Shi",
			SenderOpenID: "ou_809a189717a7a855905957ea612ca9f8",
			MentionedUsers: []MentionedUser{
				{
					Name:   "李志昊",
					OpenID: "ou_776ddbea0c07fd88caaf8fce1b413a41",
				},
			},
		},
	)

	if !changed {
		t.Fatal("expected mention replacement to happen")
	}
	want := `<at user_id="ou_809a189717a7a855905957ea612ca9f8">Xiang Shi</at> 请看，` +
		`<at user_id="ou_776ddbea0c07fd88caaf8fce1b413a41">李志昊</at> 也看看。`
	if converted != want {
		t.Fatalf("unexpected converted text:\nwant: %s\ngot : %s", want, converted)
	}
}

func TestNormalizeOutgoingReplyWithMentions_SkipsEmailAndBot(t *testing.T) {
	converted, changed := normalizeOutgoingReplyWithMentions(
		"邮箱是 a@corp.com，@Alice 不替换，@Bob 需要替换。",
		Job{
			BotOpenID:    "ou_alice",
			BotUserID:    "u_alice",
			SenderName:   "Alice",
			SenderOpenID: "ou_alice",
			SenderUserID: "u_alice",
			MentionedUsers: []MentionedUser{
				{
					Name:   "Bob",
					OpenID: "ou_bob",
				},
			},
		},
	)

	if !changed {
		t.Fatal("expected mention replacement for non-bot mention")
	}
	want := "邮箱是 a@corp.com，@Alice 不替换，<at user_id=\"ou_bob\">Bob</at> 需要替换。"
	if converted != want {
		t.Fatalf("unexpected converted text:\nwant: %s\ngot : %s", want, converted)
	}
}

func TestNormalizeOutgoingReplyWithMentions_StripsReplyWillBlock(t *testing.T) {
	converted, changed := normalizeOutgoingReplyWithMentions(
		"<reply_will>72%</reply_will>\n@Bob 需要你看看。",
		Job{
			MentionedUsers: []MentionedUser{
				{
					Name:   "Bob",
					OpenID: "ou_bob",
				},
			},
		},
	)

	if !changed {
		t.Fatal("expected mention replacement after stripping reply_will block")
	}
	want := `<at user_id="ou_bob">Bob</at> 需要你看看。`
	if converted != want {
		t.Fatalf("unexpected converted text:\nwant: %s\ngot : %s", want, converted)
	}
}
