package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"gitee.com/alicespace/alice/internal/logging"
)

type Runner struct {
	Command      string
	Timeout      time.Duration
	PromptPrefix string
	WorkspaceDir string
}

func (r Runner) Run(ctx context.Context, userText string) (string, error) {
	return r.RunWithProgress(ctx, userText, nil)
}

func (r Runner) RunWithProgress(
	ctx context.Context,
	userText string,
	onThinking func(step string),
) (string, error) {
	prompt := strings.TrimSpace(r.PromptPrefix) + "\n\n" + strings.TrimSpace(userText)
	logging.Debugf(
		"codex prompt assemble prefix=%q user_prompt=%q final_prompt=%q",
		r.PromptPrefix,
		userText,
		prompt,
	)
	if strings.TrimSpace(prompt) == "" {
		return "", errors.New("empty prompt")
	}

	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}

	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmdArgs := []string{
		"exec",
		"--json",
		"--skip-git-repo-check",
		"--sandbox",
		"danger-full-access",
		prompt,
	}
	cmd := exec.CommandContext(tctx, r.Command, cmdArgs...)
	if strings.TrimSpace(r.WorkspaceDir) != "" {
		cmd.Dir = r.WorkspaceDir
	}
	logging.Debugf(
		"run codex command command=%q args=%q cwd=%q timeout=%s",
		r.Command,
		cmdArgs,
		cmd.Dir,
		timeout,
	)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("create stdout pipe failed: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("create stderr pipe failed: %w", err)
	}

	startedAt := time.Now()
	if err := cmd.Start(); err != nil {
		logging.Debugf("codex start failed err=%v", err)
		return "", fmt.Errorf("start codex process failed: %w", err)
	}
	logging.Debugf("codex process started pid=%d", cmd.Process.Pid)

	var stderr bytes.Buffer
	stderrDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(&stderr, stderrPipe)
		close(stderrDone)
	}()

	var stdout bytes.Buffer
	var finalMessage string
	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 0, 64*1024), 5*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		stdout.WriteString(line)
		stdout.WriteByte('\n')
		logging.Debugf("codex stdout line=%s", line)

		reasoning, agentMessage := parseEventLine(line)
		if strings.TrimSpace(reasoning) != "" && onThinking != nil {
			logging.Debugf("codex reasoning=%q", strings.TrimSpace(reasoning))
			onThinking(strings.TrimSpace(reasoning))
		}
		if strings.TrimSpace(agentMessage) != "" {
			finalMessage = strings.TrimSpace(agentMessage)
			logging.Debugf("codex agent_message=%q", finalMessage)
		}
	}

	if scanErr := scanner.Err(); scanErr != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		<-stderrDone
		if errors.Is(tctx.Err(), context.DeadlineExceeded) {
			logging.Debugf("codex run timeout while scanning elapsed=%s", time.Since(startedAt))
			return "", errors.New("codex timeout")
		}
		if errors.Is(tctx.Err(), context.Canceled) {
			logging.Debugf("codex run canceled while scanning elapsed=%s", time.Since(startedAt))
			return "", context.Canceled
		}
		logging.Debugf("codex scan failed elapsed=%s err=%v", time.Since(startedAt), scanErr)
		return "", fmt.Errorf("read codex output failed: %w", scanErr)
	}

	err = cmd.Wait()
	<-stderrDone
	stderrText := strings.TrimSpace(stderr.String())
	if stderrText != "" {
		logging.Debugf("codex stderr=%s", stderrText)
	}
	if errors.Is(tctx.Err(), context.DeadlineExceeded) {
		logging.Debugf("codex run timeout elapsed=%s", time.Since(startedAt))
		return "", errors.New("codex timeout")
	}
	if errors.Is(tctx.Err(), context.Canceled) {
		logging.Debugf("codex run canceled elapsed=%s", time.Since(startedAt))
		return "", context.Canceled
	}
	if err != nil {
		detail := stderrText
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if len(detail) > 400 {
			detail = detail[:400]
		}
		logging.Debugf("codex run failed elapsed=%s err=%v detail=%s", time.Since(startedAt), err, detail)
		return "", fmt.Errorf("codex exec failed: %w (%s)", err, detail)
	}

	if finalMessage == "" {
		message, parseErr := ParseFinalMessage(stdout.String())
		if parseErr != nil {
			logging.Debugf("codex final message parse failed elapsed=%s err=%v", time.Since(startedAt), parseErr)
			return "", parseErr
		}
		finalMessage = strings.TrimSpace(message)
	}
	logging.Debugf("codex run completed elapsed=%s final_message=%q", time.Since(startedAt), finalMessage)
	return finalMessage, nil
}

func ParseFinalMessage(jsonlOutput string) (string, error) {
	var lastMessage string
	scanner := bufio.NewScanner(strings.NewReader(jsonlOutput))
	scanner.Buffer(make([]byte, 0, 64*1024), 5*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		_, text := parseEventLine(line)
		if strings.TrimSpace(text) != "" {
			lastMessage = text
		}
	}

	if err := scanner.Err(); err != nil {
		return "", err
	}
	if strings.TrimSpace(lastMessage) == "" {
		return "", errors.New("codex returned no final agent message")
	}
	return lastMessage, nil
}

func parseEventLine(line string) (reasoning string, agentMessage string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", ""
	}

	var event map[string]any
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return "", ""
	}
	if event["type"] != "item.completed" {
		return "", ""
	}

	item, ok := event["item"].(map[string]any)
	if !ok {
		return "", ""
	}
	itemType, _ := item["type"].(string)
	text, _ := item["text"].(string)
	switch itemType {
	case "reasoning":
		return text, ""
	case "agent_message":
		return "", text
	default:
		return "", ""
	}
}
