package connector

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func normalizeReasoning(step string) string {
	step = strings.TrimSpace(step)
	step = strings.Trim(step, "*")
	step = strings.TrimSpace(step)
	if step == "" {
		return ""
	}
	return clipText(step, 600)
}

func buildProgressCardContent(thinkingText, answerText string, failed bool, interrupted bool, elapsed time.Duration) string {
	status := "思考中"
	if interrupted {
		status = "已中断"
	} else if failed {
		status = "失败"
	} else if strings.TrimSpace(answerText) != "" {
		status = "已完成"
	}

	thinking := clipText(strings.TrimSpace(thinkingText), 4000)
	answer := clipText(strings.TrimSpace(answerText), 4000)
	if thinking == "" {
		thinking = "（暂无）"
	}
	durationLabel := "已思考：" + formatElapsed(elapsed)
	if interrupted || failed || strings.TrimSpace(answer) != "" {
		durationLabel = "总耗时：" + formatElapsed(elapsed)
	}

	elements := []any{
		cardMarkdown("**状态**：" + status + "（" + durationLabel + "）"),
		cardMarkdown("**Codex 思考**\n" + thinking),
	}
	if strings.TrimSpace(answer) != "" {
		elements = append(elements, cardMarkdown("**回复**\n"+answer))
	}

	card := map[string]any{
		"schema": "2.0",
		"config": map[string]any{
			"enable_forward": true,
			"update_multi":   true,
		},
		"body": map[string]any{
			"elements": elements,
		},
	}
	raw, _ := json.Marshal(card)
	return string(raw)
}

func buildReplyCardContent(markdown string) string {
	reply := strings.TrimSpace(markdown)
	if reply == "" {
		reply = " "
	}
	return buildMarkdownCardContent("", "**回复**\n"+reply)
}

func buildTitledReplyCardContent(title, markdown string) string {
	reply := strings.TrimSpace(markdown)
	if reply == "" {
		reply = " "
	}
	return buildMarkdownCardContent(title, reply)
}

type llmHeartbeatCardState struct {
	Status          string
	Elapsed         time.Duration
	SinceVisible    time.Duration
	SinceBackend    time.Duration
	LastBackendKind string
	FileChanges     []string
}

func buildLLMHeartbeatCardContent(state llmHeartbeatCardState) string {
	status := strings.TrimSpace(state.Status)
	if status == "" {
		status = "运行中"
	}
	backendKind := strings.TrimSpace(state.LastBackendKind)
	if backendKind == "" {
		backendKind = "无"
	}
	lines := []string{
		"**状态**：" + status,
		"**已运行**：" + formatElapsed(state.Elapsed),
		"**最近可见输出**：" + formatElapsed(state.SinceVisible) + " 前",
		"**最近后端活动**：" + formatElapsed(state.SinceBackend) + " 前",
		"**后端事件**：" + backendKind,
	}
	if len(state.FileChanges) == 0 {
		lines = append(lines, "**最近代码编辑**：暂无")
	} else {
		lines = append(lines, "**最近代码编辑**：")
		for _, raw := range state.FileChanges {
			line := strings.TrimSpace(raw)
			if line == "" {
				continue
			}
			lines = append(lines, "- "+clipText(line, 300))
		}
	}
	return buildMarkdownCardContent("运行状态", strings.Join(lines, "\n"))
}

func buildMarkdownCardContent(title, markdown string) string {
	card := map[string]any{
		"schema": "2.0",
		"config": map[string]any{
			"enable_forward": true,
			"update_multi":   true,
		},
		"body": map[string]any{
			"elements": []any{
				cardMarkdown(markdown),
			},
		},
	}
	if strings.TrimSpace(title) != "" {
		card["header"] = map[string]any{
			"title": map[string]any{
				"tag":     "plain_text",
				"content": strings.TrimSpace(title),
			},
			"template": "blue",
		}
	}
	raw, _ := json.Marshal(card)
	return string(raw)
}

func cardMarkdown(content string) map[string]any {
	return map[string]any{
		"tag":     "markdown",
		"content": content,
	}
}

func clipText(text string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + "..."
}

func formatElapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if d < time.Hour {
		minutes := int(d / time.Minute)
		seconds := int((d % time.Minute) / time.Second)
		return fmt.Sprintf("%dm%02ds", minutes, seconds)
	}

	hours := int(d / time.Hour)
	minutes := int((d % time.Hour) / time.Minute)
	seconds := int((d % time.Minute) / time.Second)
	return fmt.Sprintf("%dh%02dm%02ds", hours, minutes, seconds)
}
