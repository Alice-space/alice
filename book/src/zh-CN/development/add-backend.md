# 新增 LLM Backend

本指南将带你了解如何为 Alice 新增一个 LLM provider CLI 的支持。请遵循现有后端（`codex`、`claude`、`gemini`、`kimi`、`opencode`）所使用的模式。

## 前提条件

- Provider 必须有一个 **CLI 工具**，Alice 可以作为子进程运行
- CLI 必须能通过 **stdin** 或 **CLI 参数**接受 prompt
- CLI 必须将结果输出到 **stdout**

## 第 1 步：理解 Backend 接口

核心接口在 `internal/llm/backend.go` 中：

```go
type Backend interface {
    Run(ctx context.Context, req RunRequest) (RunResult, error)
}

type RunRequest struct {
    ThreadID        string
    UserText        string
    Model           string
    // ... 其他字段
    OnProgress      ProgressFunc
    OnRawEvent      RawEventFunc
}

type RunResult struct {
    Reply        string
    NextThreadID string
    GoalDone     bool
    Usage        Usage
}
```

你的后端必须：
1. 从 `RunRequest` 构建正确的 CLI 命令
2. 将其作为子进程执行
3. 将 stdout/stderr 解析为 `RunResult`
4. 通过 `OnProgress` 流式传输中间进度
5. 通过 `ctx.Done()` 处理取消

## 第 2 步：创建 Backend 文件

创建 `internal/llm/<provider>_backend.go`。遵循 `codex_backend.go` 中的模式：

```go
package llm

import (
    "context"
    "os/exec"
)

type myProviderBackend struct {
    config MyProviderConfig
}

func newMyProviderBackend(cfg MyProviderConfig) *myProviderBackend {
    return &myProviderBackend{config: cfg}
}

func (b *myProviderBackend) Run(ctx context.Context, req RunRequest) (RunResult, error) {
    // 1. 构建命令
    args := []string{"run", "--model", req.Model}
    if req.ThreadID != "" {
        args = append(args, "--continue", req.ThreadID)
    }
    cmd := exec.CommandContext(ctx, b.config.Command, args...)
    cmd.Dir = req.WorkspaceDir
    cmd.Env = mergeEnv(b.config.Env)

    // 2. 将用户文本通过管道传入 stdin
    stdin, _ := cmd.StdinPipe()
    go func() {
        defer stdin.Close()
        io.WriteString(stdin, req.UserText)
    }()

    // 3. 流式读取和解析输出
    stdout, _ := cmd.StdoutPipe()
    // ... 从 stdout 解析 JSON-lines ...
    // ... 为中间消息调用 req.OnProgress ...

    // 4. 运行
    err := cmd.Run()

    // 5. 返回结果
    return RunResult{
        Reply:        finalReply,
        NextThreadID: nextThreadID,
        Usage:        usage,
    }, err
}
```

## 第 3 步：添加配置

在 `internal/llm/factory.go` 中添加配置结构体和 provider 常量：

```go
const ProviderMyProvider = "myprovider"

type MyProviderConfig struct {
    Command      string
    Timeout      time.Duration
    Model        string
    Env          map[string]string
    WorkspaceDir string
    ProfileOverrides map[string]ProfileRunnerConfig
}
```

## 第 4 步：在 Factory 中注册

在 `factory.go` 的 `NewProvider` 中添加你的 provider：

```go
func NewProvider(cfg FactoryConfig) (Provider, error) {
    provider := normalizeProvider(cfg.Provider)
    switch provider {
    case ProviderCodex:
        return providerBundle{backend: newCodexBackend(cfg.Codex)}, nil
    case ProviderClaude:
        return providerBundle{backend: newClaudeBackend(cfg.Claude)}, nil
    case ProviderMyProvider:                                // NEW
        return providerBundle{backend: newMyProviderBackend(cfg.MyProvider)}, nil  // NEW
    default:
        return nil, fmt.Errorf("unsupported llm_provider %q", provider)
    }
}
```

同时将字段添加到 `FactoryConfig`：

```go
type FactoryConfig struct {
    Provider   string
    Codex      CodexConfig
    Claude     ClaudeConfig
    Gemini     GeminiConfig
    Kimi       KimiConfig
    OpenCode   OpenCodeConfig
    MyProvider MyProviderConfig   // NEW
}
```

## 第 5 步：从 config.yaml 接入配置

在 `internal/config` 中，扩展 LLM profile 以接受新 provider。Profile 配置应映射到你的 `MyProviderConfig` 字段（Command、Timeout、Model、Env 等）。

## 第 6 步：添加示例配置

在 `config.example.yaml` 中添加 profile 示例：

```yaml
# 示例：MyProvider profile。
# chat_myprovider:
#   provider: "myprovider"
#   command: "myprovider"
#   model: "myprovider-model-v1"
#   permissions:
#     sandbox: "workspace-write"
#     ask_for_approval: "never"
```

## 第 7 步：编写测试

创建 `internal/llm/<provider>_backend_test.go`。至少测试：

- 使用不同请求字段构建命令
- 超时处理
- 进度回调递送
- 通过 context 取消
- 无效输出的错误处理

参考 `codex_backend_test.go` 或 `opencode_appserver_driver_test.go` 中的现有测试模式。

## 第 8 步：交互式 Session 支持（可选）

部分后端支持长时间运行的交互式 session，可以在不重启子进程的情况下注入新输入。如果你的 provider 支持此功能：

1. 实现 `InteractiveProviderSession` 模式（参见 `claude_stream_driver.go` 或 `opencode_appserver_driver.go`）
2. 将交互模式接入主 `Run` 方法
3. 添加 `DisableStream*` 逃生舱用于回退

## 实现清单

- [ ] `internal/llm/<provider>_backend.go` — 后端实现
- [ ] `internal/llm/factory.go` — provider 常量 + 配置结构体 + switch case
- [ ] `internal/config` — LLM profile 配置接入
- [ ] `config.example.yaml` — 示例 profile
- [ ] `internal/llm/<provider>_backend_test.go` — 测试
- [ ] `book/src/reference/configuration.md` — 更新 provider 列表
- [ ] `book/src/how-to/configure-backend.md` — 添加 provider 示例

## 参考实现

研究以下现有后端以了解模式：

| Backend | 文件 | 备注 |
|---------|------|-------|
| Codex | `codex_backend.go` | 完整实现，包含 reasoning、personality、idle timeout |
| Claude | `claude_stream_driver.go` | 流式交互 session |
| OpenCode | `opencode_appserver_driver.go` | Appserver 模式与持久化服务器 |
| Kimi | `kimi_wire_driver.go` | 线协议驱动 |
