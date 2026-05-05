// Package opencode drives the opencode CLI as a subprocess and parses its
// JSON-lines output into a plain text reply, session ID, and token usage.
package opencode

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

	"github.com/Alice-space/alice/internal/llm/internal/repodiff"
	"github.com/Alice-space/alice/internal/llm/internal/shared"
)

// Runner executes the opencode CLI for a single request.
type Runner struct {
	shared.RunnerBase
	DefaultModel   string
	DefaultVariant string
}

type stepStart struct {
	Type      string `json:"type"`
	SessionID string `json:"sessionID"`
}

type textEvent struct {
	Type string `json:"type"`
	Part struct {
		Text string `json:"text"`
	} `json:"part"`
}

type stepFinish struct {
	Type string `json:"type"`
	Part struct {
		Tokens struct {
			Input      int64 `json:"input"`
			Output     int64 `json:"output"`
			Reasoning  int64 `json:"reasoning"`
			Total      int64 `json:"total"`
			CacheWrite int64 `json:"cache_write,omitempty"`
			CacheRead  int64 `json:"cache_read,omitempty"`
			Cache      struct {
				Write int64 `json:"write"`
				Read  int64 `json:"read"`
			} `json:"cache"`
		} `json:"tokens"`
	} `json:"part"`
}

// Run is a convenience wrapper that runs without thread resumption or progress
// callbacks. It uses the runner's DefaultModel and DefaultVariant when set.
func (r Runner) Run(ctx context.Context, userText string) (string, error) {
	reply, _, _, _, _, err := r.RunWithThreadAndProgress(ctx, "", userText, r.DefaultModel, r.DefaultVariant, nil, nil, nil)
	return reply, err
}

// RunWithThreadAndProgress runs the opencode CLI and returns the final reply,
// next session ID, and token usage.
//
//   - threadID: resume an existing session when non-empty.
//   - userText: the fully assembled prompt.
//   - model: the provider/model string (e.g. "deepseek/deepseek-v4-pro").
//   - variant: the model variant (e.g. "max", "high", "minimal").
//   - env: merged over the process environment.
//   - onProgress: called with each text event as an independent agent message; may be nil.
//   - onRawEvent: optional callback for raw stdout events (kind, line, detail);
//     nil disables raw event delivery.
func (r Runner) RunWithThreadAndProgress(
	ctx context.Context,
	threadID string,
	userText string,
	model string,
	variant string,
	env map[string]string,
	onProgress func(step string),
	onRawEvent func(kind, line, detail string),
) (string, string, int64, int64, int64, error) {
	prompt := strings.TrimSpace(userText)
	if prompt == "" {
		return "", "", 0, 0, 0, shared.ErrPromptEmpty
	}

	model = strings.TrimSpace(model)
	variant = strings.TrimSpace(variant)
	if model == "" {
		model = strings.TrimSpace(r.DefaultModel)
	}
	if variant == "" {
		variant = strings.TrimSpace(r.DefaultVariant)
	}

	timeout := r.Timeout
	if timeout <= 0 {
		timeout = shared.DefaultLLMTimeout
	}
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmdArgs := buildRunArgs(threadID, prompt, model, variant)
	cmd := exec.CommandContext(tctx, r.Command, cmdArgs...)
	if strings.TrimSpace(r.WorkspaceDir) != "" {
		cmd.Dir = r.WorkspaceDir
	}
	cmd.Env = shared.MergeEnv(shared.MergeEnv(os.Environ(), r.Env), env)
	diffEmitter := repodiff.NewEmitter(tctx, cmd.Dir, onProgress)
	defer diffEmitter.Close()

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", "", 0, 0, 0, fmt.Errorf("create stdout pipe failed: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", "", 0, 0, 0, fmt.Errorf("create stderr pipe failed: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", "", 0, 0, 0, fmt.Errorf("start opencode process failed: %w", err)
	}

	var stderr bytes.Buffer
	stderrDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(&stderr, stderrPipe)
		close(stderrDone)
	}()

	var (
		reply        string
		nextThreadID string
		inputTokens  int64
		outputTokens int64
		cacheTokens  int64
	)

	finalText := ""
	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 0, shared.DefaultScannerBuf), 2*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		lineText := string(line)
		if onRawEvent != nil {
			onRawEvent("stdout_line", lineText, "")
			if kind, detail := parseOpenCodeRawEvent(line); kind != "" {
				onRawEvent(kind, lineText, detail)
			}
		}
		diffEmitter.Emit()
		parsed := parseOpenCodeLine(line, &inputTokens, &outputTokens, &cacheTokens, &nextThreadID, &finalText)
		if parsed != "" && onProgress != nil {
			onProgress(parsed)
		}
	}

	reply = strings.TrimSpace(finalText)

	err = cmd.Wait()
	<-stderrDone
	diffEmitter.Emit()

	cachedInputTokens := cacheTokens

	detail := strings.TrimSpace(stderr.String())
	if errors.Is(tctx.Err(), context.DeadlineExceeded) {
		return "", nextThreadID, inputTokens, outputTokens, cachedInputTokens, shared.ErrLLMTimeout
	}
	if errors.Is(tctx.Err(), context.Canceled) {
		return "", nextThreadID, inputTokens, outputTokens, cachedInputTokens, context.Canceled
	}
	if err != nil {
		if detail == "" {
			detail = strings.TrimSpace(finalText)
		}
		if len(detail) > 400 {
			detail = detail[:400]
		}
		runErr := fmt.Errorf("opencode exec failed: %w (%s)", err, detail)
		return "", nextThreadID, inputTokens, outputTokens, cachedInputTokens, formatLoginError(runErr, detail)
	}
	if reply == "" {
		errDetail := strings.TrimSpace(stderr.String())
		if errDetail != "" {
			if len(errDetail) > 400 {
				errDetail = errDetail[:400]
			}
			return "", nextThreadID, inputTokens, outputTokens, cachedInputTokens, fmt.Errorf("opencode returned no response text (stderr: %s)", errDetail)
		}
		return "", nextThreadID, inputTokens, outputTokens, cachedInputTokens, errors.New("opencode returned no response text")
	}

	return reply, nextThreadID, inputTokens, outputTokens, cachedInputTokens, nil
}

