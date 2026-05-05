# 使用 `alice delegate`

`alice delegate` 子命令从命令行向任意已配置的 LLM 后端发送一次性 prompt。

## 基本用法

```bash
alice delegate --provider codex --prompt "Refactor the auth module to use JWT"
alice delegate --provider claude --prompt "Review this code for security issues"
alice delegate --provider opencode --prompt "Explain how DNS resolution works"
```

## 参数

| 参数 | 说明 |
|------|-------------|
| `--provider` | LLM 后端：`opencode`、`codex`、`claude`、`gemini`、`kimi` |
| `--prompt` | prompt 文本（必填） |
| `--model` | 覆盖默认模型 |
| `--workspace` | 覆盖工作目录 |

## 管道输入

通过 stdin 发送 diff 或文件内容：

```bash
cat diff.patch | alice delegate --provider claude --prompt "Review this PR diff"
alice delegate --provider codex --prompt "Summarize this log" < /var/log/app.log
```

## OpenCode 插件集成

`alice setup` 会将插件写入 `~/.config/opencode/plugins/alice-delegate.js`。一旦就位，OpenCode agent（包括 DeepSeek）会自动获得两个额外工具：

- `codex` — 将子任务委托给 Codex
- `claude` — 将子任务委托给 Claude

无需额外配置。OpenCode 会自动从该目录加载插件。

这是 `alice delegate` 的主要用途：让 OpenCode agent 可以将并行工作发散出去，或将专项任务委托给其他 LLM 后端。

## 连接方式

`alice delegate` 使用与 Alice 主运行时相同的 `llm_profiles` 配置。默认使用第一个 bot 下名为 `delegate` 的 profile。该 profile 决定了委托运行时的模型、权限和环境变量。

```yaml
bots:
  my_bot:
    llm_profiles:
      delegate:
        provider: "claude"
        model: "claude-sonnet-4-6"
        permissions:
          sandbox: "workspace-write"
          ask_for_approval: "never"
```

## 示例

### 快速代码审查
```bash
alice delegate --provider claude --prompt "Check this function for bugs and suggest improvements" < src/auth.go
```

### 重构
```bash
alice delegate --provider codex --prompt "Extract the database logic into a separate package"
```

### 生成文档
```bash
alice delegate --provider opencode --prompt "Generate JSDoc comments for all exported functions"
```
