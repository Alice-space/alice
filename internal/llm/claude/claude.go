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

	"gitee.com/alicespace/alice/internal/logging"
)

type Runner struct {
	Command      string
	Timeout      time.Duration
	Env          map[string]string
	PromptPrefix string
	WorkspaceDir string
}

func (r Runner) Run(ctx context.Context, userText string) (string, error) {
	reply, _, err := r.RunWithThreadAndProgress(ctx, "", userText, "", "", nil, nil)
	return reply, err
}

func (r Runner) RunWithProgress(
	ctx context.Context,
	userText string,
	onThinking func(step string),
) (string, error) {
	reply, _, err := r.RunWithThreadAndProgress(ctx, "", userText, "", "", nil, onThinking)
	return reply, err
}

func (r Runner) RunWithThread(
	ctx context.Context,
	threadID string,
	userText string,
) (string, string, error) {
	return r.RunWithThreadAndProgress(ctx, threadID, userText, "", "", nil, nil)
}

func (r Runner) RunWithThreadAndProgress(
	ctx context.Context,
	threadID string,
	userText string,
	model string,
	profile string,
	env map[string]string,
	onThinking func(step string),
) (string, string, error) {
	model = strings.TrimSpace(model)
	profile = strings.TrimSpace(profile)
	prompt := buildPrompt(threadID, r.PromptPrefix, userText)
	logging.Debugf(
		"claude prompt assemble thread_id=%s model=%q profile=%q prefix=%q user_prompt=%q final_prompt=%q",
		threadID,
		model,
		profile,
		r.PromptPrefix,
		userText,
		prompt,
	)
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
	resultMessage := ""
	resultErrors := []string{}
	resultIsError := false

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
			return "", activeThreadID, errors.New("claude timeout")
		}
		if errors.Is(tctx.Err(), context.Canceled) {
			logging.Debugf("claude run canceled while scanning elapsed=%s", time.Since(startedAt))
			return "", activeThreadID, context.Canceled
		}
		logging.Debugf("claude scan failed elapsed=%s err=%v", time.Since(startedAt), scanErr)
		return "", activeThreadID, fmt.Errorf("read claude output failed: %w", scanErr)
	}

	err = cmd.Wait()
	<-stderrDone
	stderrText := strings.TrimSpace(stderr.String())
	if stderrText != "" {
		logging.Debugf("claude stderr=%s", stderrText)
	}
	if errors.Is(tctx.Err(), context.DeadlineExceeded) {
		logging.Debugf("claude run timeout elapsed=%s", time.Since(startedAt))
		return "", activeThreadID, errors.New("claude timeout")
	}
	if errors.Is(tctx.Err(), context.Canceled) {
		logging.Debugf("claude run canceled elapsed=%s", time.Since(startedAt))
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
		return "", activeThreadID, fmt.Errorf("claude exec failed: %w (%s)", err, detail)
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
		return "", activeThreadID, fmt.Errorf("claude exec failed: %s", detail)
	}

	if strings.TrimSpace(finalMessage) == "" && strings.TrimSpace(resultMessage) != "" {
		finalMessage = strings.TrimSpace(resultMessage)
	}
	if finalMessage == "" {
		message, parseErr := ParseFinalMessage(stdout.String())
		if parseErr != nil {
			logging.Debugf("claude final message parse failed elapsed=%s err=%v", time.Since(startedAt), parseErr)
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
	return finalMessage, activeThreadID, nil
}
