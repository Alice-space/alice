package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

type kimiWireDriver struct {
	cfg      KimiConfig
	client   *lineRPCClient
	events   chan TurnEvent
	threadID string
	activeID string
	nextID   atomic.Uint64
	mu       sync.Mutex

	pendingAssistantParts []string
}

func newKimiWireDriver(cfg KimiConfig) *kimiWireDriver {
	return &kimiWireDriver{
		cfg:    cfg,
		events: make(chan TurnEvent, 128),
	}
}

func (d *kimiWireDriver) SteerMode() SteerMode {
	return SteerModeNative
}

func (d *kimiWireDriver) StartTurn(ctx context.Context, req RunRequest) (TurnRef, error) {
	if err := d.ensureStarted(ctx, req); err != nil {
		return TurnRef{}, err
	}
	turnID := "wire-" + strconv.FormatUint(d.nextID.Add(1), 10)
	d.mu.Lock()
	d.activeID = turnID
	threadID := d.threadID
	d.mu.Unlock()

	go d.runPrompt(turnID, req)
	return TurnRef{ThreadID: threadID, TurnID: turnID}, nil
}

func (d *kimiWireDriver) SteerTurn(ctx context.Context, _ TurnRef, req RunRequest) error {
	if d.client == nil {
		return ErrInteractiveClosed
	}
	_, err := d.client.Request(ctx, "steer", map[string]any{
		"user_input": strings.TrimSpace(req.UserText),
	})
	return err
}

func (d *kimiWireDriver) InterruptTurn(ctx context.Context, _ TurnRef) error {
	if d.client == nil {
		return nil
	}
	_, err := d.client.Request(ctx, "cancel", map[string]any{})
	return err
}

func (d *kimiWireDriver) Events() <-chan TurnEvent {
	return d.events
}

func (d *kimiWireDriver) Close() error {
	d.mu.Lock()
	client := d.client
	d.client = nil
	d.mu.Unlock()
	if client == nil {
		return nil
	}
	return client.Close()
}

func (d *kimiWireDriver) ensureStarted(ctx context.Context, req RunRequest) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.client != nil {
		return nil
	}
	command := strings.TrimSpace(d.cfg.Command)
	if command == "" {
		command = "kimi"
	}
	args := []string{"--wire", "--yolo"}
	if model := strings.TrimSpace(req.Model); model != "" {
		args = append(args, "--model", model)
	}
	if threadID := strings.TrimSpace(req.ThreadID); threadID != "" {
		args = append(args, "--session", threadID)
		d.threadID = threadID
	}
	if d.threadID == "" {
		d.threadID = "kimi-wire"
	}
	client, err := startLineRPCClient(ctx, command, args, lineRPCOptions{
		WorkspaceDir:   firstNonEmpty(req.WorkspaceDir, d.cfg.WorkspaceDir),
		BaseEnv:        d.cfg.Env,
		Env:            req.Env,
		IncludeJSONRPC: true,
		DefaultHandler: kimiDefaultServerRequestHandler,
	})
	if err != nil {
		return err
	}
	d.client = client
	if _, err := client.Request(ctx, "initialize", map[string]any{
		"protocol_version": "1.8",
		"client": map[string]any{
			"name":    "agentbridge",
			"version": "0.1.0",
		},
		"capabilities": map[string]any{
			"supports_question":  false,
			"supports_plan_mode": false,
		},
	}); err != nil {
		_ = client.Close()
		d.client = nil
		return err
	}
	go d.forwardKimiNotifications(client)
	return nil
}

func (d *kimiWireDriver) runPrompt(turnID string, req RunRequest) {
	_, err := d.client.Request(context.Background(), "prompt", map[string]any{
		"user_input": strings.TrimSpace(req.UserText),
	})
	if err != nil {
		d.events <- TurnEvent{
			Provider: ProviderKimi,
			ThreadID: d.threadID,
			TurnID:   turnID,
			Kind:     TurnEventError,
			Err:      err,
		}
	}
}

func (d *kimiWireDriver) forwardKimiNotifications(client *lineRPCClient) {
	for note := range client.Notifications() {
		for _, event := range d.parseKimiNotification(note) {
			d.events <- event
		}
	}
}

