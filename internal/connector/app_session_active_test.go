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

func testAppForSessionActive() *App {
	return &App{state: newRuntimeStore()}
}
