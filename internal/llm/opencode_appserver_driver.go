package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/Alice-space/alice/internal/llm/internal/shared"
	"github.com/Alice-space/alice/internal/logging"
)

type openCodeAppServerDriver struct {
	cfg    OpenCodeConfig
	client *http.Client
	events chan TurnEvent

	cmd    *exec.Cmd
	stderr bytes.Buffer

	mu                sync.Mutex
	baseURL           string
	sessionID         string
	activeID          string
	activeCompleted   bool
	lastAssistantText string
	messageRoles      map[string]string
	closed            bool
	eventCancel       context.CancelFunc
	nextID            atomic.Uint64
	closeOnce         sync.Once
}

func newOpenCodeAppServerDriver(cfg OpenCodeConfig) *openCodeAppServerDriver {
	return &openCodeAppServerDriver{
		cfg:          cfg,
		client:       &http.Client{},
		events:       make(chan TurnEvent, 128),
		messageRoles: make(map[string]string),
	}
}

func (d *openCodeAppServerDriver) SteerMode() SteerMode {
	return SteerModeNativeEnqueue
}

func (d *openCodeAppServerDriver) StartTurn(ctx context.Context, req RunRequest) (TurnRef, error) {
	if err := d.ensureServer(ctx, req); err != nil {
		return TurnRef{}, err
	}
	d.reconnectEventStream()
	sessionID, err := d.ensureSession(ctx, req)
	if err != nil {
		return TurnRef{}, err
	}
	turnID := "opencode-" + fmt.Sprint(d.nextID.Add(1))
	d.mu.Lock()
	d.activeID = turnID
	d.activeCompleted = false
	d.lastAssistantText = ""
	d.mu.Unlock()

	turn := TurnRef{ThreadID: sessionID, TurnID: turnID}
	d.emit(TurnEvent{Provider: ProviderOpenCode, ThreadID: sessionID, TurnID: turnID, Kind: TurnEventStarted})
	go d.runPrompt(ctx, turn, req)
	return turn, nil
}

func (d *openCodeAppServerDriver) SteerTurn(ctx context.Context, turn TurnRef, req RunRequest) error {
	sessionID := firstNonEmpty(turn.ThreadID, d.currentSessionID())
	if sessionID == "" {
		return errors.New("opencode app-server has no active session")
	}
	if err := d.postNoContent(ctx, "/session/"+url.PathEscape(sessionID)+"/prompt_async", d.promptBody(req), req); err != nil {
		return err
	}
	d.emit(TurnEvent{
		Provider: ProviderOpenCode,
		ThreadID: sessionID,
		TurnID:   strings.TrimSpace(turn.TurnID),
		Kind:     TurnEventSteerConsumed,
		Text:     strings.TrimSpace(req.UserText),
	})
	return nil
}

func (d *openCodeAppServerDriver) InterruptTurn(ctx context.Context, turn TurnRef) error {
	sessionID := firstNonEmpty(turn.ThreadID, d.currentSessionID())
	if sessionID == "" {
		return nil
	}
	return d.postNoContent(ctx, "/session/"+url.PathEscape(sessionID)+"/abort", nil, RunRequest{})
}

func (d *openCodeAppServerDriver) Events() <-chan TurnEvent {
	return d.events
}

func (d *openCodeAppServerDriver) Close() error {
	var err error
	d.closeOnce.Do(func() {
		d.mu.Lock()
		d.closed = true
		cmd := d.cmd
		d.cmd = nil
		cancelEvents := d.eventCancel
		d.eventCancel = nil
		d.mu.Unlock()
		if cmd != nil && cmd.Process != nil {
			_ = cmd.Process.Kill()
			err = cmd.Wait()
		}
		if cancelEvents != nil {
			cancelEvents()
		}
		close(d.events)
	})
	return err
}

