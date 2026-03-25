package automation

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func taskPrefersCard(task Task) bool {
	task = NormalizeTask(task)
	return strings.Contains(task.Action.SessionKey, "|scene:work")
}

func buildTaskCardContent(task Task, markdown string) (string, error) {
	task = NormalizeTask(task)
	reply := strings.TrimSpace(markdown)
	if reply == "" {
		reply = " "
	}
	card := map[string]any{
		"schema": "2.0",
		"config": map[string]any{
			"enable_forward": true,
			"update_multi":   true,
		},
		"header": map[string]any{
			"title": map[string]any{
				"tag":     "plain_text",
				"content": taskCardTitle(task),
			},
			"template": "blue",
		},
		"body": map[string]any{
			"elements": []any{
				map[string]any{
					"tag":     "markdown",
					"content": reply,
				},
			},
		},
	}
	raw, err := json.Marshal(card)
	if err != nil {
		return "", fmt.Errorf("marshal task card failed: %w", err)
	}
	return string(raw), nil
}

func buildTaskWarningCardContent(task Task, markdown string, reason string) (string, error) {
	reply := strings.TrimSpace(markdown)
	if reply == "" {
		reply = "自动任务已暂停，等待人工介入。"
	}
	elements := []any{
		taskCardMarkdown("**状态**\n自动任务已暂停，等待人工介入。"),
		taskCardMarkdown("**任务**\n" + taskCardTitle(task)),
	}
	if trimmedReason := strings.TrimSpace(reason); trimmedReason != "" {
		elements = append(elements, taskCardMarkdown("**原因**\n"+trimmedReason))
	}
	elements = append(elements, taskCardMarkdown("**上下文**\n"+reply))
	card := map[string]any{
		"schema": "2.0",
		"config": map[string]any{
			"enable_forward": true,
			"update_multi":   true,
		},
		"header": map[string]any{
			"title": map[string]any{
				"tag":     "plain_text",
				"content": "需要人工介入",
			},
			"template": "orange",
		},
		"body": map[string]any{
			"elements": elements,
		},
	}
	raw, err := json.Marshal(card)
	if err != nil {
		return "", fmt.Errorf("marshal warning card failed: %w", err)
	}
	return string(raw), nil
}

func taskCardMarkdown(content string) map[string]any {
	return map[string]any{
		"tag":     "markdown",
		"content": content,
	}
}

func taskCardTitle(task Task) string {
	task = NormalizeTask(task)
	if task.Title != "" {
		return task.Title
	}
	if task.ID != "" {
		return task.ID
	}
	return "自动任务"
}

func detectWorkflowSignal(commands []WorkflowCommand) *taskSignal {
	signals := detectWorkflowSignals(commands)
	for i := range signals {
		if signals[i].pause {
			return &signals[i]
		}
	}
	if len(signals) > 0 {
		return &signals[0]
	}
	return nil
}

func detectWorkflowSignals(commands []WorkflowCommand) []taskSignal {
	var signals []taskSignal
	for _, command := range commands {
		if reason, ok := parseNeedsHumanCommand(command.Text); ok {
			signals = append(signals, taskSignal{kind: taskSignalNeedsHuman, message: reason, pause: true})
			continue
		}
		if reason, ok := parseReplanCommand(command.Text); ok {
			signals = append(signals, taskSignal{kind: taskSignalReplan, message: reason, pause: true})
			continue
		}
		if reason, ok := parseBlockedCommand(command.Text); ok {
			signals = append(signals, taskSignal{kind: taskSignalBlocked, message: reason, pause: true})
			continue
		}
		if finding, ok := parseDiscoveryCommand(command.Text); ok {
			signals = append(signals, taskSignal{kind: taskSignalDiscovery, message: finding, pause: false})
		}
	}
	return signals
}

func parseReplanCommand(command string) (string, bool) {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) < 2 || fields[0] != "/alice" {
		return "", false
	}
	keyword := strings.ToLower(strings.ReplaceAll(fields[1], "_", "-"))
	if keyword != "replan" && keyword != "re-plan" {
		return "", false
	}
	reason := strings.TrimSpace(strings.Join(fields[2:], " "))
	if reason == "" {
		reason = "executor requested replanning"
	}
	return reason, true
}

func parseBlockedCommand(command string) (string, bool) {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) < 2 || fields[0] != "/alice" {
		return "", false
	}
	if strings.ToLower(fields[1]) != "blocked" {
		return "", false
	}
	reason := strings.TrimSpace(strings.Join(fields[2:], " "))
	if reason == "" {
		reason = "executor reported task is blocked"
	}
	return reason, true
}

func parseDiscoveryCommand(command string) (string, bool) {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) < 2 || fields[0] != "/alice" {
		return "", false
	}
	if strings.ToLower(fields[1]) != "discovery" {
		return "", false
	}
	finding := strings.TrimSpace(strings.Join(fields[2:], " "))
	if finding == "" {
		finding = "executor reported a new discovery"
	}
	return finding, true
}

func parseNeedsHumanCommand(command string) (string, bool) {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) < 2 || fields[0] != "/alice" {
		return "", false
	}
	keyword := strings.ToLower(strings.ReplaceAll(fields[1], "_", "-"))
	switch {
	case keyword == "needs-human" || keyword == "needshuman":
		reason := strings.TrimSpace(strings.Join(fields[2:], " "))
		if reason == "" {
			reason = "workflow requested human intervention"
		}
		return reason, true
	case keyword == "needs" && len(fields) >= 3 && strings.EqualFold(fields[2], "human"):
		reason := strings.TrimSpace(strings.Join(fields[3:], " "))
		if reason == "" {
			reason = "workflow requested human intervention"
		}
		return reason, true
	default:
		return "", false
	}
}

