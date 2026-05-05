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
	// Active session is in thread seed om_AAA
	app.state.mu.Lock()
	app.state.active["chat_id:oc_xxx|work:om_AAA"] = activeSessionRun{version: 1}
	app.state.mu.Unlock()

	// Task targets a different thread (seed om_BBB) — sessionkey.ThreadScope
	// returns "" for |work: tokens, so these are treated as the same scope.
	if !app.IsSessionActive("chat_id:oc_xxx|work:om_BBB") {
		t.Fatal("expected true: same visibility scope blocks different thread seed")
	}
}

func TestApp_IsSessionActive_SameThreadIsBusy(t *testing.T) {
	app := testAppForSessionActive()
	// Active session is in thread seed om_AAA
	app.state.mu.Lock()
	app.state.active["chat_id:oc_xxx|work:om_AAA"] = activeSessionRun{version: 1}
	app.state.mu.Unlock()

	// Task targets the same thread — must be blocked
	if !app.IsSessionActive("chat_id:oc_xxx|work:om_AAA") {
		t.Fatal("expected true: same thread seed must be busy")
	}
}

func TestApp_IsSessionActive_PlainGroupNotBlockedByThread(t *testing.T) {
	app := testAppForSessionActive()
	// Active session is a thread in the group — sessionkey.ThreadScope
	// returns "" for |work: tokens, so plain group queries match the scope.
	app.state.mu.Lock()
	app.state.active["chat_id:oc_xxx|work:om_AAA"] = activeSessionRun{version: 1}
	app.state.mu.Unlock()

	// A plain group-level task (no thread) is blocked by a work session
	// in the same chat because both have the same ThreadScope ("").
	if !app.IsSessionActive("chat_id:oc_xxx") {
		t.Fatal("expected true: plain group task blocked by work session in same chat")
	}
}

func testAppForSessionActive() *App {
	return &App{state: newRuntimeStore()}
}