func (d *kimiWireDriver) parseKimiNotification(note rpcNotification) []TurnEvent {
	if note.Method != "event" {
		return nil
	}
	var params struct {
		Type    string         `json:"type"`
		Payload map[string]any `json:"payload"`
	}
	if err := json.Unmarshal(note.Params, &params); err != nil {
		return nil
	}
	d.mu.Lock()
	threadID := d.threadID
	turnID := d.activeID
	d.mu.Unlock()
	base := TurnEvent{Provider: ProviderKimi, ThreadID: threadID, TurnID: turnID, Raw: note.Raw}
	switch params.Type {
	case "TurnBegin":
		d.clearPendingKimiAssistantText()
		base.Kind = TurnEventStarted
		base.Text = kimiInputText(params.Payload["user_input"])
		return []TurnEvent{base}
	case "TurnEnd":
		events := make([]TurnEvent, 0, 2)
		if text := d.popPendingKimiAssistantText(); strings.TrimSpace(text) != "" {
			textEvent := base
			textEvent.Kind = TurnEventAssistantText
			textEvent.Text = text
			events = append(events, textEvent)
		}
		base.Kind = TurnEventCompleted
		if status := stringFromMap(params.Payload, "status"); status == "cancelled" {
			base.Kind = TurnEventInterrupted
		}
		events = append(events, base)
		return events
	case "ContentPart":
		if text := kimiContentText(params.Payload); strings.TrimSpace(text) != "" {
			d.appendPendingKimiAssistantText(text)
		}
		return nil
	case "ToolCall", "ToolCallPart", "ToolResult":
		base.Kind = TurnEventToolUse
		base.Text = kimiToolText(params.Type, params.Payload)
		return []TurnEvent{base}
	case "SteerInput":
		base.Kind = TurnEventSteerConsumed
		base.Text = kimiInputText(params.Payload["user_input"])
		return []TurnEvent{base}
	case "PlanDisplay":
		base.Kind = TurnEventReasoning
		base.Text = stringFromMap(params.Payload, "content")
		if strings.TrimSpace(base.Text) == "" {
			return nil
		}
		return []TurnEvent{base}
	default:
		return nil
	}
}

func (d *kimiWireDriver) appendPendingKimiAssistantText(text string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pendingAssistantParts = append(d.pendingAssistantParts, text)
}

func (d *kimiWireDriver) popPendingKimiAssistantText() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	text := strings.Join(d.pendingAssistantParts, "")
	d.pendingAssistantParts = nil
	return text
}

func (d *kimiWireDriver) clearPendingKimiAssistantText() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pendingAssistantParts = nil
}

func kimiDefaultServerRequestHandler(r rpcRequest) any {
	var params struct {
		Type    string         `json:"type"`
		Payload map[string]any `json:"payload"`
	}
	_ = json.Unmarshal(r.Params, &params)
	switch params.Type {
	case "ApprovalRequest":
		return map[string]any{
			"request_id": stringFromMap(params.Payload, "id"),
			"response":   "approve_for_session",
		}
	case "QuestionRequest":
		return map[string]any{
			"request_id": stringFromMap(params.Payload, "id"),
			"response":   "",
		}
	case "ToolCallRequest":
		return map[string]any{
			"tool_call_id": stringFromMap(params.Payload, "id"),
			"return_value": map[string]any{
				"is_error": true,
				"output":   "agentbridge has no external tool handler configured",
				"message":  "external tool unavailable",
				"display":  []any{},
			},
		}
	default:
		return map[string]any{}
	}
}

func kimiContentText(payload map[string]any) string {
	if text := stringFromMap(payload, "text"); text != "" {
		return text
	}
	if typ := stringFromMap(payload, "type"); typ != "" {
		return typ
	}
	return ""
}

func kimiToolText(eventType string, payload map[string]any) string {
	name := firstNonEmpty(stringFromMap(payload, "name"), stringFromMap(payload, "tool"), stringFromMap(payload, "sender"))
	if name == "" {
		return eventType
	}
	return fmt.Sprintf("%s %s", eventType, name)
}

func kimiInputText(raw any) string {
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value)
	case []any:
		parts := make([]string, 0, len(value))
		for _, item := range value {
			m, _ := item.(map[string]any)
			if text := stringFromMap(m, "text"); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	default:
		return ""
	}
}
