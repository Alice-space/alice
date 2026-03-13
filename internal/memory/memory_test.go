package memory

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Alice-space/alice/internal/prompting"
)

func newTestManager(dir string) *Manager {
	return NewManager(dir, prompting.NewLoader(filepath.Join("..", "..", "prompts")))
}

func TestManagerInit_RequiresMemoryDir(t *testing.T) {
	mgr := newTestManager("   ")
	err := mgr.Init()
	if err == nil || !strings.Contains(err.Error(), "memory dir is empty") {
		t.Fatalf("expected memory dir validation error, got: %v", err)
	}
}

func TestManagerInit_CreatesMemoryDirStructure(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "memory")

	mgr := newTestManager(dir)
	if err := mgr.Init(); err != nil {
		t.Fatalf("init memory failed: %v", err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("init should create memory dir, err=%v", err)
	}
	shortTermDir := filepath.Join(dir, GlobalDirName, ShortTermDirName)
	if info, err := os.Stat(shortTermDir); err != nil || !info.IsDir() {
		t.Fatalf("init should create global short-term memory dir, err=%v", err)
	}
	scopeRootDir := filepath.Join(dir, ScopeRootDirName)
	if info, err := os.Stat(scopeRootDir); err != nil || !info.IsDir() {
		t.Fatalf("init should create scope root dir, err=%v", err)
	}
	longTermFile := filepath.Join(dir, GlobalDirName, LongTermFileName)
	if info, err := os.Stat(longTermFile); err != nil || info.IsDir() {
		t.Fatalf("init should create global long-term memory file, err=%v", err)
	}
}

func TestManagerBuildPrompt_ContainsLongTermAndPaths(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memory")
	now := time.Date(2026, 2, 19, 11, 30, 0, 0, time.UTC)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create dir failed: %v", err)
	}

	mgr := newTestManager(dir)
	mgr.now = func() time.Time { return now }

	if err := mgr.Init(); err != nil {
		t.Fatalf("init memory failed: %v", err)
	}
	globalLongPath := filepath.Join(dir, GlobalDirName, LongTermFileName)
	if err := os.WriteFile(globalLongPath, []byte("全局规则：回答要简洁。"), 0o644); err != nil {
		t.Fatalf("write global long-term failed: %v", err)
	}
	scopeLongPath := filepath.Join(dir, ScopeRootDirName, "chat_id", "oc_chat", LongTermFileName)
	if err := os.MkdirAll(filepath.Dir(scopeLongPath), 0o755); err != nil {
		t.Fatalf("create scope dir failed: %v", err)
	}
	if err := os.WriteFile(scopeLongPath, []byte("群里偏好：关注连接器稳定性。"), 0o644); err != nil {
		t.Fatalf("write scope long-term failed: %v", err)
	}
	shortPath := filepath.Join(dir, ScopeRootDirName, "chat_id", "oc_chat", ShortTermDirName, "2026-02-19.md")
	if err := os.MkdirAll(filepath.Dir(shortPath), 0o755); err != nil {
		t.Fatalf("create short-term dir failed: %v", err)
	}
	if err := os.WriteFile(shortPath, []byte("今天提到：关注连接器稳定性。"), 0o644); err != nil {
		t.Fatalf("write short-term failed: %v", err)
	}

	prompt, err := mgr.BuildPrompt("chat_id:oc_chat", "帮我总结下")
	if err != nil {
		t.Fatalf("build prompt failed: %v", err)
	}
	if !strings.Contains(prompt, "全局规则：回答要简洁。") {
		t.Fatalf("prompt missing global long-term memory: %s", prompt)
	}
	if !strings.Contains(prompt, "群里偏好：关注连接器稳定性。") {
		t.Fatalf("prompt missing scoped long-term memory: %s", prompt)
	}
	if !strings.Contains(prompt, globalLongPath) {
		t.Fatalf("prompt missing global long-term file location: %s", prompt)
	}
	if !strings.Contains(prompt, scopeLongPath) {
		t.Fatalf("prompt missing scoped long-term file location: %s", prompt)
	}
	if strings.Contains(prompt, "今天提到：关注连接器稳定性。") {
		t.Fatalf("prompt should not inline short-term memory: %s", prompt)
	}
	if !strings.Contains(prompt, filepath.Join(dir, ScopeRootDirName, "chat_id", "oc_chat", ShortTermDirName)) {
		t.Fatalf("prompt missing short-term dir location: %s", prompt)
	}
	if !strings.Contains(prompt, "系统仅会在会话空闲超时后自动追加“空闲摘要”") {
		t.Fatalf("prompt missing idle-summary instruction: %s", prompt)
	}
	if !strings.Contains(prompt, "不要读取或修改其他群聊、私聊的记忆目录。") {
		t.Fatalf("prompt missing isolation rule: %s", prompt)
	}
	if !strings.Contains(prompt, "帮我总结下") {
		t.Fatalf("prompt missing user message: %s", prompt)
	}
}

func TestManagerBuildPrompt_LongTermMissingIsEmpty(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memory")
	mgr := newTestManager(dir)

	prompt, err := mgr.BuildPrompt("chat_id:oc_chat", "hello")
	if err != nil {
		t.Fatalf("build prompt failed: %v", err)
	}
	if !strings.Contains(prompt, "（空）") {
		t.Fatalf("prompt should include empty long-term memory marker: %s", prompt)
	}
}

func TestManagerBuildPrompt_DoesNotIncludeProjectGuideFiles(t *testing.T) {
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

	mgr := newTestManager(memoryDir)

	prompt, err := mgr.BuildPrompt("chat_id:oc_chat", "hello")
	if err != nil {
		t.Fatalf("build prompt failed: %v", err)
	}
	if strings.Contains(prompt, "项目级执行规范文件（自动检索）") {
		t.Fatalf("prompt should not include project guide section: %s", prompt)
	}
	if strings.Contains(prompt, "AGENTS.md") {
		t.Fatalf("prompt should not include AGENTS.md: %s", prompt)
	}
	if strings.Contains(prompt, "GEMINI.md") {
		t.Fatalf("prompt should not include GEMINI.md: %s", prompt)
	}
	if strings.Contains(prompt, "规则A：先测后提。") || strings.Contains(prompt, "规则B：先事实后判断。") {
		t.Fatalf("prompt should not include project guide contents: %s", prompt)
	}
}

func TestManagerSaveInteraction_DelegatedToLLMNoSystemWrite(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "memory")
	mgr := newTestManager(dir)

	changed, err := mgr.SaveInteraction("chat_id:oc_chat", "请记住：偏好中文", "好的", false)
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
	mgr := newTestManager(dir)

	at := time.Date(2026, 2, 20, 10, 30, 0, 0, time.Local)
	if err := mgr.AppendDailySummary("chat_id:oc_chat", "chat_id:oc_chat|thread:omt_1", "- 要点1\n- 要点2", at); err != nil {
		t.Fatalf("append daily summary failed: %v", err)
	}
	if err := mgr.AppendDailySummary("chat_id:oc_chat", "", "", at.Add(time.Minute)); err != nil {
		t.Fatalf("append empty summary failed: %v", err)
	}

	dailyPath := filepath.Join(dir, ScopeRootDirName, "chat_id", "oc_chat", ShortTermDirName, "2026-02-20.md")
	data, err := os.ReadFile(dailyPath)
	if err != nil {
		t.Fatalf("read daily file failed: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "session: chat_id:oc_chat|thread:omt_1") {
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
