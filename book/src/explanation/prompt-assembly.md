# Prompt Assembly

How Alice constructs the prompt text sent to LLM backends.

## Template System

Alice uses Go `text/template` with [Sprig](https://masterminds.github.io/sprig/) functions for prompt templating. Templates are `.md.tmpl` files.

### Template Loading

```
1. Check disk: <prompt_dir>/<template>.tmpl
2. If not found, use embedded (compiled into binary)
```

Disk files override embedded templates, allowing per-bot customization.

### Template Files

All templates live under `prompts/`:

| Template | Purpose |
|----------|---------|
| `connector/bot_soul.md.tmpl` | Injects SOUL.md body into the prompt |
| `connector/current_user_input.md.tmpl` | Formats the current user message |
| `connector/reply_context.md.tmpl` | Adds context from a replied-to message |
| `connector/runtime_skill_hint.md.tmpl` | Describes available bundled skills |
| `connector/synthetic_mention.md.tmpl` | Formats synthetic @mentions |
| `connector/help.md.tmpl` | The `/help` command response |
| `llm/initial_prompt.md.tmpl` | First-turn system instructions |
| `goals/goal_start.tmpl` | Goal initialization prompt |
| `goals/goal_continue.tmpl` | Goal continuation prompt |
| `goals/goal_timeout.tmpl` | Goal timeout notification |

## Template Variables

Templates have access to the full `Job` context and session metadata. Key variables include:

| Variable | Description |
|----------|-------------|
| `.UserText` | The user's message text |
| `.BotName` | Display name of the responding bot |
| `.SenderName` | Name of the user who sent the message |
| `.MentionedUsers` | List of users @mentioned in the message |
| `.ReplyContext` | Text of the message being replied to |
| `.Attachments` | Inbound attachment metadata |
| `.Scene` | `"chat"` or `"work"` |
| `.SessionKey` | Canonical session identifier |
| `.SoulBody` | SOUL.md body content (chat only) |
| `.SkillDescriptions` | Descriptions of enabled bundled skills |

## First Turn vs Resume

The critical difference in prompt assembly:

### First Turn (No Existing Thread)

- Full initial prompt is assembled
- Includes system instructions (`initial_prompt.md.tmpl`)
- In chat scenes: SOUL.md body is prepended
- Identity hints (`Name`说：, @mention rules) unless `disable_identity_hints: true`

### Resume (Existing Provider Thread)

- Only the current user's message text is sent
- Alice relies on the **provider-side thread/session** to hold prior context
- No system prompt, no SOUL.md, no identity hints
- This is more efficient — the backend model already has full conversation history

## SOUL.md Injection

SOUL.md serves two purposes controlled by the scene:

### Chat Scene

1. Alice reads the file, parses the YAML frontmatter
2. Frontmatter keys (`image_refs`, `output_contract`) are consumed by Alice for reply control
3. The remaining Markdown body is prepended to the first-turn prompt via `bot_soul.md.tmpl`

### Work Scene

SOUL.md is intentionally **skipped** entirely. Work mode is for task execution — persona injection would interfere with tool use and code generation.

## Identity Hints

When `disable_identity_hints: false` (default), Alice formats messages with identity context:

```
张三说：fix the login timeout
```

When `disable_identity_hints: true`, the raw message text is passed through as-is:

```
fix the login timeout
```

## Prompt Prefix

Each LLM profile can have a `prompt_prefix`:

```yaml
llm_profiles:
  work:
    prompt_prefix: "You are a senior Go engineer. Be concise, use idiomatic patterns."
```

This text is prepended to every prompt for that profile, including resumed sessions.

## Prompts and Debugging

With `log_level: debug`, Alice logs the fully rendered prompt sent to each backend. Debug traces include:

- Provider name
- Model and profile
- Thread/session ID
- The complete rendered input text
- Observed tool activity and final output

> Warning: Rendered prompts may contain SOUL.md content and conversation history. Avoid sharing debug logs publicly.
