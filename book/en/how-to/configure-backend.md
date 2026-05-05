# Configure LLM Backends

Alice supports five LLM backends. Each scene references an `llm_profile` that specifies which provider, model, and settings to use.

## Supported Providers

| Provider | CLI Tool | Notes |
|----------|----------|-------|
| `opencode` | `opencode` | OpenCode CLI for DeepSeek and other models |
| `codex` | `codex` | OpenAI Codex CLI. Supports `reasoning_effort`, `personality`, `profile` |
| `claude` | `claude` | Anthropic Claude Code CLI. Streaming by default |
| `gemini` | `gemini` | Google Gemini CLI |
| `kimi` | `kimi` | Moonshot Kimi CLI |

Each provider must be installed and authenticated separately. Alice does not manage provider authentication.

## Profile Configuration

Profiles are defined under `bots.<id>.llm_profiles`:

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

### Common Fields

| Field | All | Description |
|-------|-----|-------------|
| `provider` | ✓ | Backend name: `opencode`, `codex`, `claude`, `gemini`, `kimi` |
| `command` | ✓ | Path to the CLI binary. Defaults to the provider name (e.g. `opencode`) |
| `timeout_secs` | ✓ | Per-run timeout in seconds. Default: 172800 (48 hours) |
| `model` | ✓ | Model identifier (required) |
| `permissions.sandbox` | ✓ | `"read-only"`, `"workspace-write"`, or `"danger-full-access"` |
| `permissions.ask_for_approval` | ✓ | `"untrusted"`, `"on-request"`, or `"never"` |
| `permissions.add_dirs` | ✓ | Extra directories accessible to the agent |
| `prompt_prefix` | ✓ | Text prepended to every prompt |

### Codex-Specific Fields

| Field | Description |
|-------|-------------|
| `reasoning_effort` | Thinking level: `"low"`, `"medium"`, `"high"`, or `"xhigh"` |
| `personality` | Named personality preset from Codex CLI config |
| `profile` | Named sub-profile from Codex CLI config |

### OpenCode-Specific Fields

| Field | Description |
|-------|-------------|
| `variant` | DeepSeek variant: `"max"`, `"high"`, `"minimal"` |

## Custom Binary Path

If your CLI binary is outside `$PATH`, specify the absolute path:

```yaml
llm_profiles:
  work:
    provider: "opencode"
    command: "/usr/local/bin/opencode"
    model: "deepseek/deepseek-v4-pro"
```

You can also extend `$PATH` via the `env` section:

```yaml
bots:
  my_bot:
    env:
      PATH: "/home/user/bin:/usr/local/bin:/usr/bin:/bin"
```

## Per-Profile Overrides

Some backends support per-profile runner overrides via `profile_overrides`. This is an advanced feature used when the same provider needs different CLI configurations for different scenes.

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

## Environment Variables for Backend Processes

The `env` section under `bots.<id>` passes environment variables to every LLM subprocess:

```yaml
bots:
  my_bot:
    env:
      HTTPS_PROXY: "http://127.0.0.1:8080"
      ALL_PROXY: "http://127.0.0.1:8080"
```

This is especially useful for proxy configuration and API key management.

## Examples

### OpenCode with DeepSeek (chat)

```yaml
llm_profiles:
  chat:
    provider: "opencode"
    model: "deepseek/deepseek-v4-flash"
```

### Codex with reasoning

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
