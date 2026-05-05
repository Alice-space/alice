# 部署到服务器

将 Alice 作为持久化后台服务运行，确保它能在重启后存活并可靠运行。

## systemd（Linux）

如果系统支持 systemd，`alice setup` 会自动创建 systemd 用户单元。

```bash
# 启动
systemctl --user start alice.service

# 设置开机自启
systemctl --user enable alice.service

# 查看状态
systemctl --user status alice.service

# 查看日志
journalctl --user-unit alice.service -n 100 --no-pager
journalctl --user-unit alice.service --since "30 min ago" --no-pager

# 重启
systemctl --user restart alice.service
```

如果安装时没有运行 `alice setup`，手动创建单元文件：

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

然后：
```bash
systemctl --user daemon-reload
systemctl --user start alice.service
```

## macOS

在 macOS 上，使用 `launchd` 或手动运行。

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

### 手动运行

```bash
alice --feishu-websocket
```

使用 `tmux` 或 `screen` 保持进程在登出后继续运行。

## Runtime-Only 模式

对于只需要自动化和 Runtime API（无需飞书 WebSocket）的部署：

```bash
alice --runtime-only
```

在无头环境中：
```bash
alice-headless --runtime-only
```

> **重要**：`alice-headless` 无法启动飞书连接器。它被显式限制为仅 runtime-only 模式。

## 日志

Alice 使用 `zerolog` 输出结构化 JSON 日志，支持按天滚动。

```yaml
log_level: "info"          # debug | info | warn | error
log_file: ""               # 空 = <ALICE_HOME>/log/YYYY-MM-DD.log
log_max_size_mb: 20        # 超过 20 MB 后滚动
log_max_backups: 5         # 保留 5 个滚动文件
log_max_age_days: 7        # 日志保留 7 天
log_compress: false        # 是否 gzip 压缩滚动日志
```

## 健康检查

Runtime API 暴露了一个健康检查端点：

```bash
curl http://127.0.0.1:7331/healthz
# {"status":"ok"}
```

## 监控

- 飞书中的 `/status` 命令显示用量总计和活动自动化任务
- `journalctl`（systemd）或日志文件用于结构化日志分析
- Session 和 runtime 状态持久化到 JSON 文件供检查

## 多 Bot 部署

一个 `alice` 进程可以承载多个 bot。所有 bot 共享同一进程，但各自拥有独立的 runtime 目录、工作空间和队列。

```yaml
bots:
  engineering_bot:
    feishu_app_id: "cli_11111"
    # ...
  support_bot:
    feishu_app_id: "cli_22222"
    # ...
```

> 多 bot 模式会禁用配置热重载。配置更改后需重启进程。
