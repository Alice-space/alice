package connector

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestProcessor_SessionStatePersistAndLoad(t *testing.T) {
	statePath := t.TempDir() + "/session_state.json"
	processor := NewProcessorWithMemory(
		codexStub{},
		&senderStub{},
		"Codex 暂时不可用，请稍后重试。",
		"正在思考中...",
		&memoryStub{},
	)
	if err := processor.LoadSessionState(statePath); err != nil {
		t.Fatalf("load initial state failed: %v", err)
	}

	now := time.Date(2026, 2, 20, 18, 0, 0, 0, time.UTC)
	sessionKey := "chat_id:oc_chat"
	processor.setThreadID(sessionKey, "thread_1")
	processor.touchSessionMessage(sessionKey, now)
	processor.mu.Lock()
	state := processor.sessions[sessionKey]
	state.LastIdleSummaryAnchor = now
	processor.sessions[sessionKey] = state
	processor.markStateChangedLocked()
	processor.mu.Unlock()

	if err := processor.FlushSessionState(); err != nil {
		t.Fatalf("flush state failed: %v", err)
	}

	loaded := NewProcessorWithMemory(
		codexStub{},
		&senderStub{},
		"Codex 暂时不可用，请稍后重试。",
		"正在思考中...",
		&memoryStub{},
	)
	if err := loaded.LoadSessionState(statePath); err != nil {
		t.Fatalf("load persisted state failed: %v", err)
	}
	if got := loaded.getThreadID(sessionKey); got != "thread_1" {
		t.Fatalf("unexpected thread id after reload: %s", got)
	}

	loaded.mu.Lock()
	loadedState := loaded.sessions[sessionKey]
	loaded.mu.Unlock()
	if !loadedState.LastMessageAt.Equal(now) {
		t.Fatalf("unexpected last message time after reload: %v", loadedState.LastMessageAt)
	}
	if !loadedState.LastIdleSummaryAnchor.Equal(now) {
		t.Fatalf("unexpected idle anchor after reload: %v", loadedState.LastIdleSummaryAnchor)
	}
}

func TestProcessor_IdleSummaryOncePerIdlePeriod(t *testing.T) {
	fakeCodex := &codexResumableCaptureStub{
		respByCall:   []string{"- 关键点1", "- 关键点2"},
		threadByCall: []string{"thread_1", "thread_1"},
	}
	mem := &memoryStub{}
	processor := NewProcessorWithMemory(
		fakeCodex,
		&senderStub{},
		"Codex 暂时不可用，请稍后重试。",
		"正在思考中...",
		mem,
	)

	sessionKey := "chat_id:oc_chat"
	base := time.Date(2026, 2, 20, 9, 0, 0, 0, time.UTC)
	now := base.Add(9 * time.Hour)
	processor.now = func() time.Time { return now }
	processor.setThreadID(sessionKey, "thread_1")
	processor.touchSessionMessage(sessionKey, base)

	processor.RunIdleSummaryScan(context.Background(), 8*time.Hour)
	waitForCondition(t, 2*time.Second, func() bool {
		return mem.dailySummaryCalls == 1
	}, "idle summary should be written once")
	if mem.lastSummarySession != sessionKey {
		t.Fatalf("unexpected summary session key: %s", mem.lastSummarySession)
	}

	processor.RunIdleSummaryScan(context.Background(), 8*time.Hour)
	time.Sleep(120 * time.Millisecond)
	if mem.dailySummaryCalls != 1 {
		t.Fatalf("same idle period should only write once, got %d", mem.dailySummaryCalls)
	}

	newMessageAt := base.Add(10 * time.Hour)
	processor.touchSessionMessage(sessionKey, newMessageAt)
	now = newMessageAt.Add(9 * time.Hour)
	processor.RunIdleSummaryScan(context.Background(), 8*time.Hour)
	waitForCondition(t, 2*time.Second, func() bool {
		return mem.dailySummaryCalls == 2
	}, "new idle period should trigger another summary")
}

