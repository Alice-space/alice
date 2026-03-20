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

	"github.com/Alice-space/alice/internal/logging"
	"github.com/Alice-space/alice/internal/prompting"
)

type Runner struct {
	Command                string
	Timeout                time.Duration
	DefaultModel           string
	DefaultReasoningEffort string
	Env                    map[string]string
	PromptPrefix           string
	WorkspaceDir           string
	Prompts                *prompting.Loader
}

const fileChangeCallbackPrefix = "[file_change] "

type fileDiffStat struct {
	Additions int
	Deletions int
}

type repoDiffSnapshot map[string]fileDiffStat

func (r Runner) Run(ctx context.Context, userText string) (string, error) {
	reply, _, err := r.RunWithThreadAndProgress(ctx, "", "assistant", userText, "", "", "", "", "", nil, nil)
	return reply, err
}

func (r Runner) RunWithProgress(
	ctx context.Context,
	userText string,
	onThinking func(step string),
) (string, error) {
	reply, _, err := r.RunWithThreadAndProgress(ctx, "", "assistant", userText, "", "", "", "", "", nil, onThinking)
	return reply, err
}

func (r Runner) RunWithThread(
	ctx context.Context,
	threadID string,
	userText string,
) (string, string, error) {
	return r.RunWithThreadAndProgress(ctx, threadID, "assistant", userText, "", "", "", "", "", nil, nil)
}

func (r Runner) RunWithThreadAndProgress(
	ctx context.Context,
	threadID string,
	agentName string,
	userText string,
	model string,
	profile string,
	reasoningEffort string,
	personality string,
	noReplyToken string,
	env map[string]string,
	onThinking func(step string),
) (string, string, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		model = strings.TrimSpace(r.DefaultModel)
	}
	profile = strings.TrimSpace(profile)
	reasoningEffort = strings.TrimSpace(reasoningEffort)
	if reasoningEffort == "" {
		reasoningEffort = strings.TrimSpace(r.DefaultReasoningEffort)
	}
	agentName = strings.TrimSpace(agentName)
	prompt, err := r.renderPrompt(threadID, userText, personality, noReplyToken)
	logging.Debugf(
		"codex prompt assemble thread_id=%s model=%q profile=%q prefix=%q user_prompt=%q final_prompt=%q",
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
		timeout = 172800 * time.Second
	}

	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmdArgs := buildExecArgs(threadID, prompt, model, profile, reasoningEffort)
	cmd := exec.CommandContext(tctx, r.Command, cmdArgs...)
	configureInterruptibleCommand(cmd, "codex")
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
	activeThreadID := strings.TrimSpace(threadID)
	toolCalls := make([]string, 0, 4)
	finalMessage := ""
	emitTrace := func(runErr error) {
		logging.DebugAgentTrace(logging.AgentTrace{
			Provider:  "codex",
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
		if toolCall := parseToolCallLine(line); strings.TrimSpace(toolCall) != "" {
			toolCalls = append(toolCalls, strings.TrimSpace(toolCall))
			logging.Debugf("codex tool_call=%q", strings.TrimSpace(toolCall))
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
			timeoutErr := errors.New("codex timeout")
			emitTrace(timeoutErr)
			return "", activeThreadID, timeoutErr
		}
		if errors.Is(tctx.Err(), context.Canceled) {
			logging.Debugf("codex run canceled while scanning elapsed=%s", time.Since(startedAt))
			emitTrace(context.Canceled)
			return "", activeThreadID, context.Canceled
		}
		logging.Debugf("codex scan failed elapsed=%s err=%v", time.Since(startedAt), scanErr)
		runErr := fmt.Errorf("read codex output failed: %w", scanErr)
		emitTrace(runErr)
		return "", activeThreadID, runErr
	}

	err = cmd.Wait()
	<-stderrDone
	stderrText := strings.TrimSpace(stderr.String())
	if stderrText != "" {
		logging.Debugf("codex stderr=%s", stderrText)
	}
	if errors.Is(tctx.Err(), context.DeadlineExceeded) {
		logging.Debugf("codex run timeout elapsed=%s", time.Since(startedAt))
		timeoutErr := errors.New("codex timeout")
		emitTrace(timeoutErr)
		return "", activeThreadID, timeoutErr
	}
	if errors.Is(tctx.Err(), context.Canceled) {
		logging.Debugf("codex run canceled elapsed=%s", time.Since(startedAt))
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
		logging.Debugf("codex run failed elapsed=%s err=%v detail=%s", time.Since(startedAt), err, detail)
		runErr := fmt.Errorf("codex exec failed: %w (%s)", err, detail)
		emitTrace(runErr)
		return "", activeThreadID, runErr
	}

	if onThinking != nil && !sawNativeFileChange {
		tryEmitSyntheticFileChanges()
	}
	_ = repoSnapshots

	if finalMessage == "" {
		message, parseErr := ParseFinalMessage(stdout.String())
		if parseErr != nil {
			logging.Debugf("codex final message parse failed elapsed=%s err=%v", time.Since(startedAt), parseErr)
			emitTrace(parseErr)
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
	emitTrace(nil)
	return finalMessage, activeThreadID, nil
}

func (r Runner) renderPrompt(threadID string, userText string, personality string, noReplyToken string) (string, error) {
	loader := r.Prompts
	if loader == nil {
		loader = prompting.DefaultLoader()
	}
	promptPrefix, err := r.composePromptPrefix(loader, personality, noReplyToken)
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

func (r Runner) composePromptPrefix(loader *prompting.Loader, personality string, noReplyToken string) (string, error) {
	parts := make([]string, 0, 2)
	if prefix := strings.TrimSpace(r.PromptPrefix); prefix != "" {
		parts = append(parts, prefix)
	}
	personality = strings.ToLower(strings.TrimSpace(personality))
	if personality != "" {
		rendered, err := loader.RenderFile("llm/personalities/"+personality+".md.tmpl", map[string]any{
			"NoReplyToken": strings.TrimSpace(noReplyToken),
		})
		if err != nil {
			return "", err
		}
		if rendered != "" {
			parts = append(parts, rendered)
		}
	}
	return strings.Join(parts, "\n\n"), nil
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
