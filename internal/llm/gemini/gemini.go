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
	"strings"
	"time"

	"github.com/Alice-space/alice/internal/logging"
	"github.com/Alice-space/alice/internal/prompting"
)

type Runner struct {
	Command      string
	Timeout      time.Duration
	Env          map[string]string
	PromptPrefix string
	WorkspaceDir string
	Prompts      *prompting.Loader
}

type jsonResponse struct {
	SessionID string `json:"session_id"`
	Response  string `json:"response"`
}

func (r Runner) Run(ctx context.Context, userText string) (string, error) {
	reply, _, err := r.RunWithThreadAndProgress(ctx, "", "assistant", userText, "", "", "", nil, nil)
	return reply, err
}

func (r Runner) RunWithThreadAndProgress(
	ctx context.Context,
	threadID string,
	agentName string,
	userText string,
	model string,
	personality string,
	noReplyToken string,
	env map[string]string,
	onThinking func(step string),
) (string, string, error) {
	threadID = strings.TrimSpace(threadID)
	agentName = strings.TrimSpace(agentName)
	model = strings.TrimSpace(model)
	personality = strings.TrimSpace(personality)

	prompt, err := r.renderPrompt(threadID, userText, personality, noReplyToken)
	if err != nil {
		return "", threadID, err
	}
	if strings.TrimSpace(prompt) == "" {
		return "", threadID, errors.New("empty prompt")
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
		return "", threadID, fmt.Errorf("create stdout pipe failed: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", threadID, fmt.Errorf("create stderr pipe failed: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", threadID, fmt.Errorf("start gemini process failed: %w", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	stdoutDone := make(chan struct{})
	stderrDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(&stdout, stdoutPipe)
		close(stdoutDone)
	}()
	go func() {
		_, _ = io.Copy(&stderr, stderrPipe)
		close(stderrDone)
	}()

	err = cmd.Wait()
	<-stdoutDone
	<-stderrDone

	detail := strings.TrimSpace(stderr.String())
	if errors.Is(tctx.Err(), context.DeadlineExceeded) {
		return "", threadID, errors.New("gemini timeout")
	}
	if errors.Is(tctx.Err(), context.Canceled) {
		return "", threadID, context.Canceled
	}
	if err != nil {
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if len(detail) > 400 {
			detail = detail[:400]
		}
		runErr := fmt.Errorf("gemini exec failed: %w (%s)", err, detail)
		return "", threadID, decorateNodeVersionError(runErr, detail)
	}

	response, parseErr := parseJSONResponse(stdout.String())
	if parseErr != nil {
		return "", threadID, parseErr
	}
	if onThinking != nil && strings.TrimSpace(response.Response) != "" {
		onThinking(strings.TrimSpace(response.Response))
	}

	nextThreadID := strings.TrimSpace(response.SessionID)
	if nextThreadID == "" {
		nextThreadID = threadID
	}
	logging.DebugAgentTrace(logging.AgentTrace{
		Provider: "gemini",
		Agent:    agentName,
		ThreadID: nextThreadID,
		Model:    model,
		Input:    prompt,
		Output:   strings.TrimSpace(response.Response),
	})
	return strings.TrimSpace(response.Response), nextThreadID, nil
}

func (r Runner) renderPrompt(threadID string, userText string, personality string, noReplyToken string) (string, error) {
	loader := r.Prompts
	if loader == nil {
		loader = prompting.DefaultLoader()
	}
	promptPrefix, err := prompting.ComposePromptPrefix(loader, r.PromptPrefix, personality, noReplyToken)
	if err != nil {
		return "", err
	}
	return loader.RenderFile("llm/initial_prompt.md.tmpl", map[string]any{
		"Resume":       strings.TrimSpace(threadID) != "",
		"ThreadID":     strings.TrimSpace(threadID),
		"PromptPrefix": promptPrefix,
		"UserText":     strings.TrimSpace(userText),
	})
}

func parseJSONResponse(raw string) (jsonResponse, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return jsonResponse{}, errors.New("gemini returned empty output")
	}

	var response jsonResponse
	if err := json.Unmarshal([]byte(trimmed), &response); err != nil {
		return jsonResponse{}, fmt.Errorf("parse gemini json output failed: %w", err)
	}
	if strings.TrimSpace(response.Response) == "" {
		return jsonResponse{}, errors.New("gemini returned no final response")
	}
	return response, nil
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
	for key, value := range overrides {
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
		return fmt.Errorf("%w; gemini CLI is using an older Node runtime from PATH, ensure Alice starts with Node >= 20 (for example via nvm PATH)", runErr)
	}
	return runErr
}
