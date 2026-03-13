package agent

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	textPartPattern   = regexp.MustCompile(`(?s)TextPart\(\s*type='text',\s*text='((?:\\.|[^'])*)'`)
	toolCallPattern   = regexp.MustCompile(`(?s)ToolCall\(\s*.*?id='([^']+)'.*?name='([^']+)'.*?arguments='((?:\\.|[^'])*)'`)
	toolResultPattern = regexp.MustCompile(`(?s)ToolResult\(\s*tool_call_id='([^']+)'.*?(?:output='((?:\\.|[^'])*)'|text='((?:\\.|[^'])*)')`)
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
	ID         string `json:"id,omitempty"`
	Name       string `json:"name"`
	Arguments  string `json:"arguments,omitempty"`
	ResultText string `json:"result_text,omitempty"`
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
				ID:        decodeEscapedText(match[1]),
				Name:      decodeEscapedText(match[2]),
				Arguments: decodeEscapedText(match[3]),
			})
		}
	}

	resultMatches := toolResultPattern.FindAllStringSubmatch(transcript.RawConversation, -1)
	if len(resultMatches) > 0 && len(transcript.ToolCalls) > 0 {
		resultByID := make(map[string][]string, len(resultMatches))
		for _, match := range resultMatches {
			id := decodeEscapedText(match[1])
			resultText := match[2]
			if resultText == "" {
				resultText = match[3]
			}
			resultByID[id] = append(resultByID[id], decodeEscapedText(resultText))
		}
		for i := range transcript.ToolCalls {
			results := resultByID[transcript.ToolCalls[i].ID]
			if len(results) == 0 {
				continue
			}
			transcript.ToolCalls[i].ResultText = strings.Join(results, "\n\n")
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

func renderTranscriptMarkdown(req ExecuteRequest, result *ExecuteResult, mcpEnabled bool, duration time.Duration) string {
	var b strings.Builder

	b.WriteString("# Agent Execution Context\n\n")
	b.WriteString("## Metadata\n\n")
	writeMarkdownBullet(&b, "Stage", markdownValue(req.Stage))
	writeMarkdownBullet(&b, "Skill", markdownValue(req.Skill))
	writeMarkdownBullet(&b, "Request ID", markdownValue(req.RequestID))
	writeMarkdownBullet(&b, "Event ID", markdownValue(req.EventID))
	writeMarkdownBullet(&b, "Success", boolString(result.Success))
	writeMarkdownBullet(&b, "Duration", duration.String())
	writeMarkdownBullet(&b, "MCP Enabled", boolString(mcpEnabled))
	if result.Error != "" {
		writeMarkdownBullet(&b, "Error", result.Error)
	}
	b.WriteString("\n")

	b.WriteString("## Input\n\n")
	writeMarkdownSection(&b, "System Prompt", req.SystemPrompt, "text")
	writeMarkdownSection(&b, "Task", req.Task, "text")
	if len(req.Context) > 0 {
		writeMarkdownSection(&b, "Context", mustMarshalPretty(req.Context), "json")
	}
	writeMarkdownSection(&b, "Constraints", mustMarshalPretty(req.Constraints), "json")
	writeMarkdownSection(&b, "Rendered Prompt", result.Transcript.Prompt, "text")

	b.WriteString("## Output\n\n")
	writeMarkdownSection(&b, "Final Text", result.Transcript.FinalText, "text")
	if result.StructuredOutput != nil {
		writeMarkdownSection(&b, "Structured Output", mustMarshalPretty(result.StructuredOutput), "json")
	}
	if len(result.Actions) > 0 {
		writeMarkdownSection(&b, "Actions", strings.Join(result.Actions, "\n"), "text")
	}

	b.WriteString("## Tool Calls\n\n")
	if len(result.Transcript.ToolCalls) == 0 {
		b.WriteString("_none_\n\n")
	} else {
		for i, call := range result.Transcript.ToolCalls {
			b.WriteString("### ")
			b.WriteString(strconv.Itoa(i + 1))
			b.WriteString(". ")
			b.WriteString(call.Name)
			b.WriteString("\n\n")
			writeMarkdownBullet(&b, "Tool Call ID", markdownValue(call.ID))
			b.WriteString("\n")
			writeMarkdownSection(&b, "Arguments", call.Arguments, markdownLanguage(call.Arguments))
			writeMarkdownSection(&b, "Result", call.ResultText, markdownLanguage(call.ResultText))
		}
	}

	b.WriteString("## Raw Conversation\n\n")
	writeMarkdownSection(&b, "Transcript", result.Transcript.RawConversation, "text")

	return b.String()
}

func writeMarkdownBullet(b *strings.Builder, key, value string) {
	b.WriteString("- ")
	b.WriteString(key)
	b.WriteString(": ")
	b.WriteString(value)
	b.WriteString("\n")
}

func writeMarkdownSection(b *strings.Builder, title, content, language string) {
	b.WriteString("### ")
	b.WriteString(title)
	b.WriteString("\n\n")
	if strings.TrimSpace(content) == "" {
		b.WriteString("_empty_\n\n")
		return
	}
	b.WriteString("~~~")
	b.WriteString(language)
	b.WriteString("\n")
	b.WriteString(content)
	if !strings.HasSuffix(content, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("~~~\n\n")
}

func markdownLanguage(content string) string {
	var js json.RawMessage
	if json.Unmarshal([]byte(content), &js) == nil {
		return "json"
	}
	return "text"
}

func mustMarshalPretty(v any) string {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(data)
}

func markdownValue(v string) string {
	if strings.TrimSpace(v) == "" {
		return "_none_"
	}
	return v
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func sanitizeArtifactPart(v string) string {
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range v {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	cleaned := strings.Trim(b.String(), "_")
	if cleaned == "" {
		return "unknown"
	}
	return cleaned
}
