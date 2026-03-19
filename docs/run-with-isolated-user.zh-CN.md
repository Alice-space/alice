# 在独立用户 Home 下隔离运行 Alice（推荐）

本文给出可直接落地的隔离方案：`独立用户 + 独立 home + 用户级 systemd 托管`。
目标是让 Alice 与 LLM CLI（Codex/Claude/Kimi）只在独立用户目录中运行和写入。

## 目标与边界

- 目标：Alice 进程和 LLM 子进程仅在 `codexbot` 用户 home 下读写。
- 边界：这是“用户隔离 + 进程托管”，不是容器/虚拟机级硬隔离。

## 前置要求（AlmaLinux / RHEL）

```bash
sudo dnf install -y curl git
```

如果你需要源码构建，再安装 Go：

```bash
sudo dnf install -y go
go version   # 需 >= 1.25
```

## 1. 新建专用用户（无 sudo）

```bash
id -u codexbot >/dev/null 2>&1 || sudo useradd -m -s /bin/bash codexbot
sudo gpasswd -d codexbot wheel 2>/dev/null || true
sudo passwd -l codexbot
```

说明：
- `passwd -l` 仅锁密码登录，不影响 `sudo -u codexbot ...` 与用户级 systemd。

## 2. 初始化隔离目录

```bash
sudo -u codexbot -H mkdir -p /home/codexbot/.alice/.codex
sudo chmod 700 /home/codexbot /home/codexbot/.alice /home/codexbot/.alice/.codex
```

可选：主账号 home 设为 `700`，避免被其他普通用户读取。

## 3. 以隔离用户登录 LLM CLI（一次性）

以 Codex 为例：

```bash
sudo -u codexbot -H env HOME=/home/codexbot CODEX_HOME=/home/codexbot/.alice/.codex codex login
sudo -u codexbot -H env HOME=/home/codexbot CODEX_HOME=/home/codexbot/.alice/.codex codex login status
```

若使用 Claude/Kimi，请在 `config.yaml` 配置对应命令并完成各自登录。

## 4. 一句话安装 Alice（release 二进制）

```bash
sudo -u codexbot -H bash -lc '
  curl -fsSL https://cdn.jsdelivr.net/gh/Alice-space/alice@main/scripts/alice-installer.sh | bash -s -- install
'
```

安装脚本会自动：
- 下载最新 release 到 `~/.alice/bin/alice`
- 初始化 `~/.alice` 目录与默认 `config.yaml`
- 安装用户级服务 `~/.config/systemd/user/alice.service`
- 尝试启用 `linger`，提高登出后常驻概率

> 若需指定版本：`... install --version vX.Y.Z`

## 5. 配置 Alice

编辑：`/home/codexbot/.alice/config.yaml`

最少配置：

```yaml
feishu_app_id: "cli_xxxxx"
feishu_app_secret: "xxxxxx"
llm_provider: "codex"
codex_command: "codex"
```

可选建议：

```yaml
workspace_dir: "/home/codexbot/.alice/workspace"
memory_dir: "/home/codexbot/.alice/memory"
```

## 6. 重新执行安装命令触发启动/更新

```bash
sudo -u codexbot -H bash -lc '
  curl -fsSL https://cdn.jsdelivr.net/gh/Alice-space/alice@main/scripts/alice-installer.sh | bash -s -- install
'
```

重复执行 `install` 命令就是“更新 + 重启”（幂等）。

## 7. 查看服务状态与日志

```bash
sudo -u codexbot -H bash -lc '
  export XDG_RUNTIME_DIR=/run/user/$(id -u)
  export DBUS_SESSION_BUS_ADDRESS=unix:path=$XDG_RUNTIME_DIR/bus
  systemctl --user status --no-pager alice.service
'
```

```bash
sudo -u codexbot -H bash -lc '
  export XDG_RUNTIME_DIR=/run/user/$(id -u)
  export DBUS_SESSION_BUS_ADDRESS=unix:path=$XDG_RUNTIME_DIR/bus
  journalctl --user-unit alice.service -f
'
```

## 8. 卸载（可选）

完整卸载（删服务、二进制、`~/.alice`）：

```bash
sudo -u codexbot -H bash -lc '
  curl -fsSL https://cdn.jsdelivr.net/gh/Alice-space/alice@main/scripts/alice-installer.sh | bash -s -- uninstall
'
```

仅卸载服务与二进制，保留数据：

```bash
sudo -u codexbot -H bash -lc '
  curl -fsSL https://cdn.jsdelivr.net/gh/Alice-space/alice@main/scripts/alice-installer.sh | bash -s -- uninstall --keep-data
'
```

## 9. 验证“仅隔离用户可写”

```bash
sudo -u codexbot -H bash -lc 'touch /home/codexbot/.alice/.write-ok'
# 预期：成功

sudo -u codexbot -H bash -lc 'touch /etc/forbidden'
# 预期：Permission denied
```

## 什么时候需要源码构建

仅在你要开发/调试源码时：

```bash
sudo -u codexbot -H bash -lc '
  cd /home/codexbot
  git clone https://github.com/Alice-space/alice.git
  cd alice
  go mod tidy
  go test ./...
  go build -o /home/codexbot/.alice/bin/alice ./cmd/connector
'
```

开发场景下仍建议继续使用 `alice.service` 做托管。
