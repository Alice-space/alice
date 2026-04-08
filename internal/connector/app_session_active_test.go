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
	app.state.active["chat_id:oc_xxx|scene:chat|reset:123"] = activeSessionRun{version: 1}
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

func testAppForSessionActive() *App {
	return &App{state: newRuntimeStore()}
}
