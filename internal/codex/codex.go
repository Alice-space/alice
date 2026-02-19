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
	if strings.TrimSpace(prompt) == "" {
		return "", errors.New("empty prompt")
	}

	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}

	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(
		tctx,
		r.Command,
		"exec",
		"--json",
		"--skip-git-repo-check",
		"--sandbox",
		"danger-full-access",
		prompt,
	)
	if strings.TrimSpace(r.WorkspaceDir) != "" {
		cmd.Dir = r.WorkspaceDir
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("create stdout pipe failed: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("create stderr pipe failed: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start codex process failed: %w", err)
	}

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

		reasoning, agentMessage := parseEventLine(line)
		if strings.TrimSpace(reasoning) != "" && onThinking != nil {
			onThinking(strings.TrimSpace(reasoning))
		}
		if strings.TrimSpace(agentMessage) != "" {
			finalMessage = strings.TrimSpace(agentMessage)
		}
	}

	if scanErr := scanner.Err(); scanErr != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		<-stderrDone
		return "", fmt.Errorf("read codex output failed: %w", scanErr)
	}

	err = cmd.Wait()
	<-stderrDone
	if errors.Is(tctx.Err(), context.DeadlineExceeded) {
		return "", errors.New("codex timeout")
	}
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if len(detail) > 400 {
			detail = detail[:400]
		}
		return "", fmt.Errorf("codex exec failed: %w (%s)", err, detail)
	}

	if finalMessage == "" {
		message, parseErr := ParseFinalMessage(stdout.String())
		if parseErr != nil {
			return "", parseErr
		}
		finalMessage = strings.TrimSpace(message)
	}
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
