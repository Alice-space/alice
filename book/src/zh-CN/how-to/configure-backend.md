# 配置 LLM 后端

Alice 支持五种 LLM 后端。每个场景引用一个 `llm_profile`，指定使用哪个 provider、模型和设置。

## 支持的 Provider

| Provider | CLI 工具 | 备注 |
|----------|----------|-------|
| `opencode` | `opencode` | OpenCode CLI，用于 DeepSeek 及其他模型 |
| `codex` | `codex` | OpenAI Codex CLI。支持 `reasoning_effort`、`personality`、`profile` |
| `claude` | `claude` | Anthropic Claude Code CLI。默认使用流式输出 |
| `gemini` | `gemini` | Google Gemini CLI |
| `kimi` | `kimi` | Moonshot Kimi CLI |

每个 provider 需要单独安装和认证。Alice 不管理 provider 的认证。

## Profile 配置

Profile 定义在 `bots.<id>.llm_profiles` 下：

```yaml
bots:
  my_bot:
    llm_profiles:
      my_profile:
        provider: "opencode"
        model: "deepseek/deepseek-v4-pro"
        variant: "max"
        timeout_secs: 172800
        permissions:
          sandbox: "danger-full-access"
          ask_for_approval: "never"
          add_dirs: ["/data/corpus"]
```

### 通用字段

| 字段 | 全部支持 | 说明 |
|-------|-----|-------------|
| `provider` | ✓ | 后端名称：`opencode`、`codex`、`claude`、`gemini`、`kimi` |
| `command` | ✓ | CLI 二进制路径。默认为 provider 名称（如 `opencode`） |
| `timeout_secs` | ✓ | 每次运行超时（秒）。默认：172800（48 小时） |
| `model` | ✓ | 模型标识符（必填） |
| `permissions.sandbox` | ✓ | `"read-only"`、`"workspace-write"` 或 `"danger-full-access"` |
| `permissions.ask_for_approval` | ✓ | `"untrusted"`、`"on-request"` 或 `"never"` |
| `permissions.add_dirs` | ✓ | agent 可访问的额外目录 |
| `prompt_prefix` | ✓ | 每次 prompt 前添加的文本 |

### Codex 专属字段

| 字段 | 说明 |
|-------|-------------|
| `reasoning_effort` | 思考级别：`"low"`、`"medium"`、`"high"` 或 `"xhigh"` |
| `personality` | Codex CLI 配置中的命名人格预设 |
| `profile` | Codex CLI 配置中的命名子 profile |

### OpenCode 专属字段

| 字段 | 说明 |
|-------|-------------|
| `variant` | DeepSeek 变体：`"max"`、`"high"`、`"minimal"` |

## 自定义二进制路径

如果你的 CLI 二进制不在 `$PATH` 中，请指定绝对路径：

```yaml
llm_profiles:
  work:
    provider: "opencode"
    command: "/usr/local/bin/opencode"
    model: "deepseek/deepseek-v4-pro"
```

你也可以通过 `env` 扩展 `$PATH`：

```yaml
bots:
  my_bot:
    env:
      PATH: "/home/user/bin:/usr/local/bin:/usr/bin:/bin"
```

## 按 Profile 的覆盖

部分后端支持通过 `profile_overrides` 实现按 profile 的 runner 覆盖。这是一项高级功能，适用于同一个 provider 在不同场景下需要不同 CLI 配置的情况。

```yaml
llm_profiles:
  executor:
    provider: "codex"
    model: "gpt-5.4-mini"
    profile: "executor"
    profile_overrides:
      executor:
        command: "/opt/bin/codex-executor"
        provider_profile: "executor-v2"
        timeout: 3600
        exec_policy:
          sandbox: "danger-full-access"
          ask_for_approval: "never"
```

## 后端进程的环境变量

`bots.<id>` 下的 `env` 字段将环境变量传递给每个 LLM 子进程：

```yaml
bots:
  my_bot:
    env:
      HTTPS_PROXY: "http://127.0.0.1:8080"
      ALL_PROXY: "http://127.0.0.1:8080"
```

这对代理配置和 API 密钥管理特别有用。

## 示例

### OpenCode + DeepSeek（chat）

```yaml
llm_profiles:
  chat:
    provider: "opencode"
    model: "deepseek/deepseek-v4-flash"
```

### Codex + reasoning

```yaml
llm_profiles:
  work:
    provider: "codex"
    command: "codex"
    model: "gpt-5.4-mini"
    reasoning_effort: "high"
    permissions:
      sandbox: "danger-full-access"
      ask_for_approval: "never"
```

### Claude

```yaml
llm_profiles:
  work:
    provider: "claude"
    model: "claude-sonnet-4-6"
    prompt_prefix: "You are a senior software engineer. Be concise."
    permissions:
      sandbox: "danger-full-access"
      ask_for_approval: "never"
```

### Gemini

```yaml
llm_profiles:
  chat:
    provider: "gemini"
    model: "gemini-2.5-pro"
```

### Kimi

```yaml
llm_profiles:
  chat:
    provider: "kimi"
    model: "kimi-model-identifier"
```
