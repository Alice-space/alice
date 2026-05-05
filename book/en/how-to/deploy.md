# Deploy to Server

Run Alice as a persistent background service so it survives restarts and runs reliably.

## systemd (Linux)

`alice setup` automatically creates a systemd user unit if systemd is available.

```bash
# Start
systemctl --user start alice.service

# Enable auto-start on boot
systemctl --user enable alice.service

# Check status
systemctl --user status alice.service

# View logs
journalctl --user-unit alice.service -n 100 --no-pager
journalctl --user-unit alice.service --since "30 min ago" --no-pager

# Restart
systemctl --user restart alice.service
```

If you installed without `alice setup`, create the unit manually:

```ini
# ~/.config/systemd/user/alice.service
[Unit]
Description=Alice Feishu LLM Connector
After=network-online.target

[Service]
Type=simple
ExecStart=%h/.alice/bin/alice --feishu-websocket
Restart=on-failure
RestartSec=10
Environment=HOME=%h

[Install]
WantedBy=default.target
```

Then:
```bash
systemctl --user daemon-reload
systemctl --user start alice.service
```

## macOS

On macOS, use `launchd` or run manually.

### launchd

```xml
<!-- ~/Library/LaunchAgents/com.alice.connector.plist -->
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.alice.connector</string>
    <key>ProgramArguments</key>
    <array>
        <string>/Users/you/.alice/bin/alice</string>
        <string>--feishu-websocket</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/Users/you/.alice/log/stdout.log</string>
    <key>StandardErrorPath</key>
    <string>/Users/you/.alice/log/stderr.log</string>
</dict>
</plist>
```

```bash
launchctl load ~/Library/LaunchAgents/com.alice.connector.plist
```

### Manual

```bash
alice --feishu-websocket
```

Use `tmux` or `screen` to keep it running after logout.

## Runtime-Only Mode

For deployments that only need automation and the runtime API (no Feishu WebSocket):

```bash
alice --runtime-only
```

In headless environments:
```bash
alice-headless --runtime-only
```

> **Important**: `alice-headless` cannot start the Feishu connector. It is explicitly limited to runtime-only mode.

## Logging

Alice uses structured JSON logs via `zerolog` with daily log rotation.

```yaml
log_level: "info"          # debug | info | warn | error
log_file: ""               # empty = <ALICE_HOME>/log/YYYY-MM-DD.log
log_max_size_mb: 20        # rotate after 20 MB
log_max_backups: 5         # keep 5 rotated files
log_max_age_days: 7        # keep logs for 7 days
log_compress: false        # gzip rotated logs
```

## Health Check

The runtime API exposes a health endpoint:

```bash
curl http://127.0.0.1:7331/healthz
# {"status":"ok"}
```

## Monitoring

- `/status` command in Feishu shows usage totals and active automation tasks
- `journalctl` (systemd) or log files for structured log analysis
- Session and runtime state are persisted to JSON files for inspection

## Multi-Bot Deployments

One `alice` process can host multiple bots. All bots share the same process but each gets its own runtime directory, workspace, and queue.

```yaml
bots:
  engineering_bot:
    feishu_app_id: "cli_11111"
    # ...
  support_bot:
    feishu_app_id: "cli_22222"
    # ...
```

> Multi-bot mode disables config hot reload. Restart the process after configuration changes.
