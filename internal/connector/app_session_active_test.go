package connector

import "testing"

func TestApp_IsSessionActive_NoActiveSession(t *testing.T) {
	app := testAppForSessionActive()
	if app.IsSessionActive("chat_id:oc_xxx") {
		t.Fatal("expected false when no active sessions")
	}
}

func TestApp_IsSessionActive_ExactMatch(t *testing.T) {
	app := testAppForSessionActive()
	app.state.mu.Lock()
	app.state.active["chat_id:oc_xxx"] = activeSessionRun{version: 1}
	app.state.mu.Unlock()

	if !app.IsSessionActive("chat_id:oc_xxx") {
		t.Fatal("expected true for exact key match")
	}
}

func TestApp_IsSessionActive_DecoratedActiveKey(t *testing.T) {
	app := testAppForSessionActive()
	app.state.mu.Lock()
	app.state.active["chat_id:oc_xxx|reset:123"] = activeSessionRun{version: 1}
	app.state.mu.Unlock()

	if !app.IsSessionActive("chat_id:oc_xxx") {
		t.Fatal("expected true: visibility key matches despite decorator")
	}
}

func TestApp_IsSessionActive_DifferentChat(t *testing.T) {
	app := testAppForSessionActive()
	app.state.mu.Lock()
	app.state.active["chat_id:oc_other"] = activeSessionRun{version: 1}
	app.state.mu.Unlock()

	if app.IsSessionActive("chat_id:oc_xxx") {
		t.Fatal("expected false: different chat should not match")
	}
}

func TestApp_IsSessionActive_NilApp(t *testing.T) {
	var app *App
	if app.IsSessionActive("chat_id:oc_xxx") {
		t.Fatal("expected false for nil App")
	}
}

func TestApp_IsSessionActive_EmptyKey(t *testing.T) {
	app := testAppForSessionActive()
	if app.IsSessionActive("") {
		t.Fatal("expected false for empty session key")
	}
}

func TestApp_IsSessionActive_DifferentThreadNotBusy(t *testing.T) {
	app := testAppForSessionActive()
	// Active session is in work thread om_AAA
	app.state.mu.Lock()
	app.state.active["chat_id:oc_xxx|work:om_AAA"] = activeSessionRun{version: 1}
	app.state.mu.Unlock()

	// Task targets a different thread (work om_BBB) — ThreadScope now
	// recognizes |work: tokens, so different threads do not block each other.
	if app.IsSessionActive("chat_id:oc_xxx|work:om_BBB") {
		t.Fatal("expected false: different work threads should not block each other")
	}
}

func TestApp_IsSessionActive_SameThreadIsBusy(t *testing.T) {
	app := testAppForSessionActive()
	// Active session is in work thread om_AAA
	app.state.mu.Lock()
	app.state.active["chat_id:oc_xxx|work:om_AAA"] = activeSessionRun{version: 1}
	app.state.mu.Unlock()

	// Task targets the same thread — must be blocked
	if !app.IsSessionActive("chat_id:oc_xxx|work:om_AAA") {
		t.Fatal("expected true: same work thread must be busy")
	}
}

func TestApp_IsSessionActive_PlainGroupNotBlockedByThread(t *testing.T) {
	app := testAppForSessionActive()
	// Active session is in a work thread — ThreadScope now returns
	// "|seed:om_AAA", which differs from plain group scope ("").
	app.state.mu.Lock()
	app.state.active["chat_id:oc_xxx|work:om_AAA"] = activeSessionRun{version: 1}
	app.state.mu.Unlock()

	// A plain group-level task (no thread) must NOT be blocked by a work
	// session because ThreadScope differs.
	if app.IsSessionActive("chat_id:oc_xxx") {
		t.Fatal("expected false: plain group task should not be blocked by work thread in same chat")
	}
}

func TestApp_TryAcquireSession_DifferentWorkThreadsDoNotBlock(t *testing.T) {
	app := testAppForSessionActive()
	cancelA := func(error) {}
	cancelB := func(error) {}

	// Acquire work thread A
	if !app.TryAcquireSession("chat_id:oc_xxx|work:om_AAA", cancelA) {
		t.Fatal("expected true: first work thread should acquire")
	}

	// Try to acquire different work thread B — must NOT be blocked
	if !app.TryAcquireSession("chat_id:oc_xxx|work:om_BBB", cancelB) {
		t.Fatal("expected true: different work threads should not block each other")
	}

	// Release both
	app.ReleaseSession("chat_id:oc_xxx|work:om_AAA")
	app.ReleaseSession("chat_id:oc_xxx|work:om_BBB")
}

func TestApp_TryAcquireSession_SameWorkThreadBlocks(t *testing.T) {
	app := testAppForSessionActive()

	// Acquire work thread A
	if !app.TryAcquireSession("chat_id:oc_xxx|work:om_AAA", func(error) {}) {
		t.Fatal("expected true: first acquire should succeed")
	}

	// Try to acquire the SAME work thread — must be blocked
	if app.TryAcquireSession("chat_id:oc_xxx|work:om_AAA", func(error) {}) {
		t.Fatal("expected false: same work thread should block")
	}

	app.ReleaseSession("chat_id:oc_xxx|work:om_AAA")
}

func TestApp_TryAcquireSession_ThreeIndependentWorkThreads(t *testing.T) {
	app := testAppForSessionActive()

	// Acquire 3 different work threads — all should succeed
	for _, token := range []string{"om_A", "om_B", "om_C"} {
		sk := "chat_id:oc_xxx|work:" + token
		if !app.TryAcquireSession(sk, func(error) {}) {
			t.Fatalf("expected true: work thread %s should acquire independently", token)
		}
	}

	// All 3 should be active with different ThreadScope
	for _, token := range []string{"om_A", "om_B", "om_C"} {
		sk := "chat_id:oc_xxx|work:" + token
		if !app.IsSessionActive(sk) {
			t.Fatalf("expected active: work thread %s should show as active", token)
		}
	}

	// A plain chat message should NOT see these as busy
	if app.IsSessionActive("chat_id:oc_xxx") {
		t.Fatal("expected false: plain chat should not see work threads as busy")
	}

	// Release all
	for _, token := range []string{"om_A", "om_B", "om_C"} {
		app.ReleaseSession("chat_id:oc_xxx|work:" + token)
	}
}

func TestApp_TryAcquireSession_PlainChatNotBlockedByWorkThread(t *testing.T) {
	app := testAppForSessionActive()

	// Work thread is active
	if !app.TryAcquireSession("chat_id:oc_xxx|work:om_AAA", func(error) {}) {
		t.Fatal("expected true: work thread should acquire")
	}

	// Plain chat must NOT be blocked by work thread
	if !app.TryAcquireSession("chat_id:oc_xxx", func(error) {}) {
		t.Fatal("expected true: plain chat should not be blocked by work thread")
	}

	app.ReleaseSession("chat_id:oc_xxx")
	app.ReleaseSession("chat_id:oc_xxx|work:om_AAA")
}

func testAppForSessionActive() *App {
	return &App{state: newRuntimeStore()}
}
