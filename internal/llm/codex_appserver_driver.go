package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
)

type codexAppServerDriver struct {
	cfg      CodexConfig
	client   *lineRPCClient
	events   chan TurnEvent
	threadID string
	mu       sync.Mutex
}

func newCodexAppServerDriver(cfg CodexConfig) *codexAppServerDriver {
	return &codexAppServerDriver{
		cfg:    cfg,
		events: make(chan TurnEvent, 128),
	}
}

func (d *codexAppServerDriver) SteerMode() SteerMode {
	return SteerModeNative
}

func (d *codexAppServerDriver) StartTurn(ctx context.Context, req RunRequest) (TurnRef, error) {
	if err := d.ensureStarted(ctx, req); err != nil {
		return TurnRef{}, err
	}
	threadID, err := d.ensureThread(ctx, req)
	if err != nil {
		return TurnRef{}, err
	}

	params := map[string]any{
		"threadId": threadID,
		"input":    codexTextInput(req.UserText),
	}
	if model := strings.TrimSpace(req.Model); model != "" {
		params["model"] = model
	}
	if effort := strings.TrimSpace(req.ReasoningEffort); effort != "" {
		params["effort"] = effort
	}
	if personality := strings.TrimSpace(req.Personality); personality != "" {
		params["personality"] = personality
	}
	if cwd := firstNonEmpty(req.WorkspaceDir, d.cfg.WorkspaceDir); cwd != "" {
		params["cwd"] = cwd
	}
	policy := mergeCoreCodexExecPolicy(toCoreCodexExecPolicy(d.cfg.DefaultExecPolicy), toCoreCodexExecPolicy(req.ExecPolicy))
	if approval := strings.TrimSpace(policy.AskForApproval); approval != "" {
		params["approvalPolicy"] = approval
	}

	raw, err := d.client.Request(ctx, "turn/start", params)
	if err != nil {
		return TurnRef{}, err
	}
	turnID := jsonStringAt(raw, "turn", "id")
	if turnID == "" {
		return TurnRef{}, fmt.Errorf("codex app-server turn/start returned no turn id")
	}
	return TurnRef{ThreadID: threadID, TurnID: turnID}, nil
}

func (d *codexAppServerDriver) SteerTurn(ctx context.Context, turn TurnRef, req RunRequest) error {
	if d.client == nil {
		return ErrInteractiveClosed
	}
	_, err := d.client.Request(ctx, "turn/steer", map[string]any{
		"threadId":       strings.TrimSpace(turn.ThreadID),
		"expectedTurnId": strings.TrimSpace(turn.TurnID),
		"input":          codexTextInput(req.UserText),
	})
	return err
}

func (d *codexAppServerDriver) InterruptTurn(ctx context.Context, turn TurnRef) error {
	if d.client == nil {
		return nil
	}
	_, err := d.client.Request(ctx, "turn/interrupt", map[string]any{
		"threadId": strings.TrimSpace(turn.ThreadID),
		"turnId":   strings.TrimSpace(turn.TurnID),
	})
	return err
}

func (d *codexAppServerDriver) Events() <-chan TurnEvent {
	return d.events
}

func (d *codexAppServerDriver) Close() error {
	d.mu.Lock()
	client := d.client
	d.client = nil
	d.mu.Unlock()
	if client == nil {
		return nil
	}
	return client.Close()
}

func (d *codexAppServerDriver) ensureStarted(ctx context.Context, req RunRequest) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.client != nil {
		return nil
	}
	command := strings.TrimSpace(d.cfg.Command)
	if command == "" {
		command = "codex"
	}
	client, err := startLineRPCClient(ctx, command, []string{"app-server", "--listen", "stdio://"}, lineRPCOptions{
		WorkspaceDir:   firstNonEmpty(req.WorkspaceDir, d.cfg.WorkspaceDir),
		BaseEnv:        d.cfg.Env,
		Env:            req.Env,
		DefaultHandler: codexDefaultServerRequestHandler,
	})
	if err != nil {
		return err
	}
	d.client = client
	if _, err := client.Request(ctx, "initialize", map[string]any{
		"clientInfo": map[string]any{
			"name":    "agentbridge",
			"title":   "agentbridge",
			"version": "0.1.0",
		},
		"capabilities": map[string]any{"experimentalApi": true},
	}); err != nil {
		_ = client.Close()
		d.client = nil
		return err
	}
	if err := client.Notify("initialized", nil); err != nil {
		_ = client.Close()
		d.client = nil
		return err
	}
	go d.forwardCodexNotifications(client)
	return nil
}

