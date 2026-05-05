# Quick Start

Get Alice running and responding to messages in 5 minutes.

## Prerequisites

- **Node.js** (for `npm install`) or **Go 1.25+** (for source build)
- A **Feishu app** with bot capability and long connection enabled
- At least one **LLM CLI** installed and authenticated:
  - [OpenCode](https://github.com/anomalyco/opencode)
  - [Codex](https://github.com/openai/codex)
  - [Claude](https://docs.anthropic.com/en/docs/claude-code)
  - [Gemini](https://cloud.google.com/gemini-cli)
  - [Kimi](https://github.com/moonshotai/kimi-cli)

If you haven't set up your Feishu app yet, follow the [Feishu Platform Setup](feishu-platform-setup.md) tutorial first.

## Step 1: Install

**Via npm (recommended):**
```bash
npm install -g @alice_space/alice
```

**Via installer script:**
```bash
curl -fsSL https://cdn.jsdelivr.net/gh/Alice-space/alice@main/scripts/alice-installer.sh | bash -s -- install
```

**From source:**
```bash
git clone https://github.com/Alice-space/alice.git
cd alice
go build -o bin/alice ./cmd/connector
```

## Step 2: Setup

```bash
alice setup
```

This creates the directory structure at `~/.alice/`, writes a default `config.yaml`, syncs bundled skills, and (on Linux) registers a systemd user unit.

## Step 3: Configure

Edit `~/.alice/config.yaml` and fill in at minimum:

```yaml
bots:
  my_bot:
    name: "Alice"
    feishu_app_id: "cli_xxxxxxxx"      # from Feishu Open Platform
    feishu_app_secret: "your_secret"    # from Feishu Open Platform
    llm_profiles:
      chat:
        provider: "opencode"
        model: "deepseek/deepseek-v4-flash"
      work:
        provider: "opencode"
        model: "deepseek/deepseek-v4-pro"
```

The default config ships with OpenCode profiles targeting DeepSeek models. If you use a different LLM CLI, see [Configure LLM Backends](../how-to/configure-backend.md).

## Step 4: Verify Backend Auth

Make sure your LLM CLI can authenticate:

```bash
opencode --version    # or codex, claude, etc.
```

## Step 5: Start

```bash
alice --feishu-websocket
```

You should see log output indicating the Feishu WebSocket connection and per-bot runtime initialization.

## Step 6: Test

In Feishu, find your bot in a group chat or send it a DM. Type `/help` — the bot should respond with the built-in command help card. Then try a normal message to verify the LLM backend is working.

## What's Next?

- [Configure separate chat and work scenes](../how-to/configure-chat-work.md)
- [Switch to a different LLM backend](../how-to/configure-backend.md)
- [Deploy as a persistent service](../how-to/deploy.md)
- [Understand the system model](../explanation/system-model.md)
