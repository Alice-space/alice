package connector

import "testing"

func TestSanitizeMarkdownForPlainText_PreservesMentionAndStripsMarkdown(t *testing.T) {
	input := `<at user_id="ou_bob">Bob</at> **请看** 这个` + "`结果`" + ` 和 [详情](https://example.com)
> 引用内容`
	got := sanitizeMarkdownForPlainText(input)
	want := `<at user_id="ou_bob">Bob</at> 请看 这个结果 和 详情
引用内容`
	if got != want {
		t.Fatalf("unexpected sanitized plain text:\nwant: %q\ngot : %q", want, got)
	}
}
