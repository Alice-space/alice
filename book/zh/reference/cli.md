# CLI 命令

Alice 提供多个 CLI 子命令用于不同操作。

---

## 主进程

### `alice --feishu-websocket`

启动完整的飞书连接器运行时。连接飞书 WebSocket 并处理实时消息。

```bash
alice --feishu-websocket
```

### `alice --runtime-only`

以 runtime-only 模式启动。本地 HTTP API 和自动化引擎运行，但飞书 WebSocket 不启动。

```bash
alice --runtime-only
```

### `alice-headless --runtime-only`

无头 runtime-only 二进制。明确不能启动飞书连接器。

```bash
alice-headless --runtime-only
```

> `alice-headless` 如果以 `--feishu-websocket` 调用会报错。

---

## 全局参数

| 参数 | 说明 |
|------|-------------|
| `--alice-home <path>` | 覆盖默认的运行时 home 目录 |
| `--config <path>` | config.yaml 路径（默认：`<alice_home>/config.yaml`） |
| `--log-level <level>` | 覆盖日志级别（`debug`、`info`、`warn`、`error`） |
| `--version` | 输出版本并退出 |

环境变量 `ALICE_HOME` 也会覆盖默认 home 目录。

---

## `alice setup`

初始化 Alice 运行时环境。

```bash
alice setup
```

执行内容：
1. 在 `~/.alice/` 下创建目录结构
2. 写入初始 `config.yaml`（基于 `config.example.yaml`）
3. 将内置 bundled skill 同步到 `${ALICE_HOME}/skills/`
4. Linux 上：在 `~/.config/systemd/user/alice.service` 注册 systemd 用户单元
5. 在 `~/.config/opencode/plugins/alice-delegate.js` 安装 OpenCode delegate 插件

安装后运行一次即可。

---

## `alice delegate`

向配置好的 LLM 后端发送一次性 prompt。

```bash
alice delegate --provider <name> --prompt "<text>"
```

### 选项

| 参数 | 说明 |
|------|-------------|
| `--provider <name>` | 后端：`opencode`、`codex`、`claude`、`gemini`、`kimi` |
| `--prompt <text>` | Prompt 文本（必填） |
| `--model <name>` | 覆盖默认模型 |
| `--workspace <path>` | 覆盖工作目录 |

### 示例

```bash
alice delegate --provider codex --prompt "Fix the null check in auth.go"
alice delegate --provider claude --prompt "Review this diff" < changes.patch
```

---

## `alice runtime message`

通过 runtime API 发送消息。

```bash
alice runtime message image <path> [--caption <text>]
alice runtime message file <path> [--filename <name>] [--caption <text>]
```

| 子命令 | 说明 |
|------------|-------------|
| `image <path>` | 上传并发送图片 |
| `file <path>` | 上传并发送文件 |

| 参数 | 说明 |
|------|-------------|
| `--caption <text>` | 可选的说明文字 |
| `--filename <name>` | 覆盖文件显示名称（仅 file） |

---

## `alice runtime automation`

通过 runtime API 管理自动化任务。

```bash
alice runtime automation list [--status <status>] [--limit <n>]
alice runtime automation create <payload>
alice runtime automation get <task-id>
alice runtime automation update <task-id> <payload>
alice runtime automation delete <task-id>
```

| 子命令 | 说明 |
|------------|-------------|
| `list` | 列出自动化任务 |
| `create <json>` | 从 JSON payload 创建任务 |
| `get <id>` | 获取单个任务 |
| `update <id> <json>` | 通过 JSON merge-patch 更新任务 |
| `delete <id>` | 删除任务 |

| 参数（list） | 说明 |
|-------------|-------------|
| `--status` | 按状态筛选：`active`、`completed`、`cancelled` |
| `--limit` | 每页条数 |

---

## `alice runtime goal`

管理对话作用域的活跃 goal。

```bash
alice runtime goal get
alice runtime goal create <description>
alice runtime goal pause
alice runtime goal resume
alice runtime goal complete
alice runtime goal delete
```

| 子命令 | 说明 |
|------------|-------------|
| `get` | 获取当前活跃 goal |
| `create <desc>` | 创建新 goal |
| `pause` | 暂停活跃 goal |
| `resume` | 恢复已暂停的 goal |
| `complete` | 将活跃 goal 标记为完成 |
| `delete` | 删除活跃 goal |

---

## `alice skills`

管理 bundled skill。

```bash
alice skills sync
alice skills list
```

| 子命令 | 说明 |
|------------|-------------|
| `sync` | 将内嵌 bundled skill 同步到本地 skill 目录 |
| `list` | 列出已安装的 bundled skill |

`alice skills sync` 在启动时也会自动运行。

---

## 退出码

| 码 | 含义 |
|------|---------|
| `0` | 成功 |
| `1` | 一般错误 |
| `2` | 配置错误 |
| `3` | 认证错误 |