func (d *codexAppServerDriver) ensureThread(ctx context.Context, req RunRequest) (string, error) {
	d.mu.Lock()
	if d.threadID != "" {
		threadID := d.threadID
		d.mu.Unlock()
		return threadID, nil
	}
	d.mu.Unlock()

	threadID := strings.TrimSpace(req.ThreadID)
	method := "thread/start"
	params := map[string]any{}
	if threadID != "" {
		method = "thread/resume"
		params["threadId"] = threadID
	}
	if model := strings.TrimSpace(req.Model); model != "" {
		params["model"] = model
	}
	if cwd := firstNonEmpty(req.WorkspaceDir, d.cfg.WorkspaceDir); cwd != "" {
		params["cwd"] = cwd
	}
	if approval := strings.TrimSpace(d.cfg.DefaultExecPolicy.AskForApproval); approval != "" {
		params["approvalPolicy"] = approval
	}
	if sandbox := strings.TrimSpace(d.cfg.DefaultExecPolicy.Sandbox); sandbox != "" {
		params["sandbox"] = sandbox
	}
	raw, err := d.client.Request(ctx, method, params)
	if err != nil {
		return "", err
	}
	if threadID == "" {
		threadID = jsonStringAt(raw, "thread", "id")
	}
	if threadID == "" {
		return "", errors.New("codex app-server returned no thread id")
	}
	d.mu.Lock()
	d.threadID = threadID
	d.mu.Unlock()
	return threadID, nil
}

func (d *codexAppServerDriver) forwardCodexNotifications(client *lineRPCClient) {
	defer close(d.events)
	for note := range client.Notifications() {
		event, ok := parseCodexNotification(note)
		if !ok {
			continue
		}
		d.events <- event
	}
}

func parseCodexNotification(note rpcNotification) (TurnEvent, bool) {
	var params map[string]any
	if len(note.Params) > 0 {
		_ = json.Unmarshal(note.Params, &params)
	}
	threadID := stringFromMap(params, "threadId")
	turnID := stringFromMap(params, "turnId")
	switch note.Method {
	case "turn/started":
		if turn, _ := params["turn"].(map[string]any); turnID == "" {
			turnID = stringFromMap(turn, "id")
		}
		return TurnEvent{Provider: ProviderCodex, ThreadID: threadID, TurnID: turnID, Kind: TurnEventStarted, Raw: note.Raw}, true
	case "item/agentMessage/delta":
		return TurnEvent{}, false
	case "item/completed":
		item, _ := params["item"].(map[string]any)
		switch stringFromMap(item, "type") {
		case "agentMessage":
			return TurnEvent{Provider: ProviderCodex, ThreadID: threadID, TurnID: turnID, Kind: TurnEventAssistantText, Text: stringFromMap(item, "text"), Raw: note.Raw}, true
		case "reasoning":
			return TurnEvent{Provider: ProviderCodex, ThreadID: threadID, TurnID: turnID, Kind: TurnEventReasoning, Text: strings.Join(stringSliceFromMap(item, "summary"), "\n"), Raw: note.Raw}, true
		case "fileChange":
			return TurnEvent{Provider: ProviderCodex, ThreadID: threadID, TurnID: turnID, Kind: TurnEventFileChange, Text: "file_change", Raw: note.Raw}, true
		case "commandExecution", "mcpToolCall", "dynamicToolCall":
			return TurnEvent{Provider: ProviderCodex, ThreadID: threadID, TurnID: turnID, Kind: TurnEventToolUse, Text: stringFromMap(item, "command"), Raw: note.Raw}, true
		}
	case "turn/completed":
		turn, _ := params["turn"].(map[string]any)
		if turnID == "" {
			turnID = stringFromMap(turn, "id")
		}
		status := stringFromMap(turn, "status")
		kind := TurnEventCompleted
		if status == "interrupted" {
			kind = TurnEventInterrupted
		}
		if status == "failed" {
			kind = TurnEventError
		}
		return TurnEvent{Provider: ProviderCodex, ThreadID: threadID, TurnID: turnID, Kind: kind, Raw: note.Raw}, true
	}
	return TurnEvent{}, false
}

func codexTextInput(text string) []map[string]any {
	return []map[string]any{{
		"type":          "text",
		"text":          strings.TrimSpace(text),
		"text_elements": []any{},
	}}
}

func codexDefaultServerRequestHandler(r rpcRequest) any {
	switch r.Method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		return map[string]any{"decision": "accept"}
	}
	return map[string]any{}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func jsonStringAt(raw json.RawMessage, path ...string) string {
	var current any
	if err := json.Unmarshal(raw, &current); err != nil {
		return ""
	}
	for _, key := range path {
		m, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = m[key]
	}
	value, _ := current.(string)
	return strings.TrimSpace(value)
}

func stringFromMap(m map[string]any, key string) string {
	if len(m) == 0 {
		return ""
	}
	value, _ := m[key].(string)
	return strings.TrimSpace(value)
}

func stringSliceFromMap(m map[string]any, key string) []string {
	raw, _ := m[key].([]any)
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}
