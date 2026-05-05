package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
)

type claudeStreamDriver struct {
	cfg    ClaudeConfig
	events chan TurnEvent

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stderr bytes.Buffer

	mu        sync.Mutex
	threadID  string
	activeID  string
	closed    bool
	nextID    atomic.Uint64
	requestID atomic.Uint64
	writeMu   sync.Mutex
	closeOnce sync.Once
}

func newClaudeStreamDriver(cfg ClaudeConfig) *claudeStreamDriver {
	return &claudeStreamDriver{
		cfg:    cfg,
		events: make(chan TurnEvent, 128),
	}
}

func (d *claudeStreamDriver) SteerMode() SteerMode {
	return SteerModeNativeEnqueue
}

func (d *claudeStreamDriver) StartTurn(ctx context.Context, req RunRequest) (TurnRef, error) {
	if err := d.ensureStarted(ctx, req); err != nil {
		return TurnRef{}, err
	}
	turnID := "claude-" + fmt.Sprint(d.nextID.Add(1))
	d.mu.Lock()
	d.activeID = turnID
	threadID := firstNonEmpty(d.threadID, req.ThreadID)
	d.mu.Unlock()

	if err := d.writeUserMessage(req.UserText); err != nil {
		return TurnRef{}, err
	}
	d.emit(TurnEvent{Provider: ProviderClaude, ThreadID: threadID, TurnID: turnID, Kind: TurnEventStarted})
	return TurnRef{ThreadID: threadID, TurnID: turnID}, nil
}

func (d *claudeStreamDriver) SteerTurn(_ context.Context, turn TurnRef, req RunRequest) error {
	if err := d.writeUserMessage(req.UserText); err != nil {
		return err
	}
	d.emit(TurnEvent{
		Provider: ProviderClaude,
		ThreadID: firstNonEmpty(turn.ThreadID, d.currentThreadID()),
		TurnID:   strings.TrimSpace(turn.TurnID),
		Kind:     TurnEventSteerConsumed,
		Text:     strings.TrimSpace(req.UserText),
	})
	return nil
}

func (d *claudeStreamDriver) InterruptTurn(_ context.Context, turn TurnRef) error {
	requestID := fmt.Sprintf("agentbridge-%d", d.requestID.Add(1))
	return d.writeJSONLine(map[string]any{
		"type":       "control_request",
		"request_id": requestID,
		"request": map[string]any{
			"subtype": "interrupt",
		},
	})
}

func (d *claudeStreamDriver) Events() <-chan TurnEvent {
	return d.events
}

func (d *claudeStreamDriver) Close() error {
	var err error
	d.closeOnce.Do(func() {
		d.mu.Lock()
		d.closed = true
		stdin := d.stdin
		cmd := d.cmd
		d.stdin = nil
		d.cmd = nil
		d.mu.Unlock()
		if stdin != nil {
			_ = stdin.Close()
		}
		if cmd != nil && cmd.Process != nil {
			_ = cmd.Process.Kill()
			err = cmd.Wait()
		}
		close(d.events)
	})
	return err
}

func (d *claudeStreamDriver) ensureStarted(ctx context.Context, req RunRequest) error {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return ErrInteractiveClosed
	}
	if d.cmd != nil {
		d.mu.Unlock()
		return nil
	}
	d.mu.Unlock()

	command := strings.TrimSpace(d.cfg.Command)
	if command == "" {
		command = "claude"
	}
	args := buildClaudeStreamArgs(req)
	cmd := exec.Command(command, args...)
	if cwd := firstNonEmpty(req.WorkspaceDir, d.cfg.WorkspaceDir); cwd != "" {
		cmd.Dir = cwd
	}
	cmd.Env = mergeProcessEnv(mergeProcessEnv(os.Environ(), d.cfg.Env), req.Env)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("create claude stdin pipe failed: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("create claude stdout pipe failed: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("create claude stderr pipe failed: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start claude stream-json failed: %w", err)
	}

	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return ErrInteractiveClosed
	}
	d.cmd = cmd
	d.stdin = stdin
	if threadID := strings.TrimSpace(req.ThreadID); threadID != "" {
		d.threadID = threadID
	}
	d.mu.Unlock()

	go d.readClaudeStdout(stdout)
	go func() {
		_, _ = io.Copy(&d.stderr, stderr)
	}()
	return nil
}

func buildClaudeStreamArgs(req RunRequest) []string {
	args := []string{
		"-p",
		"--input-format",
		"stream-json",
		"--output-format",
		"stream-json",
		"--verbose",
		"--permission-mode",
		"bypassPermissions",
	}
	if model := strings.TrimSpace(req.Model); model != "" {
		args = append(args, "--model", model)
	}
	if threadID := strings.TrimSpace(req.ThreadID); threadID != "" {
		args = append(args, "--resume", threadID)
	}
	return args
}

func (d *claudeStreamDriver) writeUserMessage(text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("empty prompt")
	}
	return d.writeJSONLine(map[string]any{
		"type":       "user",
		"session_id": "",
		"message": map[string]any{
			"role": "user",
			"content": []map[string]any{{
				"type": "text",
				"text": text,
			}},
		},
		"parent_tool_use_id": nil,
	})
}

