package agent

import (
	"strings"
	"testing"
	"time"
)

func TestParseTranscriptExtractsFinalTextAndToolCalls(t *testing.T) {
	prompt := "# Task\n\n查询上海明天天气"
	output := `noise before
TurnBegin(
    user_input='hello'
)
ToolCall(
    type='function',
    id='tool_123',
    function=FunctionBody(
        name='SearchWeb',
        arguments='{"query":"上海天气"}'
    )
)
ToolResult(
    tool_call_id='tool_123',
    return_value=ToolOk(
        is_error=False,
        output=[
            TextPart(
                type='text',
                text='result line 1\nresult line 2'
            )
        ],
        message='',
        display=[],
        extras=None
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
	if transcript.ToolCalls[0].ID != "tool_123" {
		t.Fatalf("unexpected tool call id: %q", transcript.ToolCalls[0].ID)
	}
	if transcript.ToolCalls[0].Arguments != `{"query":"上海天气"}` {
		t.Fatalf("unexpected tool arguments: %q", transcript.ToolCalls[0].Arguments)
	}
	if transcript.ToolCalls[0].ResultText != "result line 1\nresult line 2" {
		t.Fatalf("unexpected tool result: %q", transcript.ToolCalls[0].ResultText)
	}
}

func TestParseTranscriptExtractsToolReturnValueOutput(t *testing.T) {
	output := `TurnBegin(
    user_input='hello'
)
ToolCall(
    type='function',
    id='tool_search',
    function=FunctionBody(
        name='SearchWeb',
        arguments='{"query":"上海明天天气"}'
    )
)
ToolResult(
    tool_call_id='tool_search',
    return_value=ToolReturnValue(
        is_error=False,
        output='Title: 上海天气预报\nDate: 2026-03-13'
    )
)
TurnEnd()
`

	transcript := parseTranscript("# prompt", output)
	if len(transcript.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(transcript.ToolCalls))
	}
	if got := transcript.ToolCalls[0].ResultText; got != "Title: 上海天气预报\nDate: 2026-03-13" {
		t.Fatalf("unexpected tool return value output: %q", got)
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

func TestSelectStructuredOutputPrefersPromotionDecisionForReception(t *testing.T) {
	transcript := Transcript{
		ToolCalls: []TranscriptToolCall{
			{
				Name:       "submit_promotion_decision",
				ResultText: "{\"type\":\"promotion_decision\",\"payload\":{\"intent_kind\":\"dir\nect_query\",\"confidence\":0.95}}",
			},
			{
				Name:       "submit_direct_answer",
				ResultText: "{\"type\":\"direct_answer\",\"payload\":{\"answer\":\"上海明天\n晴\"}}",
			},
		},
	}

	outputs := extractMCPToolOutputs(transcript, "")
	if len(outputs) != 2 {
		t.Fatalf("expected two outputs, got %d", len(outputs))
	}

	selected := selectStructuredOutput(ExecuteRequest{
		Stage: "reception",
		Skill: "reception-assessment",
	}, outputs)
	if selected == nil {
		t.Fatal("expected selected structured output")
	}
	if got := selected["_output_type"]; got != "promotion_decision" {
		t.Fatalf("unexpected output type: %#v", got)
	}
	if got := selected["intent_kind"]; got != "direct_query" {
		t.Fatalf("unexpected intent_kind: %#v", got)
	}
}

func TestSelectStructuredOutputPrefersDirectAnswerForDirectAnswerStage(t *testing.T) {
	transcript := Transcript{
		ToolCalls: []TranscriptToolCall{
			{
				Name:       "submit_promotion_decision",
				ResultText: `{"type":"promotion_decision","payload":{"intent_kind":"direct_query"}}`,
			},
			{
				Name:       "submit_direct_answer",
				ResultText: `{"type":"direct_answer","payload":{"answer":"上海明天晴"}}`,
			},
		},
	}

	selected := selectStructuredOutput(ExecuteRequest{
		Stage: "direct_answer",
	}, extractMCPToolOutputs(transcript, ""))
	if selected == nil {
		t.Fatal("expected selected structured output")
	}
	if got := selected["_output_type"]; got != "direct_answer" {
		t.Fatalf("unexpected output type: %#v", got)
	}
	if got := selected["answer"]; got != "上海明天晴" {
		t.Fatalf("unexpected answer: %#v", got)
	}
}

func TestRenderTranscriptMarkdownIncludesToolResults(t *testing.T) {
	req := ExecuteRequest{
		RequestID:    "req_123",
		EventID:      "evt_123",
		Stage:        "reception",
		Skill:        "reception-assessment",
		SystemPrompt: "system prompt",
		Task:         "task body",
		Constraints: ExecuteConstraints{
			ReadOnly: true,
		},
	}
	result := &ExecuteResult{
		Success: true,
		Transcript: Transcript{
			Prompt:          "# prompt",
			RawConversation: "TurnBegin(...)\nToolCall(...)",
			FinalText:       "final answer",
			ToolCalls: []TranscriptToolCall{
				{
					ID:         "tool_123",
					Name:       "SearchWeb",
					Arguments:  `{"query":"上海天气"}`,
					ResultText: `{"type":"search","payload":"晴"}`,
				},
			},
		},
		StructuredOutput: map[string]interface{}{
			"intent_kind": "direct_query",
		},
	}

	md := renderTranscriptMarkdown(req, result, true, 2*time.Second)
	for _, want := range []string{
		"# Agent Execution Context",
		"- Request ID: req_123",
		"### 1. SearchWeb",
		`{"query":"上海天气"}`,
		`{"type":"search","payload":"晴"}`,
		"## Raw Conversation",
		"TurnBegin(...)",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("markdown missing %q\n%s", want, md)
		}
	}
}

func TestExtractExecutionError(t *testing.T) {
	output := "TurnBegin()\nStepInterrupted()\nError code: 401 - {'error':'invalid_authentication_error'}\n"
	if got := extractExecutionError(output); got != "Error code: 401 - {'error':'invalid_authentication_error'}" {
		t.Fatalf("unexpected execution error: %q", got)
	}
}
