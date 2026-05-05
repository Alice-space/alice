// Package gemini drives the gemini CLI as a subprocess and parses its JSON
// output into a plain text reply.
package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// Runner executes the gemini CLI for a single request.
type Runner struct {
	Command      string
	Timeout      time.Duration
	Env          map[string]string
	WorkspaceDir string
}

type jsonResponse struct {
	SessionID string    `json:"session_id"`
	Response  string    `json:"response"`
	Stats     jsonStats `json:"stats"`
}

type jsonStats struct {
	Models map[string]jsonModelStats `json:"models"`
}

type jsonModelStats struct {
	Tokens jsonTokenStats `json:"tokens"`
}

type jsonTokenStats struct {
	Input      int64 `json:"input"`
	Candidates int64 `json:"candidates"`
	Cached     int64 `json:"cached"`
}

// Run is a convenience wrapper that runs without thread resumption or progress
// callbacks.
func (r Runner) Run(ctx context.Context, userText string) (string, error) {
	reply, _, _, _, _, err := r.RunWithThreadAndProgress(ctx, "", userText, "", nil, nil)
	return reply, err
}

// RunWithThreadAndProgress runs the gemini CLI and returns the final reply and
// next session ID.
//
//   - threadID: resume an existing session when non-empty.
//   - userText: the fully assembled prompt.
//   - model: overrides the CLI default when non-empty.
//   - env: merged over the process environment.
//   - onProgress: called with the final reply; may be nil.
func (r Runner) RunWithThreadAndProgress(
	ctx context.Context,
	threadID string,
	userText string,
	model string,
	env map[string]string,
	onProgress func(step string),
) (string, string, int64, int64, int64, error) {
	threadID = strings.TrimSpace(threadID)
	model = strings.TrimSpace(model)
	prompt := strings.TrimSpace(userText)
	if prompt == "" {
		return "", threadID, 0, 0, 0, errors.New("empty prompt")
	}

	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 172800 * time.Second
	}
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmdArgs := buildExecArgs(threadID, prompt, model)
	cmd := exec.CommandContext(tctx, r.Command, cmdArgs...)
	if strings.TrimSpace(r.WorkspaceDir) != "" {
		cmd.Dir = r.WorkspaceDir
	}
	cmd.Env = mergeEnv(mergeEnv(os.Environ(), r.Env), env)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", threadID, 0, 0, 0, fmt.Errorf("create stdout pipe failed: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", threadID, 0, 0, 0, fmt.Errorf("create stderr pipe failed: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", threadID, 0, 0, 0, fmt.Errorf("start gemini process failed: %w", err)
	}

	stdoutPreview := limitedBuffer{limit: 4096}
	var stderr bytes.Buffer
	stderrDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(&stderr, stderrPipe)
		close(stderrDone)
	}()

	var response jsonResponse
	decodeErr := json.NewDecoder(io.TeeReader(stdoutPipe, &stdoutPreview)).Decode(&response)

	err = cmd.Wait()
	<-stderrDone

	detail := strings.TrimSpace(stderr.String())
	inputTokens, cachedInputTokens, outputTokens := response.usageTotals()
	if errors.Is(tctx.Err(), context.DeadlineExceeded) {
		return "", threadID, inputTokens, cachedInputTokens, outputTokens, errors.New("gemini timeout")
	}
	if errors.Is(tctx.Err(), context.Canceled) {
		return "", threadID, inputTokens, cachedInputTokens, outputTokens, context.Canceled
	}
	if err != nil {
		if detail == "" {
			detail = strings.TrimSpace(stdoutPreview.String())
		}
		if len(detail) > 400 {
			detail = detail[:400]
		}
		runErr := fmt.Errorf("gemini exec failed: %w (%s)", err, detail)
		return "", threadID, inputTokens, cachedInputTokens, outputTokens, decorateNodeVersionError(runErr, detail)
	}
	if decodeErr != nil {
		return "", threadID, inputTokens, cachedInputTokens, outputTokens, fmt.Errorf("parse gemini json output failed: %w", decodeErr)
	}
	if err := validateJSONResponse(response); err != nil {
		return "", threadID, inputTokens, cachedInputTokens, outputTokens, err
	}

	reply := strings.TrimSpace(response.Response)
	if onProgress != nil && reply != "" {
		onProgress(reply)
	}

	nextThreadID := strings.TrimSpace(response.SessionID)
	if nextThreadID == "" {
		nextThreadID = threadID
	}
	return reply, nextThreadID, inputTokens, cachedInputTokens, outputTokens, nil
}

func parseJSONResponse(raw string) (jsonResponse, error) {
	var response jsonResponse
	if err := json.NewDecoder(strings.NewReader(raw)).Decode(&response); err != nil {
		return jsonResponse{}, fmt.Errorf("parse gemini json output failed: %w", err)
	}
	if err := validateJSONResponse(response); err != nil {
		return jsonResponse{}, err
	}
	return response, nil
}

func validateJSONResponse(response jsonResponse) error {
	if strings.TrimSpace(response.Response) == "" {
		return errors.New("gemini returned no final response")
	}
	return nil
}

func (r jsonResponse) usageTotals() (int64, int64, int64) {
	var inputTokens int64
	var cachedInputTokens int64
	var outputTokens int64
	for _, modelStats := range r.Stats.Models {
		inputTokens += modelStats.Tokens.Input
		cachedInputTokens += modelStats.Tokens.Cached
		outputTokens += modelStats.Tokens.Candidates
	}
	return inputTokens, cachedInputTokens, outputTokens
}

func mergeEnv(base []string, overrides map[string]string) []string {
	if len(overrides) == 0 {
		return base
	}
	env := make([]string, len(base))
	copy(env, base)
	indexByKey := make(map[string]int, len(env))
	for i, item := range env {
		key := envKey(item)
		if key == "" {
			continue
		}
		indexByKey[key] = i
	}
	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := overrides[key]
		pair := key + "=" + value
		if idx, ok := indexByKey[key]; ok {
			env[idx] = pair
			continue
		}
		env = append(env, pair)
	}
	return env
}

func envKey(item string) string {
	idx := strings.Index(item, "=")
	if idx <= 0 {
		return ""
	}
	return item[:idx]
}

func decorateNodeVersionError(runErr error, detail string) error {
	lower := strings.ToLower(detail)
	if strings.Contains(lower, "invalid regular expression flags") && strings.Contains(lower, "node.js v18") {
		return fmt.Errorf("%w; gemini CLI is using an older Node runtime from PATH, ensure Node >= 20 (for example via nvm PATH)", runErr)
	}
	return runErr
}

type limitedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b == nil {
		return len(p), nil
	}
	remaining := b.limit - b.buffer.Len()
	if remaining > 0 {
		if len(p) > remaining {
			_, _ = b.buffer.Write(p[:remaining])
		} else {
			_, _ = b.buffer.Write(p)
		}
	}
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	if b == nil {
		return ""
	}
	return b.buffer.String()
}