func fallbackWorkflowReply(signal *taskSignal) string {
	if signal == nil {
		return ""
	}
	msg := strings.TrimSpace(signal.message)
	switch signal.kind {
	case taskSignalNeedsHuman:
		if msg == "" {
			return "需要人工介入，自动任务已暂停。"
		}
		return "需要人工介入，自动任务已暂停。\n\n原因：" + msg
	case taskSignalReplan:
		if msg == "" {
			return "执行方发现新情况，请求重新规划，当前任务已暂停。"
		}
		return "执行方发现新情况，请求重新规划，当前任务已暂停。\n\n原因：" + msg
	case taskSignalBlocked:
		if msg == "" {
			return "任务遇到阻塞，无法继续执行。"
		}
		return "任务遇到阻塞，无法继续执行。\n\n原因：" + msg
	case taskSignalDiscovery:
		if msg == "" {
			return "执行方报告了新发现。"
		}
		return "执行方报告了新发现：\n\n" + msg
	default:
		return ""
	}
}

func buildReplanCardContent(task Task, markdown string, reason string) (string, error) {
	reply := strings.TrimSpace(markdown)
	if reply == "" {
		reply = "执行方发现新情况，请求重新规划，当前任务已暂停。"
	}
	elements := []any{
		taskCardMarkdown("**状态**\n请求重新规划，当前任务已暂停。"),
		taskCardMarkdown("**任务**\n" + taskCardTitle(task)),
	}
	if trimmedReason := strings.TrimSpace(reason); trimmedReason != "" {
		elements = append(elements, taskCardMarkdown("**原因**\n"+trimmedReason))
	}
	elements = append(elements, taskCardMarkdown("**上下文**\n"+reply))
	card := map[string]any{
		"schema": "2.0",
		"config": map[string]any{
			"enable_forward": true,
			"update_multi":   true,
		},
		"header": map[string]any{
			"title": map[string]any{
				"tag":     "plain_text",
				"content": "请求重新规划",
			},
			"template": "red",
		},
		"body": map[string]any{
			"elements": elements,
		},
	}
	raw, err := json.Marshal(card)
	if err != nil {
		return "", fmt.Errorf("marshal replan card failed: %w", err)
	}
	return string(raw), nil
}

func buildBlockedCardContent(task Task, markdown string, reason string) (string, error) {
	reply := strings.TrimSpace(markdown)
	if reply == "" {
		reply = "任务遇到阻塞，无法继续执行。"
	}
	elements := []any{
		taskCardMarkdown("**状态**\n任务遇到阻塞，无法继续执行。"),
		taskCardMarkdown("**任务**\n" + taskCardTitle(task)),
	}
	if trimmedReason := strings.TrimSpace(reason); trimmedReason != "" {
		elements = append(elements, taskCardMarkdown("**原因**\n"+trimmedReason))
	}
	elements = append(elements, taskCardMarkdown("**上下文**\n"+reply))
	card := map[string]any{
		"schema": "2.0",
		"config": map[string]any{
			"enable_forward": true,
			"update_multi":   true,
		},
		"header": map[string]any{
			"title": map[string]any{
				"tag":     "plain_text",
				"content": "任务阻塞",
			},
			"template": "orange",
		},
		"body": map[string]any{
			"elements": elements,
		},
	}
	raw, err := json.Marshal(card)
	if err != nil {
		return "", fmt.Errorf("marshal blocked card failed: %w", err)
	}
	return string(raw), nil
}

func buildDiscoveryCardContent(task Task, markdown string, finding string) (string, error) {
	reply := strings.TrimSpace(markdown)
	if reply == "" {
		reply = "执行方报告了新发现。"
	}
	elements := []any{
		taskCardMarkdown("**任务**\n" + taskCardTitle(task)),
	}
	if trimmedFinding := strings.TrimSpace(finding); trimmedFinding != "" {
		elements = append(elements, taskCardMarkdown("**发现**\n"+trimmedFinding))
	}
	elements = append(elements, taskCardMarkdown("**上下文**\n"+reply))
	card := map[string]any{
		"schema": "2.0",
		"config": map[string]any{
			"enable_forward": true,
			"update_multi":   true,
		},
		"header": map[string]any{
			"title": map[string]any{
				"tag":     "plain_text",
				"content": "新发现",
			},
			"template": "blue",
		},
		"body": map[string]any{
			"elements": elements,
		},
	}
	raw, err := json.Marshal(card)
	if err != nil {
		return "", fmt.Errorf("marshal discovery card failed: %w", err)
	}
	return string(raw), nil
}

func renderActionTemplate(raw string, now time.Time) (string, error) {
	template := strings.TrimSpace(raw)
	if template == "" {
		return "", nil
	}
	if now.IsZero() {
		now = time.Now().Local()
	}
	now = now.Local()
	template = strings.NewReplacer(
		"{{now}}", now.Format(time.RFC3339),
		"{{date}}", now.Format("2006-01-02"),
		"{{time}}", now.Format("15:04:05"),
		"{{unix}}", strconv.FormatInt(now.Unix(), 10),
	).Replace(template)
	rendered, err := actionTemplateRenderer.RenderString("automation-action", template, map[string]any{
		"Now":  now,
		"Date": now.Format("2006-01-02"),
		"Time": now.Format("15:04:05"),
		"Unix": now.Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("render action template failed: %w", err)
	}
	return strings.TrimSpace(rendered), nil
}
