package llm

import (
	"encoding/json"
	"testing"
)

func TestCodexNotificationSuppressesAgentMessageDeltas(t *testing.T) {
	if event, ok := parseCodexNotification(testRPCNotification(t, "item/agentMessage/delta", map[string]any{
		"threadId": "thread",
		"turnId":   "turn",
		"delta":    "hel",
	})); ok {
		t.Fatalf("delta event = %#v, want suppressed", event)
	}

	event, ok := parseCodexNotification(testRPCNotification(t, "item/completed", map[string]any{
		"threadId": "thread",
		"turnId":   "turn",
		"item": map[string]any{
			"type": "agentMessage",
			"text": "hello",
		},
	}))
	if !ok {
		t.Fatal("completed agentMessage was suppressed")
	}
	if event.Kind != TurnEventAssistantText || event.Text != "hello" {
		t.Fatalf("event = %#v, want assistant_text hello", event)
	}
}

func TestCodexNotificationNormalizesToolCallsSeparately(t *testing.T) {
	event, ok := parseCodexNotification(testRPCNotification(t, "item/completed", map[string]any{
		"threadId": "thread",
		"turnId":   "turn",
		"item": map[string]any{
			"type":    "commandExecution",
			"command": "pwd",
		},
	}))
	if !ok {
		t.Fatal("commandExecution event was suppressed")
	}
	if event.Kind != TurnEventToolUse || event.Text != "pwd" {
		t.Fatalf("event = %#v, want tool_use pwd", event)
	}
}

func TestClaudeStreamDriverNormalizesAssistantTextAndToolUse(t *testing.T) {
	driver := newClaudeStreamDriver(ClaudeConfig{})
	driver.activeID = "turn"

	textEvent, ok := driver.parseClaudeLine(`{"type":"assistant","session_id":"claude-session","message":{"role":"assistant","content":[{"type":"text","text":"middle"}]}}`)
	if !ok {
		t.Fatal("assistant text event was suppressed")
	}
	if textEvent.Kind != TurnEventAssistantText || textEvent.Text != "middle" {
		t.Fatalf("text event = %#v, want assistant_text middle", textEvent)
	}

	toolEvent, ok := driver.parseClaudeLine(`{"type":"assistant","session_id":"claude-session","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","id":"toolu_1","input":{"command":"pwd"}}]}}`)
	if !ok {
		t.Fatal("tool_use event was suppressed")
	}
	if toolEvent.Kind != TurnEventToolUse || toolEvent.Text != "tool_use name=`Bash` id=`toolu_1`" {
		t.Fatalf("tool event = %#v, want tool_use detail", toolEvent)
	}
}