func (d *openCodeAppServerDriver) ensureServer(ctx context.Context, req RunRequest) error {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return ErrInteractiveClosed
	}
	if d.baseURL != "" {
		d.mu.Unlock()
		return nil
	}
	if serverURL := strings.TrimSpace(d.cfg.ServerURL); serverURL != "" {
		d.baseURL = strings.TrimRight(serverURL, "/")
		d.mu.Unlock()
		d.ensureEventStream()
		return nil
	}
	d.mu.Unlock()

	command := strings.TrimSpace(d.cfg.Command)
	if command == "" {
		command = "opencode"
	}
	cmd := exec.Command(command, "serve", "--hostname", "127.0.0.1", "--port", "0")
	if cwd := firstNonEmpty(req.WorkspaceDir, d.cfg.WorkspaceDir); cwd != "" {
		cmd.Dir = cwd
	}
	cmd.Env = mergeProcessEnv(mergeProcessEnv(os.Environ(), d.cfg.Env), req.Env)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("create opencode stdout pipe failed: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("create opencode stderr pipe failed: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start opencode serve failed: %w", err)
	}

	urlCh := make(chan string, 1)
	go d.scanOpenCodeServeOutput(stdout, urlCh)
	go func() {
		_, _ = io.Copy(&d.stderr, stderr)
	}()

	select {
	case serverURL := <-urlCh:
		if serverURL == "" {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return fmt.Errorf("opencode serve exited before reporting URL: %s", strings.TrimSpace(d.stderr.String()))
		}
		d.mu.Lock()
		if d.closed {
			d.mu.Unlock()
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return ErrInteractiveClosed
		}
		d.cmd = cmd
		d.baseURL = strings.TrimRight(serverURL, "/")
		d.mu.Unlock()
		d.ensureEventStream()
		return nil
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return ctx.Err()
	}
}

func (d *openCodeAppServerDriver) scanOpenCodeServeOutput(stdout io.Reader, urlCh chan<- string) {
	defer close(urlCh)
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		if serverURL := extractHTTPURL(line); serverURL != "" {
			urlCh <- serverURL
			return
		}
	}
	urlCh <- ""
}

func (d *openCodeAppServerDriver) ensureSession(ctx context.Context, req RunRequest) (string, error) {
	d.mu.Lock()
	if d.sessionID != "" {
		sessionID := d.sessionID
		d.mu.Unlock()
		return sessionID, nil
	}
	d.mu.Unlock()

	sessionID := strings.TrimSpace(req.ThreadID)
	if sessionID == "" {
		var created struct {
			ID string `json:"id"`
		}
		if err := d.postJSON(ctx, "/session", map[string]any{}, req, &created); err != nil {
			return "", err
		}
		sessionID = strings.TrimSpace(created.ID)
	}
	if sessionID == "" {
		return "", errors.New("opencode app-server returned no session id")
	}
	d.mu.Lock()
	if d.sessionID == "" {
		d.sessionID = sessionID
	}
	sessionID = d.sessionID
	d.mu.Unlock()
	return sessionID, nil
}

func (d *openCodeAppServerDriver) runPrompt(ctx context.Context, turn TurnRef, req RunRequest) {
	if err := d.postNoContent(ctx, "/session/"+url.PathEscape(turn.ThreadID)+"/prompt_async", d.promptBody(req), req); err != nil {
		kind := TurnEventError
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			kind = TurnEventInterrupted
		}
		d.markOpenCodeTurnCompleted()
		d.emit(TurnEvent{Provider: ProviderOpenCode, ThreadID: turn.ThreadID, TurnID: turn.TurnID, Kind: kind, Err: err})
		return
	}
}

func (d *openCodeAppServerDriver) promptBody(req RunRequest) map[string]any {
	body := map[string]any{
		"parts": []map[string]any{{
			"type": "text",
			"text": strings.TrimSpace(req.UserText),
		}},
	}
	if model := firstNonEmpty(req.Model, d.cfg.Model); model != "" {
		providerID, modelID := splitOpenCodeModel(model)
		if providerID != "" && modelID != "" {
			body["model"] = map[string]any{"providerID": providerID, "modelID": modelID}
		}
	}
	if variant := firstNonEmpty(req.Variant, d.cfg.Variant); variant != "" {
		body["variant"] = variant
	}
	if agent := strings.TrimSpace(req.Profile); agent != "" {
		resolvedAgent := agent
		if override, ok := d.cfg.ProfileOverrides[agent]; ok && strings.TrimSpace(override.ProviderProfile) != "" {
			resolvedAgent = strings.TrimSpace(override.ProviderProfile)
		}
		if isKnownOpenCodeAgent(resolvedAgent) {
			body["agent"] = resolvedAgent
		}
	}
	return body
}