func (d *claudeStreamDriver) writeJSONLine(payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	d.mu.Lock()
	closed := d.closed
	stdin := d.stdin
	d.mu.Unlock()
	if closed || stdin == nil {
		return ErrInteractiveClosed
	}
	_, err = stdin.Write(append(raw, '\n'))
	return err
}

func (d *claudeStreamDriver) readClaudeStdout(stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		event, ok := d.parseClaudeLine(scanner.Text())
		if !ok {
			continue
		}
		d.emit(event)
	}
	if err := scanner.Err(); err != nil {
		d.emit(TurnEvent{Provider: ProviderClaude, ThreadID: d.currentThreadID(), TurnID: d.currentTurnID(), Kind: TurnEventError, Err: err})
	}
	d.markClosedFromReader()
}

func (d *claudeStreamDriver) parseClaudeLine(line string) (TurnEvent, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return TurnEvent{}, false
	}
	var msg map[string]any
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		return TurnEvent{}, false
	}
	eventType := stringFromMap(msg, "type")
	sessionID := stringFromMap(msg, "session_id")
	if sessionID != "" {
		d.mu.Lock()
		d.threadID = sessionID
		d.mu.Unlock()
	}
	threadID := firstNonEmpty(sessionID, d.currentThreadID())
	turnID := d.currentTurnID()
	base := TurnEvent{Provider: ProviderClaude, ThreadID: threadID, TurnID: turnID, Raw: line}
	switch eventType {
	case "system":
		return TurnEvent{}, false
	case "assistant":
		message, _ := msg["message"].(map[string]any)
		if text := extractClaudeAssistantText(message); text != "" {
			base.Kind = TurnEventAssistantText
			base.Text = text
			return base, true
		}
		if tool := extractClaudeToolUse(message); tool != "" {
			base.Kind = TurnEventToolUse
			base.Text = tool
			return base, true
		}
	case "result":
		base.Usage = Usage{
			InputTokens:       int64FromClaudeMap(msg, "usage", "input_tokens"),
			CachedInputTokens: int64FromClaudeMap(msg, "usage", "cache_read_input_tokens"),
			OutputTokens:      int64FromClaudeMap(msg, "usage", "output_tokens"),
		}
		if boolFromAny(msg["is_error"]) {
			base.Kind = TurnEventError
			base.Err = errors.New(firstNonEmpty(stringFromMap(msg, "result"), strings.Join(stringSliceFromMap(msg, "errors"), "\n"), "claude result error"))
			return base, true
		}
		base.Kind = TurnEventCompleted
		return base, true
	}
	return TurnEvent{}, false
}

func (d *claudeStreamDriver) markClosedFromReader() {
	d.closeOnce.Do(func() {
		d.mu.Lock()
		d.closed = true
		d.stdin = nil
		d.cmd = nil
		d.mu.Unlock()
		close(d.events)
	})
}

func (d *claudeStreamDriver) currentThreadID() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.threadID
}

func (d *claudeStreamDriver) currentTurnID() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.activeID
}

func (d *claudeStreamDriver) emit(event TurnEvent) {
	d.mu.Lock()
	closed := d.closed
	d.mu.Unlock()
	if closed {
		return
	}
	select {
	case d.events <- event:
	default:
	}
}

func extractClaudeAssistantText(message map[string]any) string {
	if len(message) == 0 {
		return ""
	}
	content, ok := message["content"].([]any)
	if !ok {
		return stringFromMap(message, "text")
	}
	parts := make([]string, 0, len(content))
	for _, raw := range content {
		block, _ := raw.(map[string]any)
		if strings.ToLower(stringFromMap(block, "type")) != "text" {
			continue
		}
		if text := stringFromMap(block, "text"); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func extractClaudeToolUse(message map[string]any) string {
	if len(message) == 0 {
		return ""
	}
	content, ok := message["content"].([]any)
	if !ok {
		return ""
	}
	tools := make([]string, 0, len(content))
	for _, raw := range content {
		block, _ := raw.(map[string]any)
		if strings.ToLower(stringFromMap(block, "type")) != "tool_use" {
			continue
		}
		name := stringFromMap(block, "name")
		id := stringFromMap(block, "id")
		detail := "tool_use"
		if name != "" {
			detail += " name=`" + name + "`"
		}
		if id != "" {
			detail += " id=`" + id + "`"
		}
		tools = append(tools, detail)
	}
	return strings.Join(tools, "; ")
}

func int64FromClaudeMap(payload map[string]any, path ...string) int64 {
	var current any = payload
	for _, key := range path {
		m, ok := current.(map[string]any)
		if !ok {
			return 0
		}
		current = m[key]
	}
	switch value := current.(type) {
	case int:
		return int64(value)
	case int64:
		return value
	case float64:
		return int64(value)
	default:
		return 0
	}
}

func boolFromAny(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		v = strings.ToLower(strings.TrimSpace(v))
		return v == "true" || v == "1" || v == "yes"
	default:
		return false
	}
}
