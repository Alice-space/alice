package agent

import "testing"

func TestParseTranscriptExtractsFinalTextAndToolCalls(t *testing.T) {
	prompt := "# Task\n\n查询上海明天天气"
	output := `noise before
TurnBegin(
    user_input='hello'
)
ToolCall(
    type='function',
    function=FunctionBody(
        name='SearchWeb',
        arguments='{"query":"上海天气"}'
    )
)
TextPart(
    type='text',
    text='上海明天晴转多云\n最高 20C'
)
TurnEnd()
`

	transcript := parseTranscript(prompt, output)
	if transcript.Prompt != prompt {
		t.Fatalf("prompt mismatch: %q", transcript.Prompt)
	}
	if transcript.RawConversation == "" || transcript.RawConversation[:10] != "TurnBegin(" {
		t.Fatalf("expected conversation to start at TurnBegin, got %q", transcript.RawConversation)
	}
	if transcript.FinalText != "上海明天晴转多云\n最高 20C" {
		t.Fatalf("unexpected final text: %q", transcript.FinalText)
	}
	if len(transcript.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(transcript.ToolCalls))
	}
	if transcript.ToolCalls[0].Name != "SearchWeb" {
		t.Fatalf("unexpected tool call name: %q", transcript.ToolCalls[0].Name)
	}
	if transcript.ToolCalls[0].Arguments != `{"query":"上海天气"}` {
		t.Fatalf("unexpected tool arguments: %q", transcript.ToolCalls[0].Arguments)
	}
}

func TestExtractMCPToolOutputPrefersLastWrapper(t *testing.T) {
	output := `{"type":"promotion_decision","payload":{"intent_kind":"direct_query","confidence":0.8}}
{"type":"direct_answer","payload":{"answer":"上海明天晴","citations":["weather.com.cn"]}}`

	result := extractMCPToolOutput(output)
	if result == nil {
		t.Fatal("expected structured output")
	}
	if got := result["_output_type"]; got != "direct_answer" {
		t.Fatalf("unexpected output type: %#v", got)
	}
	if got := result["answer"]; got != "上海明天晴" {
		t.Fatalf("unexpected answer: %#v", got)
	}
}

func TestExtractMCPToolOutputHandlesEscapedJSONSnippet(t *testing.T) {
	output := `ToolResult(output="{\"type\":\"direct_answer\",\"payload\":{\"answer\":\"上海明天晴\"}}")`

	result := extractMCPToolOutput(output)
	if result == nil {
		t.Fatal("expected structured output from escaped snippet")
	}
	if got := result["answer"]; got != "上海明天晴" {
		t.Fatalf("unexpected answer: %#v", got)
	}
}
