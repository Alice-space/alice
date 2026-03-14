# 在独立用户 Home 下隔离运行本项目（Codex 自动运行）

本文给出可直接落地的“三层隔离”方案：`独立用户 + 独立 home 目录 + systemd 沙箱`。目标是让 Codex 只在新用户自己的 home 内工作，不再使用 `/srv/codex`。

## 目标与边界

- 目标：连接器和 `codex` 进程仅在 `codexbot` 用户的 home 范围内读写。
- 边界：`systemd` 沙箱属于“加固隔离”，不是虚拟机/容器级别的绝对安全边界。

## AlmaLinux / RHEL 前置说明

本指南已按 RHEL 系命令编写（`useradd`、`wheel` 组）。  
建议先安装基础工具：

```bash
sudo dnf install -y git go
```

## 1. 新建专用用户（无 sudo）

```bash
id -u codexbot >/dev/null 2>&1 || sudo useradd -m -s /bin/bash codexbot
sudo gpasswd -d codexbot wheel 2>/dev/null || true
sudo passwd -l codexbot
```

说明：
- 不同发行版 `adduser` 参数差异很大，这里统一用 `useradd`。
- RHEL 系默认提权组通常是 `wheel`。
- `passwd -l` 会锁定密码登录，不影响 `sudo -u codexbot ...` 或 systemd 以该用户启动服务。

## 2. 在新用户 home 内创建工作目录

```bash
sudo -u codexbot -H mkdir -p /home/codexbot/.alice/.codex
sudo chmod 700 /home/codexbot /home/codexbot/.alice /home/codexbot/.alice/.codex
```

可选（建议）：
- 收紧你主账号 home 权限，避免被其他普通用户读取：

```bash
chmod 700 /home/<your-user>
```

## 3. 部署项目到 `/home/codexbot/alice`

```bash
sudo -u codexbot -H bash -lc '
  cd /home/codexbot
  git clone https://github.com/Alice-space/alice.git alice
  mkdir -p ~/.alice
  cp alice/config.example.yaml ~/.alice/config.yaml
'
```

说明：
- 如果目录已存在，可改为 `cd /home/codexbot/alice && git pull --ff-only`。

## 4. 编译连接器（二进制）

```bash
sudo -u codexbot -H bash -lc '
  cd /home/codexbot/alice
  go mod tidy
  go build -o /home/codexbot/.alice/bin/alice ./cmd/connector
'
```

如果编译时遇到网络问题（如 `go mod tidy` 超时），先设置 Go 模块参数再重试：

```bash
sudo -u codexbot -H bash -lc '
  go env -w GO111MODULE=on
  go env -w GOPROXY=https://goproxy.cn,direct
'
```

可选检查：

```bash
sudo -u codexbot -H bash -lc 'cd /home/codexbot/alice && go test ./...'
```

## 5. 以 codexbot 完成 Codex 登录（一次性）

```bash
sudo -u codexbot -H env HOME=/home/codexbot CODEX_HOME=/home/codexbot/.alice/.codex codex login
sudo -u codexbot -H env HOME=/home/codexbot CODEX_HOME=/home/codexbot/.alice/.codex codex login status
```

## 6. 调整项目配置（`~/.alice/config.yaml`）

建议至少确认：

```yaml
llm_provider: "codex"
codex_command: "/usr/local/bin/codex"
workspace_dir: "/home/codexbot/.alice/workspace"
memory_dir: "/home/codexbot/.alice/memory"
```

并确保目录归属：

```bash
sudo install -d -m 700 -o codexbot -g codexbot /home/codexbot/.alice/memory
```

## 7. 先手动验证一次

```bash
sudo -u codexbot -H env HOME=/home/codexbot CODEX_HOME=/home/codexbot/.alice/.codex \
  bash -lc '/home/codexbot/.alice/bin/alice -c /home/codexbot/.alice/config.yaml'
```

说明：
- 若提示找不到二进制，请先执行第 4 步编译。

## 8. 使用 codexbot 用户级 systemd 自动运行（推荐）

这样做的好处是：后续 `restart` 不需要 sudo，`codexbot` 自己就能重启服务。

一次性 root 初始化（仅首次）：

```bash
sudo loginctl enable-linger codexbot
```

创建用户级服务文件 `/home/codexbot/.config/systemd/user/alice-codex-connector.service`：

```bash
sudo -u codexbot -H bash -lc '
  mkdir -p ~/.config/systemd/user
  cat > ~/.config/systemd/user/alice-codex-connector.service <<'"'"'EOF'"'"'
[Unit]
Description=Alice Feishu Codex Connector (user service)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=%h
Environment=ALICE_HOME=%h/.alice
Environment=HOME=%h
Environment=CODEX_HOME=%h/.alice/.codex
ExecStart=%h/.alice/bin/alice -c %h/.alice/config.yaml
Restart=always
RestartSec=3
NoNewPrivileges=yes

[Install]
WantedBy=default.target
EOF
'
```

启动并设置开机自启：

```bash
sudo -u codexbot -H bash -lc '
  export XDG_RUNTIME_DIR=/run/user/$(id -u)
  export DBUS_SESSION_BUS_ADDRESS=unix:path=$XDG_RUNTIME_DIR/bus
  systemctl --user daemon-reload
  systemctl --user enable --now alice-codex-connector.service
  systemctl --user status --no-pager alice-codex-connector.service
'
```

查看日志：

```bash
sudo -u codexbot -H bash -lc '
  export XDG_RUNTIME_DIR=/run/user/$(id -u)
  export DBUS_SESSION_BUS_ADDRESS=unix:path=$XDG_RUNTIME_DIR/bus
  journalctl --user-unit alice-codex-connector.service -f
'
```

## 9. 让它“自己重启自己”（无 sudo）

当你让 Codex 修改完项目后，可以在命令末尾执行：

```bash
export XDG_RUNTIME_DIR=/run/user/$(id -u)
export DBUS_SESSION_BUS_ADDRESS=unix:path=$XDG_RUNTIME_DIR/bus
systemctl --user restart --no-block alice-codex-connector.service
```

## 10. 验证“仅新用户 home 可写”

```bash
sudo -u codexbot -H bash -lc 'touch /home/codexbot/alice/.write-ok'
# 预期：成功

sudo -u codexbot -H bash -lc 'touch /etc/forbidden'
# 预期：Permission denied

sudo -u codexbot -H bash -lc 'ls /home/<your-user>'
# 若你的 home 是 700，预期：Permission denied
```

## systemd 沙箱是什么意思

`systemd` 沙箱就是给服务进程加“运行时护栏”，限制它能看到和能写的系统资源。

本方案使用的是用户级 systemd（`systemctl --user`），重点价值是“无需 sudo 自重启”。  
安全边界主要来自：

- 独立用户（`codexbot`）
- 主用户 home 权限收紧（`700`）
- 仅在 `/home/codexbot` 内运行和写入

如果你要更强的内核级沙箱能力（例如更严格的只读挂载），通常还是系统级 service 更稳。

## 可选增强

1. 加网络白名单（仅允许 Git/包管理/飞书 API 等必要域名）。
2. 每次任务使用 `alice/task-YYYYMMDD-HHMM` 独立目录，任务结束归档或删除。
3. 定期快照（`btrfs`/`zfs`/`rsync`）保证可回滚。
