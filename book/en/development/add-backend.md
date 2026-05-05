# Adding a New LLM Backend

This guide walks through adding support for a new LLM provider CLI to Alice. Follow the same pattern used by the existing backends (`codex`, `claude`, `gemini`, `kimi`, `opencode`).

## Prerequisites

- The provider must have a **CLI tool** that Alice can run as a subprocess
- The CLI must accept a prompt via **stdin** or **CLI flags**
- The CLI must output results to **stdout**

## Step 1: Understand the Backend Interface

The core interface is in `internal/llm/backend.go`:

```go
type Backend interface {
    Run(ctx context.Context, req RunRequest) (RunResult, error)
}

type RunRequest struct {
    ThreadID        string
    UserText        string
    Model           string
    // ... other fields
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

Your backend must:
1. Build the correct CLI command from `RunRequest`
2. Execute it as a subprocess
3. Parse stdout/stderr into `RunResult`
4. Stream intermediate progress via `OnProgress`
5. Handle `ctx.Done()` for cancellation

## Step 2: Create the Backend File

Create `internal/llm/<provider>_backend.go`. Follow the pattern in `codex_backend.go`:

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
    // 1. Build command
    args := []string{"run", "--model", req.Model}
    if req.ThreadID != "" {
        args = append(args, "--continue", req.ThreadID)
    }
    cmd := exec.CommandContext(ctx, b.config.Command, args...)
    cmd.Dir = req.WorkspaceDir
    cmd.Env = mergeEnv(b.config.Env)

    // 2. Pipe user text to stdin
    stdin, _ := cmd.StdinPipe()
    go func() {
        defer stdin.Close()
        io.WriteString(stdin, req.UserText)
    }()

    // 3. Stream and parse output
    stdout, _ := cmd.StdoutPipe()
    // ... parse JSON-lines from stdout ...
    // ... call req.OnProgress for intermediate messages ...

    // 4. Run
    err := cmd.Run()

    // 5. Return result
    return RunResult{
        Reply:        finalReply,
        NextThreadID: nextThreadID,
        Usage:        usage,
    }, err
}
```

## Step 3: Add Configuration

Add a config struct and provider constant in `internal/llm/factory.go`:

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

## Step 4: Register in the Factory

Add your provider to `NewProvider` in `factory.go`:

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

Also add the field to `FactoryConfig`:

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

## Step 5: Wire Configuration from config.yaml

In `internal/config`, extend the LLM profile to accept the new provider. The profile config should map to your `MyProviderConfig` fields (Command, Timeout, Model, Env, etc.).

## Step 6: Add Example Config

Add a profile example in `config.example.yaml`:

```yaml
# Example: MyProvider profile.
# chat_myprovider:
#   provider: "myprovider"
#   command: "myprovider"
#   model: "myprovider-model-v1"
#   permissions:
#     sandbox: "workspace-write"
#     ask_for_approval: "never"
```

## Step 7: Write Tests

Create `internal/llm/<provider>_backend_test.go`. Test at minimum:

- Command construction with different request fields
- Timeout handling
- Progress callback delivery
- Cancellation via context
- Error handling for invalid output

Use the existing test patterns in `codex_backend_test.go` or `opencode_appserver_driver_test.go` as reference.

## Step 8: Interactive Session Support (Optional)

Some backends support long-running interactive sessions where new input can be injected without restarting the subprocess. If your provider supports this:

1. Implement the `InteractiveProviderSession` pattern (see `claude_stream_driver.go` or `opencode_appserver_driver.go`)
2. Wire the interactive mode into the main `Run` method
3. Add a `DisableStream*` escape hatch for fallback

## Implementation Checklist

- [ ] `internal/llm/<provider>_backend.go` — backend implementation
- [ ] `internal/llm/factory.go` — provider constant + config struct + switch case
- [ ] `internal/config` — LLM profile config wiring
- [ ] `config.example.yaml` — example profile
- [ ] `internal/llm/<provider>_backend_test.go` — tests
- [ ] `book/src/reference/configuration.md` — update provider list
- [ ] `book/src/how-to/configure-backend.md` — add provider example

## Reference Implementations

Study these existing backends for patterns:

| Backend | File | Notes |
|---------|------|-------|
| Codex | `codex_backend.go` | Full implementation with reasoning, personality, idle timeout |
| Claude | `claude_stream_driver.go` | Streaming interactive sessions |
| OpenCode | `opencode_appserver_driver.go` | Appserver mode with persistent server |
| Kimi | `kimi_wire_driver.go` | Wire-protocol driver |
