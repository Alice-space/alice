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

func buildTaskCardContent(task Task, markdown string) string {
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
	raw, _ := json.Marshal(card)
	return string(raw)
}

func buildTaskWarningCardContent(task Task, markdown string, reason string) string {
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
	raw, _ := json.Marshal(card)
	return string(raw)
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
	for _, command := range commands {
		if reason, ok := parseNeedsHumanCommand(command.Text); ok {
			return &taskSignal{
				kind:    taskSignalNeedsHuman,
				message: reason,
				pause:   true,
			}
		}
	}
	return nil
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
	if signal.kind != taskSignalNeedsHuman {
		return ""
	}
	if strings.TrimSpace(signal.message) == "" {
		return "需要人工介入，自动任务已暂停。"
	}
	return "需要人工介入，自动任务已暂停。\n\n原因：" + signal.message
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