func (d *openCodeAppServerDriver) postNoContent(ctx context.Context, path string, body any, req RunRequest) error {
	return d.postJSON(ctx, path, body, req, nil)
}

func (d *openCodeAppServerDriver) postJSON(ctx context.Context, path string, body any, req RunRequest, out any) error {
	endpoint, err := d.endpoint(path, req)
	if err != nil {
		return err
	}
	var payload io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(raw)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, payload)
	if err != nil {
		return err
	}
	if body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	resp, err := d.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("opencode app-server %s failed status=%d body=%s", path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			d.resetServerForNextRequest()
		}
		msg := fmt.Sprintf("decode opencode app-server %s response failed: %v", path, err)
		if stderr := strings.TrimSpace(d.stderr.String()); stderr != "" {
			msg += " (server stderr: " + stderr + ")"
		}
		return errors.New(msg)
	}
	return nil
}

func (d *openCodeAppServerDriver) ensureEventStream() {
	d.mu.Lock()
	if d.closed || d.eventCancel != nil || d.baseURL == "" {
		d.mu.Unlock()
		return
	}
	baseURL := d.baseURL
	ctx, cancel := context.WithCancel(context.TODO())
	d.eventCancel = cancel
	d.mu.Unlock()

	go d.readEventStream(ctx, strings.TrimRight(baseURL, "/")+"/event")
}

func (d *openCodeAppServerDriver) reconnectEventStream() {
	d.mu.Lock()
	if d.eventCancel != nil {
		d.eventCancel()
		d.eventCancel = nil
	}
	if d.closed || d.baseURL == "" {
		d.mu.Unlock()
		return
	}
	d.mu.Unlock()
	d.ensureEventStream()
}

func (d *openCodeAppServerDriver) readEventStream(ctx context.Context, endpoint string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := d.client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return
	}
	d.scanEventStream(resp.Body)
}

func (d *openCodeAppServerDriver) scanEventStream(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, shared.DefaultScannerBuf), shared.MaxScannerTokenSize2MB)
	var data []string
	flush := func() {
		if len(data) == 0 {
			return
		}
		payload := strings.Join(data, "\n")
		data = data[:0]
		event, ok := d.parseOpenCodeEvent(payload)
		if !ok {
			return
		}
		d.emit(event)
	}
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		if value, ok := strings.CutPrefix(line, "data:"); ok {
			data = append(data, strings.TrimPrefix(value, " "))
		}
	}
	flush()
}

