package agent

import (
	"regexp"
	"strings"
)

var (
	textPartPattern = regexp.MustCompile(`(?s)TextPart\(\s*type='text',\s*text='((?:\\.|[^'])*)'`)
	toolCallPattern = regexp.MustCompile(`(?s)ToolCall\(\s*.*?name='([^']+)'.*?arguments='((?:\\.|[^'])*)'`)
)

// Transcript captures the raw agent exchange together with commonly used extracts.
type Transcript struct {
	Prompt          string               `json:"prompt"`
	RawOutput       string               `json:"raw_output"`
	RawConversation string               `json:"raw_conversation"`
	FinalText       string               `json:"final_text,omitempty"`
	ToolCalls       []TranscriptToolCall `json:"tool_calls,omitempty"`
}

// TranscriptToolCall records a tool call from the raw agent conversation.
type TranscriptToolCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments,omitempty"`
}

func parseTranscript(prompt, output string) Transcript {
	transcript := Transcript{
		Prompt:          prompt,
		RawOutput:       output,
		RawConversation: extractConversation(output),
	}

	if transcript.RawConversation == "" {
		transcript.RawConversation = strings.TrimSpace(output)
	}

	textMatches := textPartPattern.FindAllStringSubmatch(transcript.RawConversation, -1)
	if len(textMatches) > 0 {
		transcript.FinalText = decodeEscapedText(textMatches[len(textMatches)-1][1])
	}

	toolMatches := toolCallPattern.FindAllStringSubmatch(transcript.RawConversation, -1)
	if len(toolMatches) > 0 {
		transcript.ToolCalls = make([]TranscriptToolCall, 0, len(toolMatches))
		for _, match := range toolMatches {
			transcript.ToolCalls = append(transcript.ToolCalls, TranscriptToolCall{
				Name:      decodeEscapedText(match[1]),
				Arguments: decodeEscapedText(match[2]),
			})
		}
	}

	return transcript
}

func extractConversation(output string) string {
	trimmed := strings.TrimSpace(output)
	if idx := strings.Index(trimmed, "TurnBegin("); idx >= 0 {
		return strings.TrimSpace(trimmed[idx:])
	}
	return trimmed
}

func decodeEscapedText(v string) string {
	replacer := strings.NewReplacer(
		`\n`, "\n",
		`\t`, "\t",
		`\r`, "\r",
		`\"`, `"`,
		`\'`, `'`,
		`\\`, `\`,
	)
	return replacer.Replace(v)
}
