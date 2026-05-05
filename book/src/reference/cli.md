# CLI Commands

Alice provides several CLI subcommands for different operations.

---

## Main Process

### `alice --feishu-websocket`

Start the full Feishu connector runtime. Connects to Feishu WebSocket and processes live messages.

```bash
alice --feishu-websocket
```

### `alice --runtime-only`

Start in runtime-only mode. The local HTTP API and automation engine run, but the Feishu WebSocket does not start.

```bash
alice --runtime-only
```

### `alice-headless --runtime-only`

Headless runtime-only binary. Explicitly cannot start the Feishu connector.

```bash
alice-headless --runtime-only
```

> `alice-headless` will error if invoked with `--feishu-websocket`.

---

## Global Flags

| Flag | Description |
|------|-------------|
| `--alice-home <path>` | Override the default runtime home directory |
| `--config <path>` | Path to config.yaml (default: `<alice_home>/config.yaml`) |
| `--log-level <level>` | Override log level (`debug`, `info`, `warn`, `error`) |
| `--version` | Print version and exit |

Environment variable `ALICE_HOME` also overrides the default home directory.

---

## `alice setup`

Initialize the Alice runtime environment.

```bash
alice setup
```

What it does:
1. Creates the directory structure under `~/.alice/`
2. Writes a starter `config.yaml` (based on `config.example.yaml`)
3. Syncs bundled skills to `${ALICE_HOME}/skills/`
4. On Linux: registers a systemd user unit at `~/.config/systemd/user/alice.service`
5. Installs the OpenCode delegate plugin at `~/.config/opencode/plugins/alice-delegate.js`

Run this once after installation.

---

## `alice delegate`

Send a one-shot prompt to a configured LLM backend.

```bash
alice delegate --provider <name> --prompt "<text>"
```

### Options

| Flag | Description |
|------|-------------|
| `--provider <name>` | Backend: `opencode`, `codex`, `claude`, `gemini`, `kimi` |
| `--prompt <text>` | Prompt text (required) |
| `--model <name>` | Override the default model |
| `--workspace <path>` | Override the working directory |

### Examples

```bash
alice delegate --provider codex --prompt "Fix the null check in auth.go"
alice delegate --provider claude --prompt "Review this diff" < changes.patch
```

---

## `alice runtime message`

Send messages via the runtime API.

```bash
alice runtime message image <path> [--caption <text>]
alice runtime message file <path> [--filename <name>] [--caption <text>]
```

| Subcommand | Description |
|------------|-------------|
| `image <path>` | Upload and send an image |
| `file <path>` | Upload and send a file |

| Flag | Description |
|------|-------------|
| `--caption <text>` | Optional caption text |
| `--filename <name>` | Override file display name (file only) |

---

## `alice runtime automation`

Manage automation tasks via the runtime API.

```bash
alice runtime automation list [--status <status>] [--limit <n>]
alice runtime automation create <payload>
alice runtime automation get <task-id>
alice runtime automation update <task-id> <payload>
alice runtime automation delete <task-id>
```

| Subcommand | Description |
|------------|-------------|
| `list` | List automation tasks |
| `create <json>` | Create a task from JSON payload |
| `get <id>` | Get a single task |
| `update <id> <json>` | Update a task with JSON merge-patch |
| `delete <id>` | Delete a task |

| Flag (list) | Description |
|-------------|-------------|
| `--status` | Filter by status: `active`, `completed`, `cancelled` |
| `--limit` | Items per page |

---

## `alice runtime goal`

Manage the active goal for a conversation scope.

```bash
alice runtime goal get
alice runtime goal create <description>
alice runtime goal pause
alice runtime goal resume
alice runtime goal complete
alice runtime goal delete
```

| Subcommand | Description |
|------------|-------------|
| `get` | Get the current active goal |
| `create <desc>` | Create a new goal |
| `pause` | Pause the active goal |
| `resume` | Resume a paused goal |
| `complete` | Mark the active goal as completed |
| `delete` | Delete the active goal |

---

## `alice skills`

Manage bundled skills.

```bash
alice skills sync
alice skills list
```

| Subcommand | Description |
|------------|-------------|
| `sync` | Sync embedded bundled skills to the local skills directory |
| `list` | List installed bundled skills |

`alice skills sync` is also run automatically at startup.

---

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | General error |
| `2` | Configuration error |
| `3` | Authentication error |
