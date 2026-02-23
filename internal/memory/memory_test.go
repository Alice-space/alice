package memory

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestManagerInit_RequiresMemoryDir(t *testing.T) {
	mgr := NewManager("   ")
	err := mgr.Init()
	if err == nil || !strings.Contains(err.Error(), "memory dir is empty") {
		t.Fatalf("expected memory dir validation error, got: %v", err)
	}
}

func TestManagerInit_CreatesMemoryDirStructure(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "memory")

	mgr := NewManager(dir)
	if err := mgr.Init(); err != nil {
		t.Fatalf("init memory failed: %v", err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("init should create memory dir, err=%v", err)
	}
	shortTermDir := filepath.Join(dir, ShortTermDirName)
	if info, err := os.Stat(shortTermDir); err != nil || !info.IsDir() {
		t.Fatalf("init should create short-term memory dir, err=%v", err)
	}
	longTermFile := filepath.Join(dir, LongTermFileName)
	if info, err := os.Stat(longTermFile); err != nil || info.IsDir() {
		t.Fatalf("init should create long-term memory file, err=%v", err)
	}
}

func TestManagerBuildPrompt_ContainsLongTermAndPaths(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memory")
	now := time.Date(2026, 2, 19, 11, 30, 0, 0, time.UTC)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create dir failed: %v", err)
	}

	mgr := NewManager(dir)
	mgr.now = func() time.Time { return now }

	longPath := filepath.Join(dir, LongTermFileName)
	if err := os.WriteFile(longPath, []byte("长期偏好：回答要简洁。"), 0o644); err != nil {
		t.Fatalf("write long-term failed: %v", err)
	}
	shortPath := filepath.Join(dir, ShortTermDirName, "2026-02-19.md")
	if err := os.MkdirAll(filepath.Dir(shortPath), 0o755); err != nil {
		t.Fatalf("create short-term dir failed: %v", err)
	}
	if err := os.WriteFile(shortPath, []byte("今天提到：关注连接器稳定性。"), 0o644); err != nil {
		t.Fatalf("write short-term failed: %v", err)
	}

	prompt, err := mgr.BuildPrompt("帮我总结下")
	if err != nil {
		t.Fatalf("build prompt failed: %v", err)
	}
	if !strings.Contains(prompt, "长期偏好：回答要简洁。") {
		t.Fatalf("prompt missing long-term memory: %s", prompt)
	}
	if !strings.Contains(prompt, longPath) {
		t.Fatalf("prompt missing long-term file location: %s", prompt)
	}
	if strings.Contains(prompt, "今天提到：关注连接器稳定性。") {
		t.Fatalf("prompt should not inline short-term memory: %s", prompt)
	}
	if !strings.Contains(prompt, filepath.Join(dir, ShortTermDirName)) {
		t.Fatalf("prompt missing short-term dir location: %s", prompt)
	}
	if !strings.Contains(prompt, "系统仅会在会话空闲超时后自动追加“空闲摘要”") {
		t.Fatalf("prompt missing idle-summary instruction: %s", prompt)
	}
	if !strings.Contains(prompt, "帮我总结下") {
		t.Fatalf("prompt missing user message: %s", prompt)
	}
}

func TestManagerBuildPrompt_LongTermMissingIsEmpty(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memory")
	mgr := NewManager(dir)

	prompt, err := mgr.BuildPrompt("hello")
	if err != nil {
		t.Fatalf("build prompt failed: %v", err)
	}
	if !strings.Contains(prompt, "（空）") {
		t.Fatalf("prompt should include empty long-term memory marker: %s", prompt)
	}
}

