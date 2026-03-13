package kimi

import (
	"bufio"
	"bytes"
	"context"
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

func (r Runner) Run(ctx context.Context, userText string) (string, error) {
	reply, _, err := r.RunWithThreadAndProgress(ctx, "", "assistant", userText, "", nil, nil)
	return reply, err
}

func (r Runner) RunWithThreadAndProgress(
	ctx context.Context,
	threadID string,
	agentName string,
	userText string,
	model string,
	env map[string]string,
	onThinking func(step string),
) (string, string, error) {
	agentName = strings.TrimSpace(agentName)
	model = strings.TrimSpace(model)
	prompt, err := r.renderPrompt(threadID, userText)
	if err != nil {
		return "", strings.TrimSpace(threadID), err
	}
	if strings.TrimSpace(prompt) == "" {
		return "", strings.TrimSpace(threadID), errors.New("empty prompt")
	}

	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 120 * time.Second
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
		return "", strings.TrimSpace(threadID), fmt.Errorf("create stdout pipe failed: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", strings.TrimSpace(threadID), fmt.Errorf("create stderr pipe failed: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", strings.TrimSpace(threadID), fmt.Errorf("start kimi process failed: %w", err)
	}

	var stderr bytes.Buffer
	stderrDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(&stderr, stderrPipe)
		close(stderrDone)
	}()

	var stdout bytes.Buffer
	finalMessage := ""
	activeThreadID := strings.TrimSpace(threadID)
	toolCalls := make([]string, 0, 2)
	emitTrace := func(runErr error) {
		logging.DebugAgentTrace(logging.AgentTrace{
			Provider:  "kimi",
			Agent:     agentName,
			ThreadID:  activeThreadID,
			Model:     model,
			Input:     prompt,
			Output:    finalMessage,
			ToolCalls: toolCalls,
			Error:     errorString(runErr),
		})
	}

	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 0, 64*1024), 5*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		stdout.WriteString(line)
		stdout.WriteByte('\n')

		event := parseEventLine(line)
		if strings.TrimSpace(event.ToolCall) != "" {
			toolCalls = append(toolCalls, strings.TrimSpace(event.ToolCall))
		}
		if strings.TrimSpace(event.Text) != "" {
			finalMessage = strings.TrimSpace(event.Text)
			if onThinking != nil {
				onThinking(finalMessage)
			}
		}
	}

	if scanErr := scanner.Err(); scanErr != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		<-stderrDone
		runErr := fmt.Errorf("read kimi output failed: %w", scanErr)
		emitTrace(runErr)
		return "", activeThreadID, runErr
	}

	err = cmd.Wait()
	<-stderrDone
	if errors.Is(tctx.Err(), context.DeadlineExceeded) {
		timeoutErr := errors.New("kimi timeout")
		emitTrace(timeoutErr)
		return "", activeThreadID, timeoutErr
	}
	if errors.Is(tctx.Err(), context.Canceled) {
		emitTrace(context.Canceled)
		return "", activeThreadID, context.Canceled
	}
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if len(detail) > 400 {
			detail = detail[:400]
		}
		runErr := fmt.Errorf("kimi exec failed: %w (%s)", err, detail)
		emitTrace(runErr)
		return "", activeThreadID, runErr
	}

	if finalMessage == "" {
		message, parseErr := ParseFinalMessage(stdout.String())
		if parseErr != nil {
			emitTrace(parseErr)
			return "", activeThreadID, parseErr
		}
		finalMessage = strings.TrimSpace(message)
	}
	emitTrace(nil)
	return finalMessage, activeThreadID, nil
}

func (r Runner) renderPrompt(threadID string, userText string) (string, error) {
	if r.Prompts == nil {
		return buildPrompt(threadID, r.PromptPrefix, userText), nil
	}
	return r.Prompts.RenderFile("llm/initial_prompt.md.tmpl", map[string]any{
		"Resume":       strings.TrimSpace(threadID) != "",
		"ThreadID":     strings.TrimSpace(threadID),
		"PromptPrefix": strings.TrimSpace(r.PromptPrefix),
		"UserText":     strings.TrimSpace(userText),
	})
}

func mergeEnv(base []string, overrides map[string]string) []string {
	if len(overrides) == 0 {
		return base
	}
	env := make([]string, len(base))
	copy(env, base)
	indexByKey := make(map[string]int, len(env))
	for i, item := range env {
		idx := strings.Index(item, "=")
		if idx <= 0 {
			continue
		}
		indexByKey[item[:idx]] = i
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

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
