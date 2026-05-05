package connector

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

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
	Status           string
	Elapsed          time.Duration
	SinceVisible     time.Duration
	SinceBackend     time.Duration
	LastBackendKind  string
	ShellCommand     string
	ShellCommandKind string
	FileChanges      []string
	FileChangeTotal  int
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
	if state.ShellCommand != "" {
		kindLabel := strings.TrimSpace(state.ShellCommandKind)
		switch kindLabel {
		case "tool_use", "tool_call":
			kindLabel = "后端指令"
		default:
			kindLabel = "后端操作"
		}
		lines = append(lines, "**"+kindLabel+"**："+clipText(state.ShellCommand, 300))
	}
	if len(state.FileChanges) == 0 {
		lines = append(lines, "**最近代码编辑**：暂无")
	} else {
		total := state.FileChangeTotal
		if total < len(state.FileChanges) {
			total = len(state.FileChanges)
		}
		lines = append(lines, fmt.Sprintf("**最近代码编辑**：%d 项", total))
		for _, raw := range state.FileChanges {
			line := trimLLMHeartbeatMarkdownListPrefix(raw)
			if line == "" {
				continue
			}
			lines = append(lines, "- "+clipText(line, 300))
		}
		if hidden := total - len(state.FileChanges); hidden > 0 {
			lines = append(lines, fmt.Sprintf("另有 %d 项未展示", hidden))
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
