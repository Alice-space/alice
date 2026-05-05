# Customize SOUL.md Persona

Each bot can have a persona document called `SOUL.md` that defines its behavior, tone, and reply preferences.

## What is SOUL.md?

`SOUL.md` is a Markdown file with YAML frontmatter. It serves two purposes:

1. **Persona**: The Markdown body is injected into the LLM prompt for `chat` scenes, shaping the bot's tone and behavior
2. **Metadata**: The YAML frontmatter controls machine-readable reply behavior

## File Location

By default, Alice looks for `SOUL.md` in the bot's `alice_home`:

```
~/.alice/bots/<bot_id>/SOUL.md
```

You can customize the path with `soul_path`:

```yaml
bots:
  my_bot:
    soul_path: "SOUL.md"            # relative to alice_home (default)
    # soul_path: "/path/to/custom/SOUL.md"  # absolute path
```

If the file doesn't exist at startup, Alice writes an embedded template from `prompts/SOUL.md.example`.

## Frontmatter Keys

```yaml
---
image_refs:
  - refs/avatar.png
  - refs/signature.jpg
output_contract:
  hidden_tags:
    - reply_will
    - motion
  reply_will_tag: reply_will
  reply_will_field: reply_will
  motion_tag: motion
  suppress_token: "[[NO_REPLY]]"
---
```

| Key | Description |
|-----|-------------|
| `image_refs` | List of local image paths the bot can reference. Paths are relative to the directory containing `SOUL.md` |
| `output_contract.hidden_tags` | Tags in the bot's reply that Alice strips before sending to Feishu |
| `output_contract.reply_will_tag` | Tag marking the bot's intent to reply |
| `output_contract.reply_will_field` | Field name within the tag |
| `output_contract.motion_tag` | Tag for motion/animation cues |
| `output_contract.suppress_token` | If the bot outputs this token, Alice suppresses the reply entirely |

## Full Example

```markdown
---
image_refs:
  - refs/avatar.png
output_contract:
  hidden_tags:
    - reply_will
    - motion
  reply_will_tag: reply_will
  reply_will_field: reply_will
  motion_tag: motion
  suppress_token: "[[NO_REPLY]]"
---

# Persona

You are Alice, a helpful engineering assistant. You speak concisely in Chinese
mixed with English technical terms. You never use emoji unless explicitly asked.

## Rules

- Keep code snippets under 30 lines
- Prefer explaining the approach before showing code
- Never apologize — just fix the problem
```

## When is SOUL.md Applied?

- **Chat scene**: The full body is prepended to the prompt, and frontmatter is parsed by Alice for reply control
- **Work scene**: SOUL.md is intentionally **skipped**. Work mode is for task execution, not persona roleplay

## Testing Your Persona

1. Edit `SOUL.md`
2. Restart Alice (multi-bot mode requires restart; single-bot mode supports hot reload)
3. Send a message in a chat scene — the bot should reflect the updated persona
4. Use `/clear` to reset the conversation if needed
