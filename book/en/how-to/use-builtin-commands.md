# Use Built-in Commands

Alice provides several slash commands that bypass the LLM and are handled directly by the connector. All commands work in both group chats and direct messages.

## `/help`

Displays the built-in command help card with all available commands.

```
/help
```

## `/status`

Shows a status card with:
- Total sessions and usage counters
- Active automation tasks
- Current LLM backend and session details

```
/status
```

## `/clear`

Clears the backend session binding for the current conversation. The next message starts a fresh conversation on a new backend session with no prior context.

```
/clear
```

When to use:

- **Direct messages**: clears the DM's backend session binding.
- **Group chats with `chat` scene enabled**: clears the chat-scene backend session binding for the group.
- After switching providers (for example, migrating from codex to opencode): the old backend session id cannot be resumed by the new provider — `/clear` unbinds it so the next message starts a fresh session.

> Does not affect `work` scene threads. Each `work` thread has its own session; to rebind one, use `/session <backend-session-id>` inside that thread.

## `/stop`

Immediately cancels the currently running LLM call for the active session.

```
/stop
```

Use this when the agent is stuck in a loop or taking too long. The bot will acknowledge the stop and become available for new messages.

## `/session`

Binds a Feishu work thread to an existing backend session. Useful for resuming long-running tasks after a restart.

```
/session <backend-session-id>
/session <backend-session-id> Continue the review
```

- Without an instruction: binds the session, no LLM call
- With an instruction: binds the session and immediately calls the LLM with the instruction

> Only works in `work` scene threads.

## `/cd`, `/ls`, `/pwd`

Inspect and change the current working directory for the active work session:

```
/pwd               # Show current directory
/ls                # List files
/ls internal/      # List files in subdirectory
/cd /tmp/build     # Change directory
```

These commands only affect `work` sessions. The directory change persists for the duration of the session.

## Command Precedence

When a message starts with `/`, Alice checks for built-in commands before routing to the LLM:

1. Built-in command match → handle directly
2. No match → route to scene (LLM handles it)

To force a message starting with `/` to go to the LLM, prefix it with a space or use the work trigger:

```
 /some-custom-command     # Space before slash → LLM path
@Alice #work /some-cmd    # Work trigger → LLM path
```