func parseOpenCodeRawEvent(line []byte) (string, string) {
	var ev map[string]any
	if err := json.Unmarshal(line, &ev); err != nil {
		return "", ""
	}
	eventType := strings.ToLower(shared.ExtractString(ev, "type"))
	part, _ := ev["part"].(map[string]any)
	switch eventType {
	case "reasoning":
		return "reasoning", shared.ExtractString(part, "text")
	case "tool_use":
		detail := formatOpenCodeToolUse(part)
		if detail == "" {
			return "tool_use", "tool_use"
		}
		return "tool_use", detail
	default:
		return "", ""
	}
}

func formatOpenCodeToolUse(part map[string]any) string {
	if len(part) == 0 {
		return ""
	}
	state, _ := part["state"].(map[string]any)
	input, _ := state["input"].(map[string]any)
	metadata, _ := state["metadata"].(map[string]any)
	parts := []string{"tool_use"}
	if tool := shared.ExtractString(part, "tool"); tool != "" {
		parts = append(parts, "tool=`"+tool+"`")
	}
	if callID := shared.ExtractString(part, "callID"); callID != "" {
		parts = append(parts, "call_id=`"+callID+"`")
	}
	if status := shared.ExtractString(state, "status"); status != "" {
		parts = append(parts, "status=`"+status+"`")
	}
	if command := shared.ExtractString(input, "command"); command != "" {
		parts = append(parts, "command=`"+command+"`")
	}
	desc := shared.ExtractString(input, "description")
	if desc == "" {
		desc = shared.ExtractString(metadata, "description")
	}
	if desc != "" {
		parts = append(parts, "description=`"+desc+"`")
	}
	if title := shared.ExtractString(state, "title"); title != "" {
		parts = append(parts, "title=`"+title+"`")
	}
	return strings.Join(parts, " ")
}

func parseOpenCodeLine(line []byte, inputTokens, outputTokens, cacheTokens *int64, nextThreadID *string, finalText *string) string {
	var peek struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(line, &peek); err != nil {
		return ""
	}

	switch peek.Type {
	case "step_start":
		var ev stepStart
		if err := json.Unmarshal(line, &ev); err != nil {
			return ""
		}
		if ev.SessionID != "" && *nextThreadID == "" {
			*nextThreadID = ev.SessionID
		}
		return ""
	case "text":
		var ev textEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			return ""
		}
		text := strings.TrimSpace(ev.Part.Text)
		if text != "" {
			*finalText = text
			return text
		}
		return ""
	case "step_finish":
		var ev stepFinish
		if err := json.Unmarshal(line, &ev); err != nil {
			return ""
		}
		*inputTokens = ev.Part.Tokens.Input
		*outputTokens = ev.Part.Tokens.Output
		*cacheTokens = ev.Part.Tokens.CacheRead
		if *cacheTokens == 0 {
			*cacheTokens = ev.Part.Tokens.Cache.Read
		}
		return ""
	}

	return ""
}