func (d *openCodeAppServerDriver) parseOpenCodeEvent(payload string) (TurnEvent, bool) {
	var event map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(payload)), &event); err != nil {
		return TurnEvent{}, false
	}
	eventType := stringFromMap(event, "type")
	properties, _ := event["properties"].(map[string]any)
	if len(properties) == 0 {
		return TurnEvent{}, false
	}

	if extra := openCodeEventSummary(eventType, properties); extra != "" {
		logging.Debugf("opencode sse event type=%s session=%s %s", eventType, stringFromMap(properties, "sessionID"), extra)
	} else {
		logging.Debugf("opencode sse event type=%s session=%s", eventType, stringFromMap(properties, "sessionID"))
	}

	switch eventType {
	case "message.updated":
		info, _ := properties["info"].(map[string]any)
		sessionID := firstNonEmpty(stringFromMap(properties, "sessionID"), stringFromMap(info, "sessionID"))
		if !d.openCodeEventBelongsToActiveTurn(sessionID) {
			return TurnEvent{}, false
		}
		role := stringFromMap(info, "role")
		d.recordOpenCodeMessageRole(stringFromMap(info, "id"), role)
		if strings.ToLower(role) != "assistant" {
			return TurnEvent{}, false
		}
		assistantInfo := openCodeAssistantInfoFromMap(info)
		if assistantInfo.Error != nil {
			d.markOpenCodeTurnCompleted()
			return TurnEvent{
				Provider: ProviderOpenCode,
				ThreadID: sessionID,
				TurnID:   d.currentTurnID(),
				Kind:     TurnEventError,
				Err:      fmt.Errorf("opencode turn failed: %s", assistantInfo.Message()),
				Usage:    assistantInfo.Usage(),
				Raw:      payload,
			}, true
		}
		if assistantInfo.TurnFinished() {
			d.markOpenCodeTurnCompleted()
			return TurnEvent{
				Provider: ProviderOpenCode,
				ThreadID: sessionID,
				TurnID:   d.currentTurnID(),
				Kind:     TurnEventCompleted,
				Usage:    assistantInfo.Usage(),
				Raw:      payload,
			}, true
		}
		return TurnEvent{}, false
	case "message.part.updated":
		part, _ := properties["part"].(map[string]any)
		if len(part) == 0 {
			return TurnEvent{}, false
		}
		sessionID := firstNonEmpty(stringFromMap(properties, "sessionID"), stringFromMap(part, "sessionID"))
		if !d.openCodeEventBelongsToActiveTurn(sessionID) {
			return TurnEvent{}, false
		}
		turnID := d.currentTurnID()
		switch stringFromMap(part, "type") {
		case "text":
			if boolFromAny(part["ignored"]) {
				return TurnEvent{}, false
			}
			text := stringFromMap(part, "text")
			if text == "" || !openCodePartIsComplete(part) {
				return TurnEvent{}, false
			}
			kind := TurnEventAssistantText
			if d.openCodePartRole(properties, part) == "user" {
				kind = TurnEventUserText
			}
			return d.openCodeTextEvent(sessionID, turnID, kind, text, payload)
		case "reasoning":
			text := stringFromMap(part, "text")
			if text == "" {
				return TurnEvent{}, false
			}
			return TurnEvent{Provider: ProviderOpenCode, ThreadID: sessionID, TurnID: turnID, Kind: TurnEventReasoning, Text: text, Raw: payload}, true
		case "tool":
			return TurnEvent{Provider: ProviderOpenCode, ThreadID: sessionID, TurnID: turnID, Kind: TurnEventToolUse, Text: formatOpenCodeAppServerToolUse(part), Raw: payload}, true
		}
	case "message.part.delta":
		return TurnEvent{}, false
	case "session.idle":
		sessionID := stringFromMap(properties, "sessionID")
		if !d.openCodeEventBelongsToActiveTurn(sessionID) {
			return TurnEvent{}, false
		}
		if !d.hasOpenCodeAssistantText() {
			return TurnEvent{}, false
		}
		d.markOpenCodeTurnCompleted()
		return TurnEvent{
			Provider: ProviderOpenCode,
			ThreadID: sessionID,
			TurnID:   d.currentTurnID(),
			Kind:     TurnEventCompleted,
			Raw:      payload,
		}, true
	}
	return TurnEvent{}, false
}

func openCodeEventSummary(eventType string, properties map[string]any) string {
	switch eventType {
	case "message.updated":
		info, _ := properties["info"].(map[string]any)
		parts := []string{"role=" + stringFromMap(info, "role")}
		if finish := stringFromMap(info, "finish"); finish != "" {
			parts = append(parts, "finish="+finish)
		}
		return strings.Join(parts, " ")
	case "message.part.updated":
		part, _ := properties["part"].(map[string]any)
		partType := stringFromMap(part, "type")
		parts := []string{"part=" + partType}
		switch partType {
		case "text", "reasoning":
			text := stringFromMap(part, "text")
			if len(text) > 100 {
				text = text[:100]
			}
			if text != "" {
				parts = append(parts, fmt.Sprintf("text=%q", text))
			}
		case "tool":
			if tool := stringFromMap(part, "tool"); tool != "" {
				parts = append(parts, "tool="+tool)
			}
		}
		return strings.Join(parts, " ")
	case "message.part.delta":
		part, _ := properties["part"].(map[string]any)
		text := stringFromMap(part, "text")
		if len(text) > 100 {
			text = text[:100]
		}
		if text != "" {
			return "text=" + fmt.Sprintf("%q", text)
		}
	}
	return ""
}