func TestOpenCodeAppServerEventNormalizesAssistantTextAndToolUse(t *testing.T) {
	driver := newOpenCodeAppServerDriver(OpenCodeConfig{})
	driver.sessionID = "session-1"
	driver.activeID = "turn-1"

	if event, ok := driver.parseOpenCodeEvent(mustJSON(t, map[string]any{
		"type": "message.part.delta",
		"properties": map[string]any{
			"sessionID": "session-1",
			"messageID": "msg-1",
			"partID":    "part-1",
			"field":     "text",
			"delta":     "hel",
		},
	})); ok {
		t.Fatalf("delta event = %#v, want suppressed", event)
	}

	if event, ok := driver.parseOpenCodeEvent(mustJSON(t, map[string]any{
		"type": "message.part.updated",
		"properties": map[string]any{
			"sessionID": "session-1",
			"part": map[string]any{
				"id":        "part-1",
				"sessionID": "session-1",
				"messageID": "msg-1",
				"type":      "text",
				"text":      "middle",
				"time":      map[string]any{"start": 1},
			},
		},
	})); ok {
		t.Fatalf("incomplete text part event = %#v, want suppressed", event)
	}

	if event, ok := driver.parseOpenCodeEvent(mustJSON(t, map[string]any{
		"type": "message.updated",
		"properties": map[string]any{
			"sessionID": "session-1",
			"info": map[string]any{
				"id":        "msg-user",
				"sessionID": "session-1",
				"role":      "user",
			},
		},
	})); ok {
		t.Fatalf("user message role event = %#v, want suppressed", event)
	}
	userTextEvent, ok := driver.parseOpenCodeEvent(mustJSON(t, map[string]any{
		"type": "message.part.updated",
		"properties": map[string]any{
			"sessionID": "session-1",
			"part": map[string]any{
				"id":        "part-user",
				"sessionID": "session-1",
				"messageID": "msg-user",
				"type":      "text",
				"text":      "user prompt should not be forwarded",
				"time":      map[string]any{"start": 1, "end": 2},
			},
		},
	}))
	if !ok {
		t.Fatal("user text part event was suppressed")
	}
	if userTextEvent.Kind != TurnEventUserText || userTextEvent.Text != "user prompt should not be forwarded" {
		t.Fatalf("user text event = %#v, want user_text", userTextEvent)
	}

	textEvent, ok := driver.parseOpenCodeEvent(mustJSON(t, map[string]any{
		"type": "message.part.updated",
		"properties": map[string]any{
			"sessionID": "session-1",
			"part": map[string]any{
				"id":        "part-1",
				"sessionID": "session-1",
				"messageID": "msg-1",
				"type":      "text",
				"text":      "middle",
				"time":      map[string]any{"start": 1, "end": 2},
			},
		},
	}))
	if !ok {
		t.Fatal("completed text part event was suppressed")
	}
	if textEvent.Kind != TurnEventAssistantText || textEvent.Text != "middle" {
		t.Fatalf("text event = %#v, want assistant_text middle", textEvent)
	}

	if event, ok := driver.parseOpenCodeEvent(mustJSON(t, map[string]any{
		"type": "message.part.updated",
		"properties": map[string]any{
			"sessionID": "session-1",
			"part": map[string]any{
				"id":        "part-1",
				"sessionID": "session-1",
				"messageID": "msg-1",
				"type":      "text",
				"text":      "middle",
				"time":      map[string]any{"start": 1, "end": 2},
			},
		},
	})); ok {
		t.Fatalf("duplicate text event = %#v, want suppressed", event)
	}

	if event, ok := driver.parseOpenCodeEvent(mustJSON(t, map[string]any{
		"type": "message.updated",
		"properties": map[string]any{
			"sessionID": "session-1",
			"info": map[string]any{
				"id":        "msg-assistant",
				"sessionID": "session-1",
				"role":      "assistant",
			},
		},
	})); ok {
		t.Fatalf("assistant message role event = %#v, want suppressed", event)
	}
	assistantRoleTextEvent, ok := driver.parseOpenCodeEvent(mustJSON(t, map[string]any{
		"type": "message.part.updated",
		"properties": map[string]any{
			"sessionID": "session-1",
			"part": map[string]any{
				"id":        "part-assistant",
				"sessionID": "session-1",
				"messageID": "msg-assistant",
				"type":      "text",
				"text":      "assistant role text",
			},
		},
	}))
	if !ok {
		t.Fatal("assistant role text part event was suppressed")
	}
	if assistantRoleTextEvent.Kind != TurnEventAssistantText || assistantRoleTextEvent.Text != "assistant role text" {
		t.Fatalf("assistant role text event = %#v, want assistant_text", assistantRoleTextEvent)
	}

	toolEvent, ok := driver.parseOpenCodeEvent(mustJSON(t, map[string]any{
		"type": "message.part.updated",
		"properties": map[string]any{
			"sessionID": "session-1",
			"part": map[string]any{
				"id":        "part-2",
				"sessionID": "session-1",
				"messageID": "msg-1",
				"type":      "tool",
				"tool":      "bash",
				"callID":    "call-1",
				"state": map[string]any{
					"status": "completed",
					"input":  map[string]any{"command": "pwd"},
				},
			},
		},
	}))
	if !ok {
		t.Fatal("tool part event was suppressed")
	}
	if toolEvent.Kind != TurnEventToolUse || toolEvent.Text != "tool_use tool=`bash` call_id=`call-1` status=`completed` command=`pwd`" {
		t.Fatalf("tool event = %#v, want tool_use detail", toolEvent)
	}

	if event, ok := driver.parseOpenCodeEvent(mustJSON(t, map[string]any{
		"type": "message.updated",
		"properties": map[string]any{
			"sessionID": "session-1",
			"info": map[string]any{
				"id":        "msg-tool-calls",
				"sessionID": "session-1",
				"role":      "assistant",
				"time":      map[string]any{"completed": 4},
				"finish":    "tool-calls",
			},
		},
	})); ok {
		t.Fatalf("tool-calls assistant completion event = %#v, want suppressed", event)
	}

	completedEvent, ok := driver.parseOpenCodeEvent(mustJSON(t, map[string]any{
		"type": "message.updated",
		"properties": map[string]any{
			"sessionID": "session-1",
			"info": map[string]any{
				"id":        "msg-assistant",
				"sessionID": "session-1",
				"role":      "assistant",
				"time":      map[string]any{"completed": 3},
				"finish":    "stop",
				"tokens": map[string]any{
					"input":  7,
					"output": 11,
					"cache":  map[string]any{"read": 2, "write": 0},
				},
			},
		},
	}))
	if !ok {
		t.Fatal("assistant completed message event was suppressed")
	}
	if completedEvent.Kind != TurnEventCompleted || completedEvent.ThreadID != "session-1" || completedEvent.TurnID != "turn-1" {
		t.Fatalf("completed event = %#v, want turn_completed for active turn", completedEvent)
	}
	if completedEvent.Usage.InputTokens != 7 || completedEvent.Usage.OutputTokens != 11 || completedEvent.Usage.CachedInputTokens != 2 {
		t.Fatalf("completed usage = %#v, want 7/11/2", completedEvent.Usage)
	}
}

