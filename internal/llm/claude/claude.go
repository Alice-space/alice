package claude

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
	reply, _, err := r.RunWithThreadAndProgress(ctx, "", "assistant", userText, "", "", nil, nil)
	return reply, err
}

func (r Runner) RunWithProgress(
	ctx context.Context,
	userText string,
	onThinking func(step string),
) (string, error) {
	reply, _, err := r.RunWithThreadAndProgress(ctx, "", "assistant", userText, "", "", nil, onThinking)
	return reply, err
}

func (r Runner) RunWithThread(
	ctx context.Context,
	threadID string,
	userText string,
) (string, string, error) {
	return r.RunWithThreadAndProgress(ctx, threadID, "assistant", userText, "", "", nil, nil)
}

func (r Runner) RunWithThreadAndProgress(
	ctx context.Context,
	threadID string,
	agentName string,
	userText string,
	model string,
	profile string,
	env map[string]string,
	onThinking func(step string),
) (string, string, error) {
	model = strings.TrimSpace(model)
	profile = strings.TrimSpace(profile)
	agentName = strings.TrimSpace(agentName)
	prompt, err := r.renderPrompt(threadID, userText)
	logging.Debugf(
		"claude prompt assemble thread_id=%s model=%q profile=%q prefix=%q user_prompt=%q final_prompt=%q",
		threadID,
		model,
		profile,
		r.PromptPrefix,
		userText,
		prompt,
	)
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(prompt) == "" {
		return "", "", errors.New("empty prompt")
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

	logging.Debugf(
		"run claude command command=%q thread_id=%s model=%q profile=%q args=%q cwd=%q timeout=%s",
		r.Command,
		threadID,
		model,
		profile,
		cmdArgs,
		cmd.Dir,
		timeout,
	)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", "", fmt.Errorf("create stdout pipe failed: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", "", fmt.Errorf("create stderr pipe failed: %w", err)
	}

	startedAt := time.Now()
	if err := cmd.Start(); err != nil {
		logging.Debugf("claude start failed err=%v", err)
		return "", "", fmt.Errorf("start claude process failed: %w", err)
	}
	logging.Debugf("claude process started pid=%d", cmd.Process.Pid)

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
	resultMessage := ""
	resultErrors := []string{}
	resultIsError := false
	emitTrace := func(runErr error) {
		logging.DebugAgentTrace(logging.AgentTrace{
			Provider:  "claude",
			Agent:     agentName,
			ThreadID:  activeThreadID,
			Model:     model,
			Profile:   profile,
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
		logging.Debugf("claude stdout line=%s", line)

		event := parseEventLine(line)
		if strings.TrimSpace(event.SessionID) != "" {
			activeThreadID = strings.TrimSpace(event.SessionID)
			logging.Debugf("claude session id=%s", activeThreadID)
		}
		if strings.TrimSpace(event.AssistantText) != "" {
			finalMessage = strings.TrimSpace(event.AssistantText)
			if onThinking != nil {
				onThinking(finalMessage)
			}
			logging.Debugf("claude assistant_message=%q", finalMessage)
		}
		if toolCall := event.ToolCall; strings.TrimSpace(toolCall) != "" {
			toolCalls = append(toolCalls, toolCall)
			logging.Debugf("claude tool_call=%q", toolCall)
		}
		if event.HasResultEvent {
			if strings.TrimSpace(event.ResultText) != "" {
				resultMessage = strings.TrimSpace(event.ResultText)
			}
			if len(event.ResultErrors) > 0 {
				resultErrors = event.ResultErrors
			}
			resultIsError = event.ResultIsError
		}
	}

	if scanErr := scanner.Err(); scanErr != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		<-stderrDone
		if errors.Is(tctx.Err(), context.DeadlineExceeded) {
			logging.Debugf("claude run timeout while scanning elapsed=%s", time.Since(startedAt))
			timeoutErr := errors.New("claude timeout")
			emitTrace(timeoutErr)
			return "", activeThreadID, timeoutErr
		}
		if errors.Is(tctx.Err(), context.Canceled) {
			logging.Debugf("claude run canceled while scanning elapsed=%s", time.Since(startedAt))
			emitTrace(context.Canceled)
			return "", activeThreadID, context.Canceled
		}
		logging.Debugf("claude scan failed elapsed=%s err=%v", time.Since(startedAt), scanErr)
		runErr := fmt.Errorf("read claude output failed: %w", scanErr)
		emitTrace(runErr)
		return "", activeThreadID, runErr
	}

	err = cmd.Wait()
	<-stderrDone
	stderrText := strings.TrimSpace(stderr.String())
	if stderrText != "" {
		logging.Debugf("claude stderr=%s", stderrText)
	}
	if errors.Is(tctx.Err(), context.DeadlineExceeded) {
		logging.Debugf("claude run timeout elapsed=%s", time.Since(startedAt))
		timeoutErr := errors.New("claude timeout")
		emitTrace(timeoutErr)
		return "", activeThreadID, timeoutErr
	}
	if errors.Is(tctx.Err(), context.Canceled) {
		logging.Debugf("claude run canceled elapsed=%s", time.Since(startedAt))
		emitTrace(context.Canceled)
		return "", activeThreadID, context.Canceled
	}
	if err != nil {
		detail := stderrText
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if len(detail) > 400 {
			detail = detail[:400]
		}
		logging.Debugf("claude run failed elapsed=%s err=%v detail=%s", time.Since(startedAt), err, detail)
		runErr := fmt.Errorf("claude exec failed: %w (%s)", err, detail)
		emitTrace(runErr)
		return "", activeThreadID, runErr
	}

	if resultIsError {
		detail := strings.TrimSpace(resultMessage)
		if detail == "" && len(resultErrors) > 0 {
			detail = strings.Join(resultErrors, "\n")
		}
		if detail == "" {
			detail = "unknown claude error"
		}
		logging.Debugf("claude result error elapsed=%s detail=%q", time.Since(startedAt), detail)
		runErr := fmt.Errorf("claude exec failed: %s", detail)
		emitTrace(runErr)
		return "", activeThreadID, runErr
	}

	if strings.TrimSpace(finalMessage) == "" && strings.TrimSpace(resultMessage) != "" {
		finalMessage = strings.TrimSpace(resultMessage)
	}
	if finalMessage == "" {
		message, parseErr := ParseFinalMessage(stdout.String())
		if parseErr != nil {
			logging.Debugf("claude final message parse failed elapsed=%s err=%v", time.Since(startedAt), parseErr)
			emitTrace(parseErr)
			return "", activeThreadID, parseErr
		}
		finalMessage = strings.TrimSpace(message)
	}

	logging.Debugf(
		"claude run completed elapsed=%s session_id=%s final_message=%q",
		time.Since(startedAt),
		activeThreadID,
		finalMessage,
	)
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

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