func (d *openCodeAppServerDriver) endpoint(path string, req RunRequest) (string, error) {
	d.mu.Lock()
	base := d.baseURL
	d.mu.Unlock()
	if base == "" {
		return "", errors.New("opencode app-server is not started")
	}
	u, err := url.Parse(base + path)
	if err != nil {
		return "", err
	}
	if directory := firstNonEmpty(req.WorkspaceDir, d.cfg.WorkspaceDir); directory != "" {
		q := u.Query()
		q.Set("directory", directory)
		u.RawQuery = q.Encode()
	}
	return u.String(), nil
}

func (d *openCodeAppServerDriver) currentSessionID() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.sessionID
}

func (d *openCodeAppServerDriver) currentTurnID() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.activeID
}

func (d *openCodeAppServerDriver) openCodeEventBelongsToActiveTurn(sessionID string) bool {
	sessionID = strings.TrimSpace(sessionID)
	d.mu.Lock()
	defer d.mu.Unlock()
	return !d.closed &&
		!d.activeCompleted &&
		d.activeID != "" &&
		sessionID != "" &&
		sessionID == d.sessionID
}

func (d *openCodeAppServerDriver) recordOpenCodeMessageRole(messageID, role string) {
	messageID = strings.TrimSpace(messageID)
	role = strings.ToLower(strings.TrimSpace(role))
	if messageID == "" || role == "" {
		return
	}
	d.mu.Lock()
	if d.messageRoles == nil {
		d.messageRoles = make(map[string]string)
	}
	d.messageRoles[messageID] = role
	d.mu.Unlock()
}

func (d *openCodeAppServerDriver) openCodePartRole(properties, part map[string]any) string {
	messageID := firstNonEmpty(stringFromMap(part, "messageID"), stringFromMap(properties, "messageID"))
	d.mu.Lock()
	role := d.messageRoles[messageID]
	d.mu.Unlock()
	return role
}

func (d *openCodeAppServerDriver) openCodeTextEvent(sessionID, turnID string, kind TurnEventKind, text, raw string) (TurnEvent, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return TurnEvent{}, false
	}
	if kind == TurnEventAssistantText {
		d.mu.Lock()
		if d.lastAssistantText == text {
			d.mu.Unlock()
			return TurnEvent{}, false
		}
		d.lastAssistantText = text
		d.mu.Unlock()
	}
	return TurnEvent{Provider: ProviderOpenCode, ThreadID: sessionID, TurnID: turnID, Kind: kind, Text: text, Raw: raw}, true
}

func (d *openCodeAppServerDriver) markOpenCodeTurnCompleted() {
	d.mu.Lock()
	d.activeCompleted = true
	d.mu.Unlock()
}

func (d *openCodeAppServerDriver) emitOpenCodeAssistantText(sessionID, turnID, text, raw string) {
	event, ok := d.openCodeAssistantTextEvent(sessionID, turnID, text, raw)
	if !ok {
		return
	}
	d.emit(event)
}

func (d *openCodeAppServerDriver) openCodeAssistantTextEvent(sessionID, turnID, text, raw string) (TurnEvent, bool) {
	return d.openCodeTextEvent(sessionID, turnID, TurnEventAssistantText, text, raw)
}

func (d *openCodeAppServerDriver) hasOpenCodeAssistantText() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return strings.TrimSpace(d.lastAssistantText) != ""
}

func (d *openCodeAppServerDriver) resetServerForNextRequest() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.baseURL = ""
	d.sessionID = ""
	d.activeID = ""
	d.activeCompleted = false
	d.lastAssistantText = ""
	d.messageRoles = make(map[string]string)
	if d.eventCancel != nil {
		d.eventCancel()
		d.eventCancel = nil
	}
	if d.cmd != nil && d.cmd.Process != nil {
		_ = d.cmd.Process.Kill()
		_ = d.cmd.Wait()
	}
	d.cmd = nil
}

