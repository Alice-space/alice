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
