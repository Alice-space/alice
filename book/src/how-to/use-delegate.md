# Use `alice delegate`

The `alice delegate` subcommand sends a one-shot prompt to any configured LLM backend from the command line.

## Basic Usage

```bash
alice delegate --provider codex --prompt "Refactor the auth module to use JWT"
alice delegate --provider claude --prompt "Review this code for security issues"
alice delegate --provider opencode --prompt "Explain how DNS resolution works"
```

## Flags

| Flag | Description |
|------|-------------|
| `--provider` | LLM backend: `opencode`, `codex`, `claude`, `gemini`, `kimi` |
| `--prompt` | The prompt text (required) |
| `--model` | Override the default model |
| `--workspace` | Override the working directory |

## Piping Input

Send a diff or file content via stdin:

```bash
cat diff.patch | alice delegate --provider claude --prompt "Review this PR diff"
alice delegate --provider codex --prompt "Summarize this log" < /var/log/app.log
```

## OpenCode Plugin Integration

`alice setup` writes a plugin to `~/.config/opencode/plugins/alice-delegate.js`. Once present, OpenCode agents (including DeepSeek) automatically gain two extra tools:

- `codex` — delegates a subtask to Codex
- `claude` — delegates a subtask to Claude

No extra configuration is needed. OpenCode loads plugins from that directory automatically.

This is the primary use case for `alice delegate`: allowing an OpenCode agent to fan out parallel work or delegate specialized tasks to other LLM backends.

## How It Connects

`alice delegate` uses the same `llm_profiles` configuration as the main Alice runtime. A profile named `delegate` under the first bot is used by default. The profile determines the model, permissions, and environment variables for the delegated run.

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

## Examples

### Quick code review
```bash
alice delegate --provider claude --prompt "Check this function for bugs and suggest improvements" < src/auth.go
```

### Refactoring
```bash
alice delegate --provider codex --prompt "Extract the database logic into a separate package"
```

### Documentation generation
```bash
alice delegate --provider opencode --prompt "Generate JSDoc comments for all exported functions"
```
