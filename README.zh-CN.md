# 飞书 -> Codex 连接器（Go，长连接）

[English](./README.md)

一个最小可用连接器，流程如下：

1. 使用 **飞书官方 Go SDK**（`github.com/larksuite/oapi-sdk-go/v3`）的长连接（`ws`）模式。
2. 接收 `im.message.receive_v1` 文本消息事件。
3. 每条文本消息调用一次 `codex exec`。
4. 将回复发送回飞书。

该模式**不需要公网 IP**，因为它走的是飞书长连接（WebSocket），不是公网 webhook 回调。

## 为什么用 Go 而不是 Rust

飞书当前官方服务端 SDK 提供 Go/Java/Python/Node，且官方长连接能力在 Go SDK 中可直接使用。Rust 暂无官方 SDK。

## 运行要求

- Go 1.23+（已在 Go 1.26 验证）
- 已安装并登录 `codex` CLI（`codex login status`）
- 飞书应用侧需要：
  - 开启机器人能力
  - 订阅 `im.message.receive_v1` 事件
  - 开通所需消息权限
  - 在飞书开放平台开启长连接模式

## 快速开始

```bash
cp config.example.yaml config.yaml
# 编辑 config.yaml

# 安装依赖
go mod tidy

# 运行测试
go test ./...

# 启动连接器
go run ./cmd/connector -c config.yaml
```

## 编译

编译当前平台可执行文件：

```bash
go build -o bin/alice-connector ./cmd/connector
```

运行：

```bash
./bin/alice-connector -c config.yaml
```

## 提交前检查

手动运行全部检查：

```bash
make check
```

安装 git hooks：

- `pre-commit`：提交前自动执行 `make check`
- `commit-msg`：校验 Conventional Commits 提交信息格式

```bash
make precommit-install
```

## 贡献规则

贡献规范见 [CONTRIBUTING.md](./CONTRIBUTING.md)。

## 配置文件

程序从 YAML 配置文件读取参数（默认路径：`config.yaml`）。

你也可以传入自定义路径：

```bash
go run ./cmd/connector -c /path/to/config.yaml
```

`config.example.yaml` 示例：

```yaml
feishu_app_id: "cli_xxxxx"
feishu_app_secret: "xxxxxx"
feishu_base_url: "https://open.feishu.cn"

codex_command: "codex"
codex_timeout_secs: 120
workspace_dir: "."
env:
  HTTPS_PROXY: "http://127.0.0.1:7890"
  ALL_PROXY: "socks5://127.0.0.1:7891"
memory_dir: ".memory"

codex_prompt_prefix: "你是一个助手，请用中文简洁回答，不要使用 Markdown 标题。"
failure_message: "Codex 暂时不可用，请稍后重试。"
thinking_message: "正在思考中..."

queue_capacity: 256
worker_concurrency: 1
idle_summary_hours: 8

log_level: "info"
```

必填项：

- `feishu_app_id`
- `feishu_app_secret`

可选项：

- `env`：注入到 `codex` 子进程的环境变量键值（例如 HTTP/HTTPS/SOCKS 代理配置）。
- `idle_summary_hours`：触发后台分日期摘要落盘的空闲阈值（小时，默认 `8`）。

## 隔离运行（独立用户）

如果你希望把本项目放到独立账号下自动运行，降低误改主账号文件风险，参考：

- [在独立用户下隔离运行本项目（Codex 自动运行）](./docs/run-with-isolated-user.zh-CN.md)

## 运行行为

- 非文本消息会忽略。
- 群聊中的 `<at ...>...</at>` 会先清理，再发送给 Codex。
- 默认启用记忆模块，文件写入 `memory_dir`：长期记忆 `MEMORY.md`，分日期记忆在 `daily/YYYY-MM-DD.md`。
- 首次启动时会自动创建 `memory_dir` 及其 `daily/` 子目录。
- 连接器会把每个聊天的会话状态持久化到 `memory_dir/session_state.json`，重启后仍可续接线程。
- 每次调用 Codex 前，仅把长期记忆注入提示词；分日期记忆只提供目录位置，让 Codex 按需检索。
- 每个聊天（`chat_id`，没有则 `open_id`）始终复用同一个 Codex 线程。
- 若某聊天连续空闲达到 `idle_summary_hours`（默认 8 小时），后台会异步 resume 该线程并将“空闲摘要”追加到 `daily/YYYY-MM-DD.md`，同一段空闲期仅写一次。
- 消息主处理路径不会等待空闲摘要落盘，新消息会被立即处理。
- 机器人会使用“**卡片消息 + 引用回复原消息**”方式返回结果。
- Codex 执行期间，会把思考过程持续同步到同一条卡片消息。
- 同一会话内若收到新的用户消息，会立即中断旧任务并切换到最新消息（steer）。
- Codex 完成后，会把同一条卡片更新为最终答案。
- 回复目标优先级（非卡片回退路径）：`chat_id`，没有则回退到发送者 `open_id`。
- Codex 超时或失败时，发送 `failure_message`。

说明：飞书 IM 官方目录里有“回复消息/卡片更新”接口，但没有独立的机器人“正在输入”状态接口。本项目使用“卡片增量更新”来提供接近打字中的体验。

## 飞书 API 参考

- 回复消息: https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/reference/im-v1/message/reply
- 更新消息卡片: https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/reference/im-v1/message/patch
- API 目录: https://open.feishu.cn/api_explorer/v1/api_catalog

## 项目结构

- `cmd/connector/main.go`：启动与生命周期
- `internal/config/config.go`：配置文件读取与校验（`viper`）
- `internal/memory/memory.go`：记忆模块（长期记忆 + 按日期短期记忆文件）
- `internal/codex/codex.go`：Codex CLI 调用与 JSONL 解析
- `internal/connector/connector.go`：长连接、队列、worker、飞书发消息
