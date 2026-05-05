# Troubleshooting

Common problems and solutions for running Alice.

## Bot doesn't respond in group chats

**Check scene routing:**
- Verify `group_scenes.chat.enabled` is `true`
- If both scenes are disabled, check `trigger_mode` (should be `at` or `prefix`)

**Check bot identity:**
- Bot `open_id` is fetched automatically at startup now — no manual `feishu_bot_open_id` config needed
- Verify `feishu_app_id` and `feishu_app_secret` are correct

**Check logs:**
```bash
# Look for WebSocket connection status
grep "long connection" ~/.alice/log/*.log
# Look for auth errors
grep "error" ~/.alice/log/*.log | head -20
```

## Work mode never triggers

- Verify `group_scenes.work.enabled` is `true`
- Verify `trigger_tag` is set (e.g., `"#work"`)
- Message must contain `@BotName #work ...` — both the @mention and the trigger tag
- The trigger tag must appear after the @mention in the same message

## Wrong model or reasoning level

- Check `llm_profiles` for the correct `provider`, `model`, and provider-specific fields
- Verify the scene points at the correct profile key:
  ```yaml
  group_scenes:
    work:
      llm_profile: "work"  # must match a key under llm_profiles
  ```
- Run the provider CLI directly to verify authentication:
  ```bash
  codex --version
  claude --version
  ```

## Skills can't send attachments or manage tasks

**Check permissions:**
```yaml
permissions:
  runtime_message: true
  runtime_automation: true
```

**Check API connectivity:**
```bash
# From the machine running Alice
curl -s -H "Authorization: Bearer <token>" http://127.0.0.1:7331/healthz
# Should return {"status":"ok"}
```

The runtime HTTP API binds to the address in `runtime_http_addr` (default `127.0.0.1:7331`). Multi-bot setups auto-increment the port.

## Configuration changes don't apply

- **Multi-bot mode**: Config hot reload is disabled. Restart Alice.
- **Single-bot mode**: Partial hot reload is supported, but not all config keys are watched.
- Always check logs after a config change:
  ```bash
  grep "config" ~/.alice/log/*.log | tail -5
  ```

## WebSocket connection errors

If you see connection failures in the logs:

1. Verify long connection mode is enabled in the Feishu Open Platform
2. Check that the app has been published and approved
3. Verify network connectivity to `open.feishu.cn` (or `open.larksuite.com` for Lark)
4. Check that `feishu_base_url` is set correctly for Lark (international) users:
   ```yaml
   feishu_base_url: "https://open.larksuite.com"
   ```

## Provider CLI not found

Alice looks for the CLI binary in `$PATH` by default. If it's not found:

1. Specify an absolute path:
   ```yaml
   llm_profiles:
     chat:
       command: "/usr/local/bin/opencode"
   ```
2. Or extend `$PATH` in the bot's env:
   ```yaml
   env:
     PATH: "/home/user/.local/bin:/usr/local/bin:/usr/bin:/bin"
   ```

## LLM runs hang indefinitely

- Check `timeout_secs` in the LLM profile (default: 48 hours)
- Use `/stop` in Feishu to cancel a running session
- Check logs for provider-specific errors:
  ```bash
  grep -E "timeout|cancelled|killed" ~/.alice/log/*.log
  ```
- For Codex, check `codex_idle_timeout_secs` settings

## Logs show nothing useful

Increase log level to `debug`:

```yaml
log_level: "debug"
```

Restart Alice. Debug mode includes:
- Provider and agent name per run
- Thread/session IDs
- Rendered input prompts
- Observed tool activity
- Final output or error

> Warning: Debug logs may contain the full rendered prompt including SOUL.md content.
