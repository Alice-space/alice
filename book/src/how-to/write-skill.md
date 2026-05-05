# Write a Bundled Skill

Bundled skills extend Alice with script-based tools that call the Runtime HTTP API. This guide shows you how to create one.

## Skill Anatomy

A bundled skill is a directory under `skills/`:

```
skills/my-skill/
├── SKILL.md           # Skill documentation
├── scripts/
│   └── my-skill.sh    # Executable script
└── agents/
    └── openai.yaml    # OpenAI agent configuration (optional)
```

## Step 1: Create the Directory

Create your skill under `skills/` in the Alice source tree or under `${ALICE_HOME}/skills/` for local development.

## Step 2: Write SKILL.md

`SKILL.md` documents your skill for both humans and LLM agents:

```markdown
# my-skill

Sends a daily summary of active automation tasks to a specified Feishu chat.

## Usage

This skill is triggered by the automation system. It reads all active tasks
from the runtime API and sends a formatted summary card.

## Environment

Requires `ALICE_RUNTIME_API_BASE_URL` and `ALICE_RUNTIME_API_TOKEN` to be set.
```

## Step 3: Write the Script

The script runs as a subprocess. Alice injects these environment variables:

| Variable | Description |
|----------|-------------|
| `ALICE_RUNTIME_API_BASE_URL` | Base URL of the runtime API (e.g. `http://127.0.0.1:7331`) |
| `ALICE_RUNTIME_API_TOKEN` | Bearer token for API authentication |
| `ALICE_RUNTIME_BIN` | Path to the `alice` binary |
| `ALICE_RECEIVE_ID_TYPE` | Type of the receive target (e.g. `chat_id`) |
| `ALICE_RECEIVE_ID` | ID of the receive target |
| `ALICE_SOURCE_MESSAGE_ID` | ID of the triggering message (if applicable) |
| `ALICE_ACTOR_USER_ID` | Feishu user ID of the person interacting |
| `ALICE_ACTOR_OPEN_ID` | Feishu open ID of the person interacting |
| `ALICE_CHAT_TYPE` | Chat type: `group` or `p2p` |
| `ALICE_SESSION_KEY` | Canonical session key for the current conversation |

### Example Script

```bash
#!/usr/bin/env bash
set -euo pipefail

# Get all active tasks
TASKS=$(curl -sS \
  -H "Authorization: Bearer ${ALICE_RUNTIME_API_TOKEN}" \
  "${ALICE_RUNTIME_API_BASE_URL}/api/v1/automation/tasks?status=active")

# Count and format
COUNT=$(echo "$TASKS" | jq '. | length')
echo "Active tasks: $COUNT"
```

Make it executable:
```bash
chmod +x skills/my-skill/scripts/my-skill.sh
```

## Step 4: Register the Skill

Add your skill to the bot's allowed skills list:

```yaml
bots:
  my_bot:
    permissions:
      allowed_skills: ["alice-message", "alice-scheduler", "my-skill"]
```

## Runtime API Endpoints Available to Skills

Skills primarily use these endpoints:

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/api/v1/messages/image` | POST | Send an image to the chat |
| `/api/v1/messages/file` | POST | Send a file to the chat |
| `/api/v1/automation/tasks` | GET | List automation tasks |
| `/api/v1/automation/tasks` | POST | Create an automation task |
| `/api/v1/automation/tasks/:id` | GET/PATCH/DELETE | Manage a specific task |

All requests require the `Authorization: Bearer <token>` header.

## Permissions

Skills operate under the bot's runtime permissions:

```yaml
permissions:
  runtime_message: true       # Allow sending messages via API
  runtime_automation: true    # Allow managing automation tasks
```

If a permission is disabled, the corresponding API endpoints return `403 Forbidden`.

## Built-in Skills Reference

Alice ships with two bundled skills:

- **alice-message**: Sends rich messages and attachments via the runtime API
- **alice-scheduler**: Manages automation tasks from Feishu conversations

Study their source (`skills/alice-message/` and `skills/alice-scheduler/`) for real-world examples of skill structure and API usage.