func TestOpenCodeAppServerSessionIdleCompletesActiveTurn(t *testing.T) {
	driver := newOpenCodeAppServerDriver(OpenCodeConfig{})
	driver.sessionID = "session-1"
	driver.activeID = "turn-1"

	if event, ok := driver.parseOpenCodeEvent(mustJSON(t, map[string]any{
		"type": "session.idle",
		"properties": map[string]any{
			"sessionID": "session-1",
		},
	})); ok {
		t.Fatalf("idle without assistant text event = %#v, want suppressed", event)
	}

	textEvent, ok := driver.parseOpenCodeEvent(mustJSON(t, map[string]any{
		"type": "message.part.updated",
		"properties": map[string]any{
			"sessionID": "session-1",
			"part": map[string]any{
				"id":        "part-1",
				"sessionID": "session-1",
				"messageID": "msg-assistant",
				"type":      "text",
				"text":      "done",
				"time":      map[string]any{"start": 1, "end": 2},
			},
		},
	}))
	if !ok || textEvent.Kind != TurnEventAssistantText {
		t.Fatalf("text event = %#v ok=%v, want assistant_text", textEvent, ok)
	}

	event, ok := driver.parseOpenCodeEvent(mustJSON(t, map[string]any{
		"type": "session.idle",
		"properties": map[string]any{
			"sessionID": "session-1",
		},
	}))
	if !ok {
		t.Fatal("session idle event was suppressed")
	}
	if event.Kind != TurnEventCompleted || event.ThreadID != "session-1" || event.TurnID != "turn-1" {
		t.Fatalf("event = %#v, want turn_completed for active session", event)
	}
}

func TestKimiNotificationCoalescesContentParts(t *testing.T) {
	driver := newKimiWireDriver(KimiConfig{})
	driver.threadID = "thread"
	driver.activeID = "turn"

	started := driver.parseKimiNotification(testKimiEvent(t, "TurnBegin", map[string]any{
		"user_input": "go",
	}))
	if len(started) != 1 || started[0].Kind != TurnEventStarted {
		t.Fatalf("started events = %#v, want one turn_started", started)
	}

	if events := driver.parseKimiNotification(testKimiEvent(t, "ContentPart", map[string]any{"text": "hel"})); len(events) != 0 {
		t.Fatalf("first content events = %#v, want suppressed fragment", events)
	}
	if events := driver.parseKimiNotification(testKimiEvent(t, "ContentPart", map[string]any{"text": "lo"})); len(events) != 0 {
		t.Fatalf("second content events = %#v, want suppressed fragment", events)
	}

	ended := driver.parseKimiNotification(testKimiEvent(t, "TurnEnd", map[string]any{"status": "completed"}))
	if len(ended) != 2 {
		t.Fatalf("ended events = %#v, want assistant text and completion", ended)
	}
	if ended[0].Kind != TurnEventAssistantText || ended[0].Text != "hello" {
		t.Fatalf("assistant event = %#v, want coalesced hello", ended[0])
	}
	if ended[1].Kind != TurnEventCompleted {
		t.Fatalf("completion event = %#v, want turn_completed", ended[1])
	}
}

func testKimiEvent(t *testing.T, eventType string, payload map[string]any) rpcNotification {
	t.Helper()
	return testRPCNotification(t, "event", map[string]any{
		"type":    eventType,
		"payload": payload,
	})
}

func testRPCNotification(t *testing.T, method string, params any) rpcNotification {
	t.Helper()
	rawParams, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	raw, err := json.Marshal(map[string]any{
		"method": method,
		"params": params,
	})
	if err != nil {
		t.Fatalf("marshal raw notification: %v", err)
	}
	return rpcNotification{Method: method, Params: rawParams, Raw: string(raw)}
}
