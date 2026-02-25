package codex

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

const fileChangeCallbackPrefix = "[file_change] "

type fileDiffStat struct {
	Additions int
	Deletions int
}

type repoDiffSnapshot map[string]fileDiffStat

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
		"codex prompt assemble thread_id=%s model=%q profile=%q prefix=%q user_prompt=%q final_prompt=%q",
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

	cmdArgs := buildExecArgs(threadID, prompt, model, profile)
	cmd := exec.CommandContext(tctx, r.Command, cmdArgs...)
	if strings.TrimSpace(r.WorkspaceDir) != "" {
		cmd.Dir = r.WorkspaceDir
	}
	cmd.Env = mergeEnv(mergeEnv(os.Environ(), r.Env), env)
	logging.Debugf(
		"run codex command command=%q thread_id=%s model=%q profile=%q args=%q cwd=%q timeout=%s",
		r.Command,
		threadID,
		model,
		profile,
		cmdArgs,
		cmd.Dir,
		timeout,
	)
	watchedRepos := discoverWatchRepos(cmd.Dir)
	repoLease := syntheticDiffGuard.Acquire(watchedRepos)
	defer syntheticDiffGuard.Release(repoLease)
	repoSnapshots := captureRepoSnapshots(tctx, watchedRepos)

	tryEmitSyntheticFileChanges := func() {
		if onThinking == nil {
			return
		}
		if !syntheticDiffGuard.CanEmit(repoLease) {
			repoSnapshots = captureRepoSnapshots(tctx, watchedRepos)
			logging.Debugf("codex synthetic file_change suppressed reason=concurrent_runs")
			return
		}
		diffMessages, nextSnapshots := collectRepoDiffMessages(tctx, watchedRepos, repoSnapshots)
		repoSnapshots = nextSnapshots
		for _, message := range diffMessages {
			logging.Debugf("codex synthetic file_change=%q", strings.TrimSpace(message))
			onThinking(fileChangeCallbackPrefix + strings.TrimSpace(message))
		}
	}

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
		logging.Debugf("codex start failed err=%v", err)
		return "", "", fmt.Errorf("start codex process failed: %w", err)
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
	activeThreadID := strings.TrimSpace(threadID)
	sawNativeFileChange := false
	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 0, 64*1024), 5*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		stdout.WriteString(line)
		stdout.WriteByte('\n')
		logging.Debugf("codex stdout line=%s", line)

		reasoning, agentMessage, fileChangeMessage, parsedThreadID := parseEventLine(line)
		if strings.TrimSpace(parsedThreadID) != "" {
			activeThreadID = strings.TrimSpace(parsedThreadID)
			logging.Debugf("codex thread started thread_id=%s", activeThreadID)
		}
		if strings.TrimSpace(reasoning) != "" {
			logging.Debugf("codex reasoning=%q", strings.TrimSpace(reasoning))
		}
		if strings.TrimSpace(fileChangeMessage) != "" {
			resolvedFileChangeMessage := enrichFileChangeMessageStats(tctx, fileChangeMessage, watchedRepos)
			if strings.TrimSpace(resolvedFileChangeMessage) == "" {
				resolvedFileChangeMessage = strings.TrimSpace(fileChangeMessage)
			}
			sawNativeFileChange = true
			logging.Debugf("codex file_change=%q", strings.TrimSpace(resolvedFileChangeMessage))
			if onThinking != nil {
				onThinking(fileChangeCallbackPrefix + strings.TrimSpace(resolvedFileChangeMessage))
			}
		}
		if strings.TrimSpace(agentMessage) != "" {
			finalMessage = strings.TrimSpace(agentMessage)
			if onThinking != nil {
				onThinking(finalMessage)
			}
			logging.Debugf("codex agent_message=%q", finalMessage)
		}

		if onThinking != nil && !sawNativeFileChange && isSuccessfulCommandExecutionCompleted(line) {
			tryEmitSyntheticFileChanges()
		}
	}

	if scanErr := scanner.Err(); scanErr != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		<-stderrDone
		if errors.Is(tctx.Err(), context.DeadlineExceeded) {
			logging.Debugf("codex run timeout while scanning elapsed=%s", time.Since(startedAt))
			return "", activeThreadID, errors.New("codex timeout")
		}
		if errors.Is(tctx.Err(), context.Canceled) {
			logging.Debugf("codex run canceled while scanning elapsed=%s", time.Since(startedAt))
			return "", activeThreadID, context.Canceled
		}
		logging.Debugf("codex scan failed elapsed=%s err=%v", time.Since(startedAt), scanErr)
		return "", activeThreadID, fmt.Errorf("read codex output failed: %w", scanErr)
	}

	err = cmd.Wait()
	<-stderrDone
	stderrText := strings.TrimSpace(stderr.String())
	if stderrText != "" {
		logging.Debugf("codex stderr=%s", stderrText)
	}
	if errors.Is(tctx.Err(), context.DeadlineExceeded) {
		logging.Debugf("codex run timeout elapsed=%s", time.Since(startedAt))
		return "", activeThreadID, errors.New("codex timeout")
	}
	if errors.Is(tctx.Err(), context.Canceled) {
		logging.Debugf("codex run canceled elapsed=%s", time.Since(startedAt))
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
		logging.Debugf("codex run failed elapsed=%s err=%v detail=%s", time.Since(startedAt), err, detail)
		return "", activeThreadID, fmt.Errorf("codex exec failed: %w (%s)", err, detail)
	}

	if onThinking != nil && !sawNativeFileChange {
		tryEmitSyntheticFileChanges()
	}
	_ = repoSnapshots

	if finalMessage == "" {
		message, parseErr := ParseFinalMessage(stdout.String())
		if parseErr != nil {
			logging.Debugf("codex final message parse failed elapsed=%s err=%v", time.Since(startedAt), parseErr)
			return "", activeThreadID, parseErr
		}
		finalMessage = strings.TrimSpace(message)
	}
	logging.Debugf(
		"codex run completed elapsed=%s thread_id=%s final_message=%q",
		time.Since(startedAt),
		activeThreadID,
		finalMessage,
	)
	return finalMessage, activeThreadID, nil
}
