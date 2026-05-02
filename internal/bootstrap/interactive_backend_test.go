package bootstrap

import (
	"testing"

	agentbridge "github.com/Alice-space/agentbridge"
)

func TestUpdateAssistantTextAccumulatesCodexDeltas(t *testing.T) {
	text := ""
	for _, event := range []agentbridge.TurnEvent{
		{Provider: agentbridge.ProviderCodex, Text: "你", Raw: `{"method":"item/agentMessage/delta"}`},
		{Provider: agentbridge.ProviderCodex, Text: "好", Raw: `{"method":"item/agentMessage/delta"}`},
	} {
		text = updateAssistantText(text, event)
	}
	if text != "你好" {
		t.Fatalf("expected accumulated deltas, got %q", text)
	}

	text = updateAssistantText(text, agentbridge.TurnEvent{
		Provider: agentbridge.ProviderCodex,
		Text:     "你好，已完成。",
		Raw:      `{"method":"item/completed"}`,
	})
	if text != "你好，已完成。" {
		t.Fatalf("expected completed assistant message to replace accumulator, got %q", text)
	}
}

func TestUpdateAssistantTextAccumulatesKimiContentParts(t *testing.T) {
	text := ""
	for _, event := range []agentbridge.TurnEvent{
		{Provider: agentbridge.ProviderKimi, Text: "你", Raw: `{"params":{"type":"ContentPart"}}`},
		{Provider: agentbridge.ProviderKimi, Text: "好", Raw: `{"params":{"type":"ContentPart"}}`},
	} {
		text = updateAssistantText(text, event)
	}
	if text != "你好" {
		t.Fatalf("expected accumulated Kimi content parts, got %q", text)
	}
}

func TestUpdateAssistantTextReplacesCompleteMessages(t *testing.T) {
	text := updateAssistantText("", agentbridge.TurnEvent{
		Provider: agentbridge.ProviderClaude,
		Text:     "第一条完整消息",
		Raw:      `{"type":"assistant"}`,
	})
	text = updateAssistantText(text, agentbridge.TurnEvent{
		Provider: agentbridge.ProviderClaude,
		Text:     "第二条完整消息",
		Raw:      `{"type":"assistant"}`,
	})
	if text != "第二条完整消息" {
		t.Fatalf("expected complete assistant messages to replace, got %q", text)
	}
}