func (d *openCodeAppServerDriver) emit(event TurnEvent) {
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

func openCodePartIsComplete(part map[string]any) bool {
	timePayload, _ := part["time"].(map[string]any)
	if len(timePayload) == 0 {
		return true
	}
	_, ok := timePayload["end"]
	return ok
}

func formatOpenCodeAppServerToolUse(part map[string]any) string {
	if len(part) == 0 {
		return "tool_use"
	}
	state, _ := part["state"].(map[string]any)
	input, _ := state["input"].(map[string]any)
	metadata, _ := state["metadata"].(map[string]any)
	parts := []string{"tool_use"}
	if tool := stringFromMap(part, "tool"); tool != "" {
		parts = append(parts, "tool=`"+tool+"`")
	}
	if callID := stringFromMap(part, "callID"); callID != "" {
		parts = append(parts, "call_id=`"+callID+"`")
	}
	if status := stringFromMap(state, "status"); status != "" {
		parts = append(parts, "status=`"+status+"`")
	}
	if command := stringFromMap(input, "command"); command != "" {
		parts = append(parts, "command=`"+command+"`")
	}
	desc := strings.TrimSpace(stringFromMap(input, "description"))
	if desc == "" {
		desc = strings.TrimSpace(stringFromMap(metadata, "description"))
	}
	if desc != "" {
		parts = append(parts, "description=`"+desc+"`")
	}
	if title := stringFromMap(state, "title"); title != "" {
		parts = append(parts, "title=`"+title+"`")
	}
	return strings.Join(parts, " ")
}

type openCodeAssistantInfo struct {
	Error map[string]any `json:"error"`
	Time  struct {
		Completed *float64 `json:"completed"`
	} `json:"time"`
	Finish string `json:"finish"`
	Tokens struct {
		Input     int64 `json:"input"`
		Output    int64 `json:"output"`
		Reasoning int64 `json:"reasoning"`
		Cache     struct {
			Read  int64 `json:"read"`
			Write int64 `json:"write"`
		} `json:"cache"`
	} `json:"tokens"`
}

func openCodeAssistantInfoFromMap(info map[string]any) openCodeAssistantInfo {
	var out openCodeAssistantInfo
	if len(info) == 0 {
		return out
	}
	raw, err := json.Marshal(info)
	if err != nil {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	return out
}

func (i openCodeAssistantInfo) Completed() bool {
	return i.Time.Completed != nil
}

func (i openCodeAssistantInfo) TurnFinished() bool {
	if !i.Completed() {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(i.Finish)) {
	case "", "tool-calls", "unknown":
		return false
	default:
		return true
	}
}

func (i openCodeAssistantInfo) Usage() Usage {
	return Usage{
		InputTokens:       i.Tokens.Input,
		CachedInputTokens: i.Tokens.Cache.Read,
		OutputTokens:      i.Tokens.Output,
	}
}

func (i openCodeAssistantInfo) Message() string {
	return openCodeErrorMessage(i.Error)
}

func openCodeErrorMessage(payload map[string]any) string {
	if len(payload) == 0 {
		return ""
	}
	if data, _ := payload["data"].(map[string]any); len(data) > 0 {
		if msg := stringFromMap(data, "message"); msg != "" {
			return msg
		}
	}
	if msg := stringFromMap(payload, "message"); msg != "" {
		return msg
	}
	if name := stringFromMap(payload, "name"); name != "" {
		return name
	}
	return "unknown error"
}

func splitOpenCodeModel(model string) (string, string) {
	model = strings.TrimSpace(model)
	providerID, modelID, ok := strings.Cut(model, "/")
	if !ok {
		return "", ""
	}
	return strings.TrimSpace(providerID), strings.TrimSpace(modelID)
}

func extractHTTPURL(line string) string {
	for _, field := range strings.Fields(line) {
		field = strings.TrimRight(field, ".,)")
		if strings.HasPrefix(field, "http://") || strings.HasPrefix(field, "https://") {
			return field
		}
	}
	return ""
}

var knownOpenCodeAgents = map[string]bool{
	"build":   true,
	"explore": true,
	"general": true,
	"plan":    true,
}

func isKnownOpenCodeAgent(agent string) bool {
	return knownOpenCodeAgents[strings.ToLower(strings.TrimSpace(agent))]
}

type openCodePromptResponse struct {
	Info  openCodeAssistantInfo `json:"info"`
	Parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"parts"`
}

func (r openCodePromptResponse) Text() string {
	parts := make([]string, 0, len(r.Parts))
	for _, part := range r.Parts {
		if part.Type != "text" {
			continue
		}
		if text := strings.TrimSpace(part.Text); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}