func TestProcessor_IdleSummaryAnchorChangedSkipsWriteAndNoParallelRun(t *testing.T) {
	fakeCodex := newBlockingResumableCodexStub()
	mem := &memoryStub{}
	processor := NewProcessorWithMemory(
		fakeCodex,
		&senderStub{},
		"Codex 暂时不可用，请稍后重试。",
		"正在思考中...",
		mem,
	)

	sessionKey := "chat_id:oc_chat"
	base := time.Date(2026, 2, 20, 9, 0, 0, 0, time.UTC)
	processor.now = func() time.Time { return base.Add(9 * time.Hour) }
	processor.setThreadID(sessionKey, "thread_1")
	processor.touchSessionMessage(sessionKey, base)

	processor.RunIdleSummaryScan(context.Background(), 8*time.Hour)
	waitForCondition(t, 2*time.Second, func() bool {
		return fakeCodex.CallCount() == 1
	}, "summary codex call should start")

	processor.RunIdleSummaryScan(context.Background(), 8*time.Hour)
	time.Sleep(100 * time.Millisecond)
	if fakeCodex.CallCount() != 1 {
		t.Fatalf("same session should not run multiple summary tasks in parallel, got %d", fakeCodex.CallCount())
	}

	processor.touchSessionMessage(sessionKey, base.Add(10*time.Hour))
	fakeCodex.Release()

	time.Sleep(120 * time.Millisecond)
	if mem.dailySummaryCalls != 0 {
		t.Fatalf("summary should be skipped when anchor changes, got %d writes", mem.dailySummaryCalls)
	}
}

func TestProcessor_SavesFailureFallbackToMemory(t *testing.T) {
	fakeCodex := &codexCaptureStub{err: errors.New("boom")}
	sender := &senderStub{}
	memory := &memoryStub{prompt: "记忆上下文 + 用户消息"}

	processor := NewProcessorWithMemory(
		fakeCodex,
		sender,
		"Codex 暂时不可用，请稍后重试。",
		"正在思考中...",
		memory,
	)

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:     "oc_chat",
		ReceiveIDType: "chat_id",
		Text:          "hello",
	})

	if memory.saveCalls != 1 {
		t.Fatalf("expected 1 memory save, got %d", memory.saveCalls)
	}
	if !memory.lastSaveFailed {
		t.Fatal("expected failed=true when codex returns error")
	}
	if memory.lastSaveReply != "Codex 暂时不可用，请稍后重试。" {
		t.Fatalf("unexpected fallback reply saved: %s", memory.lastSaveReply)
	}
}

func TestBuildProgressCardContent_UsesCardSchemaV2BodyElements(t *testing.T) {
	content := buildProgressCardContent("思考", "答案", false, false, 1250*time.Millisecond)

	var payload map[string]any
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		t.Fatalf("unmarshal card content failed: %v", err)
	}

	if payload["schema"] != "2.0" {
		t.Fatalf("expected schema 2.0, got %#v", payload["schema"])
	}
	if _, exists := payload["header"]; exists {
		t.Fatalf("card should not include header title")
	}
	if _, exists := payload["elements"]; exists {
		t.Fatalf("schema 2.0 card should not use top-level elements")
	}

	body, ok := payload["body"].(map[string]any)
	if !ok {
		t.Fatalf("expected body object, got %#v", payload["body"])
	}
	elements, ok := body["elements"].([]any)
	if !ok || len(elements) == 0 {
		t.Fatalf("expected non-empty body.elements, got %#v", body["elements"])
	}
	first, ok := elements[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first element object, got %#v", elements[0])
	}
	if first["tag"] != "markdown" {
		t.Fatalf("expected markdown element, got %#v", first["tag"])
	}

	var joined strings.Builder
	for _, element := range elements {
		elementMap, ok := element.(map[string]any)
		if !ok {
			t.Fatalf("expected element object, got %#v", element)
		}
		content, _ := elementMap["content"].(string)
		joined.WriteString(content)
		joined.WriteByte('\n')
	}

	all := joined.String()
	if strings.Contains(all, "Alice 助手") {
		t.Fatalf("card should not include assistant name: %s", all)
	}
	if strings.Contains(all, "你的消息") {
		t.Fatalf("card should not include user message block: %s", all)
	}
	if strings.Contains(all, "更新时间") {
		t.Fatalf("card should not include update timestamp: %s", all)
	}
	if !strings.Contains(all, "耗时：") && !strings.Contains(all, "已思考：") {
		t.Fatalf("card should include elapsed duration: %s", all)
	}
}
