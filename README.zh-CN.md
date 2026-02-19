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

安装 git pre-commit hook（提交前自动执行 `make check`）：

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

codex_prompt_prefix: "你是一个助手，请用中文简洁回答，不要使用 Markdown 标题。"
failure_message: "Codex 暂时不可用，请稍后重试。"

queue_capacity: 256
worker_concurrency: 1

log_level: "info"
```

必填项：

- `feishu_app_id`
- `feishu_app_secret`

## 运行行为

- 非文本消息会忽略。
- 群聊中的 `<at ...>...</at>` 会先清理，再发送给 Codex。
- 回复目标优先 `chat_id`，没有则回退到发送者 `open_id`。
- Codex 超时或失败时，发送 `failure_message`。

## 项目结构

- `cmd/connector/main.go`：启动与生命周期
- `internal/config/config.go`：配置文件读取与校验（`viper`）
- `internal/codex/codex.go`：Codex CLI 调用与 JSONL 解析
- `internal/connector/connector.go`：长连接、队列、worker、飞书发消息
