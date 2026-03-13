package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildPromptUsesStructuredExecutionPayload(t *testing.T) {
	skillsDir := t.TempDir()
	writeSkillFile(t, skillsDir, "mcp-tool-output", `---
name: mcp-tool-output
description: test
---

# MCP Tool Output

Submit results with MCP tools.`)
	writeSkillFile(t, skillsDir, "direct-answer", `---
name: direct-answer
description: test
---

# Direct Answer

Use the structured payload.`)

	agent := NewLocalAgent(Config{
		SkillsDir: skillsDir,
		MCPServer: stubMCPServer{},
	})
	prompt, err := agent.buildPrompt(ExecuteRequest{
		Operation: "direct_answer",
		Skills:    []string{"mcp-tool-output", "direct-answer"},
		Input: map[string]any{
			"user_input": "查询上海明天天气",
			"context": map[string]any{
				"locale": "zh-CN",
			},
		},
		Constraints: ExecuteConstraints{
			ReadOnly: true,
		},
	})
	if err != nil {
		t.Fatalf("buildPrompt returned error: %v", err)
	}

	if strings.Contains(prompt, "name: mcp-tool-output") {
		t.Fatalf("expected frontmatter to be stripped, prompt=%q", prompt)
	}
	if !strings.Contains(prompt, `<skill name="mcp-tool-output">`) {
		t.Fatalf("expected wrapped skill content, prompt=%q", prompt)
	}
	if !strings.Contains(prompt, `"operation": "direct_answer"`) {
		t.Fatalf("expected operation in prompt payload, prompt=%q", prompt)
	}
	if !strings.Contains(prompt, `"read_only": true`) {
		t.Fatalf("expected read_only constraint in prompt payload, prompt=%q", prompt)
	}
	if !strings.Contains(prompt, `<alice-execution-request>`) {
		t.Fatalf("expected structured execution envelope, prompt=%q", prompt)
	}
	if strings.Contains(prompt, "# Task") {
		t.Fatalf("expected legacy task headings to be removed, prompt=%q", prompt)
	}
}

func TestBuildPromptSkipsMCPToolOutputWithoutMCPServer(t *testing.T) {
	skillsDir := t.TempDir()
	writeSkillFile(t, skillsDir, "mcp-tool-output", "# MCP Tool Output")
	writeSkillFile(t, skillsDir, "direct-answer", "# Direct Answer")

	agent := NewLocalAgent(Config{SkillsDir: skillsDir})
	prompt, err := agent.buildPrompt(ExecuteRequest{
		Operation: "direct_answer",
		Skills:    []string{"mcp-tool-output", "direct-answer"},
	})
	if err != nil {
		t.Fatalf("buildPrompt returned error: %v", err)
	}

	if strings.Contains(prompt, "MCP Tool Output") {
		t.Fatalf("expected mcp-tool-output skill to be skipped when MCP is unavailable, prompt=%q", prompt)
	}
	if !strings.Contains(prompt, "Direct Answer") {
		t.Fatalf("expected remaining skills to be included, prompt=%q", prompt)
	}
}

func TestRequestedSkillsDeduplicatesAndPreservesOrder(t *testing.T) {
	req := ExecuteRequest{
		Skill:  "direct-answer",
		Skills: []string{"mcp-tool-output", "direct-answer", "public-info-query", "mcp-tool-output"},
	}

	got := requestedSkills(req)
	want := []string{"direct-answer", "mcp-tool-output", "public-info-query"}
	if len(got) != len(want) {
		t.Fatalf("unexpected length: got=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected skills order: got=%v want=%v", got, want)
		}
	}
}

func writeSkillFile(t *testing.T, skillsDir, skillName, content string) {
	t.Helper()
	skillDir := filepath.Join(skillsDir, skillName)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", skillDir, err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write skill %s: %v", skillName, err)
	}
}

type stubMCPServer struct{}

func (stubMCPServer) URL() string {
	return "http://127.0.0.1:0"
}

func (stubMCPServer) ConfigJSON(string) string {
	return `{"mcpServers":{"alice-tools":{"transport":"http","url":"http://127.0.0.1:0"}}}`
}