func TestManagerBuildPrompt_IncludesProjectGuideFiles(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "project")
	memoryDir := filepath.Join(projectDir, ".memory")
	if err := os.MkdirAll(memoryDir, 0o755); err != nil {
		t.Fatalf("create memory dir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "AGENTS.md"), []byte("规则A：先测后提。"), 0o644); err != nil {
		t.Fatalf("write AGENTS.md failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "GEMINI.md"), []byte("规则B：先事实后判断。"), 0o644); err != nil {
		t.Fatalf("write GEMINI.md failed: %v", err)
	}

	mgr := NewManager(memoryDir)
	mgr.SetProjectDir(projectDir)

	prompt, err := mgr.BuildPrompt("hello")
	if err != nil {
		t.Fatalf("build prompt failed: %v", err)
	}
	if !strings.Contains(prompt, "项目级执行规范文件（自动检索）") {
		t.Fatalf("prompt missing project guide section: %s", prompt)
	}
	if !strings.Contains(prompt, filepath.Join(projectDir, "AGENTS.md")) {
		t.Fatalf("prompt missing AGENTS.md path: %s", prompt)
	}
	if !strings.Contains(prompt, "规则A：先测后提。") {
		t.Fatalf("prompt missing AGENTS.md content: %s", prompt)
	}
	if !strings.Contains(prompt, filepath.Join(projectDir, "GEMINI.md")) {
		t.Fatalf("prompt missing GEMINI.md path: %s", prompt)
	}
	if !strings.Contains(prompt, "规则B：先事实后判断。") {
		t.Fatalf("prompt missing GEMINI.md content: %s", prompt)
	}
}

func TestManagerBuildPrompt_ProjectGuideFilesMissing(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "project")
	memoryDir := filepath.Join(projectDir, ".memory")
	if err := os.MkdirAll(memoryDir, 0o755); err != nil {
		t.Fatalf("create memory dir failed: %v", err)
	}

	mgr := NewManager(memoryDir)
	mgr.SetProjectDir(projectDir)

	prompt, err := mgr.BuildPrompt("hello")
	if err != nil {
		t.Fatalf("build prompt failed: %v", err)
	}
	if !strings.Contains(prompt, "当前未发现上述文件") {
		t.Fatalf("prompt should mention missing project guide files: %s", prompt)
	}
}

func TestManagerSaveInteraction_DelegatedToLLMNoSystemWrite(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "memory")
	mgr := NewManager(dir)

	changed, err := mgr.SaveInteraction("请记住：偏好中文", "好的", false)
	if err != nil {
		t.Fatalf("save interaction failed: %v", err)
	}
	if changed {
		t.Fatal("save interaction should not report memory changed by system")
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("save interaction should not create memory dir, stat err=%v", err)
	}
}

func TestManagerAppendDailySummary_CreatesAndAppendsDailyFile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "memory")
	mgr := NewManager(dir)

	at := time.Date(2026, 2, 20, 10, 30, 0, 0, time.Local)
	if err := mgr.AppendDailySummary("chat_id:oc_chat", "- 要点1\n- 要点2", at); err != nil {
		t.Fatalf("append daily summary failed: %v", err)
	}
	if err := mgr.AppendDailySummary("", "", at.Add(time.Minute)); err != nil {
		t.Fatalf("append empty summary failed: %v", err)
	}

	dailyPath := filepath.Join(dir, ShortTermDirName, "2026-02-20.md")
	data, err := os.ReadFile(dailyPath)
	if err != nil {
		t.Fatalf("read daily file failed: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "session: chat_id:oc_chat") {
		t.Fatalf("missing session key in daily file: %s", content)
	}
	if !strings.Contains(content, "session: unknown") {
		t.Fatalf("missing fallback session key in daily file: %s", content)
	}
	if !strings.Contains(content, "空闲摘要：") {
		t.Fatalf("missing summary label in daily file: %s", content)
	}
	if !strings.Contains(content, "无重要新增信息") {
		t.Fatalf("missing empty-summary fallback text: %s", content)
	}
	if strings.Count(content, "## ") != 2 {
		t.Fatalf("expected two appended summary entries, got content: %s", content)
	}
}
