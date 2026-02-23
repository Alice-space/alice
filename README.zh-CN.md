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
feishu_bot_open_id: ""
feishu_bot_user_id: ""

llm_provider: "codex"
codex_command: "codex"
codex_timeout_secs: 120
workspace_dir: "."
env:
  HTTPS_PROXY: "http://127.0.0.1:7890"
  ALL_PROXY: "socks5://127.0.0.1:7891"
memory_dir: ".memory"

codex_prompt_prefix: ""
failure_message: "Codex 暂时不可用，请稍后重试。"
thinking_message: "正在思考中..."

queue_capacity: 256
worker_concurrency: 1
automation_task_timeout_secs: 600
idle_summary_hours: 8
group_context_window_minutes: 5

log_level: "info"
```

必填项：

- `feishu_app_id`
- `feishu_app_secret`

可选项：

- `llm_provider`：LLM 后端类型选择。当前支持 `codex`（默认）。
- `env`：注入到 `codex` 子进程的环境变量键值（例如 HTTP/HTTPS/SOCKS 代理配置）。
- `codex_prompt_prefix`：仅在新线程中追加的全局指令前缀，默认为空。
- `automation_task_timeout_secs`：单次自动化用户任务（`send_text`/`run_llm`）的执行超时秒数，默认 `600`。
- `idle_summary_hours`：触发后台分日期摘要落盘的空闲阈值（小时，默认 `8`）。
- `group_context_window_minutes`：群聊中未艾特机器人的消息缓存窗口（分钟，默认 `5`）。窗口内文本与多媒体会在后续艾特触发时并入上下文。
- `feishu_bot_open_id` / `feishu_bot_user_id`：用于群聊严格艾特过滤的机器人 ID。群聊中只有艾特命中这些 ID 的消息才会触发处理。

## 隔离运行（独立用户）

如果你希望把本项目放到独立账号下自动运行，降低误改主账号文件风险，参考：

- [在独立用户下隔离运行本项目（Codex 自动运行）](./docs/run-with-isolated-user.zh-CN.md)

## 运行行为

- 支持接收消息类型：`text`、`image`、`sticker`、`audio`、`file`。
- 群聊/话题群中，仅处理艾特机器人的消息。
  - 若 `feishu_bot_open_id` 与 `feishu_bot_user_id` 都为空，则群聊/话题群消息全部忽略。
- 群聊/话题群中未艾特机器人的消息会按“同群同人”进入滑动窗口缓存（时长由 `group_context_window_minutes` 配置），缓存内容包含文本与多媒体。
- 同一用户后续在该群艾特机器人触发时，会把窗口内缓存的文本与多媒体并入本次上下文；thread 消息会按 `thread_id`/`root_id` 隔离，不跨 thread 混入。
- 群聊中的 `<at ...>...</at>` 会先清理，再发送给 Codex。
- 说话人上下文仍会注入参与者的 id 映射和 `@提及` 文本，并附带“可直接使用 `@姓名`/`@id`”提示，但会过滤机器人自身身份（`feishu_bot_open_id`/`feishu_bot_user_id`）对应的注入内容。
- 发送回复时会基于当前消息上下文中的身份信息，把 `@姓名`/`@id` 自动规范化为飞书 mention 标签（`<at user_id="...">...</at>`）。
- 用户昵称补全会先调用 Contact `GetUser`；若在群聊/话题群中返回空名，会按 `chat_id` 回退调用 `GetChatMembers`。
- 若要启用群成员昵称回退，请开通以下任一权限：`im:chat.members:read`、`im:chat.group_info:readonly`、`im:chat:readonly`、`im:chat`。
- 默认启用记忆模块，文件写入 `memory_dir`：长期记忆 `MEMORY.md`，分日期记忆在 `daily/YYYY-MM-DD.md`。
- 下载的消息资源会落盘到 `memory_dir/resources/YYYY-MM-DD/<source_message_id>/`。
- 首次启动时会自动创建 `memory_dir` 及其 `daily/` 子目录。
- 连接器会把每个聊天的会话状态持久化到 `memory_dir/session_state.json`，重启后仍可续接线程。
- 连接器会把队列中/执行中的任务持久化到 `memory_dir/runtime_state.json`，重启后会继续回复未完成或未回复的消息。
- 若任务文本明显是“自更新并重启自己”，且处理过程中因重启导致中断，会将该任务视为已完成，避免重启后循环再次处理同一更新指令。
- 每次调用 Codex 前，只会注入长期记忆；分日期记忆只提供目录位置，让 Codex 按需检索。
- 会话复用改为“按话题优先”：
  - 同一飞书话题线程（`thread_id`，没有则回退 `root_id`）内的消息复用同一个 Codex 线程。
  - 不属于任何话题线程的消息，每条消息都会新建一个 Codex session。
- 若某聊天连续空闲达到 `idle_summary_hours`（默认 8 小时），后台会异步 resume 该线程并将“空闲摘要”追加到 `daily/YYYY-MM-DD.md`，同一段空闲期仅写一次。
- 消息主处理路径不会等待空闲摘要落盘，新消息会被立即处理。
- 在“引用回复”链路里，机器人会优先使用“话题回复”（`reply_in_thread=true`）发送收到/进度/结果；若飞书拒绝话题模式，则自动回退普通引用回复。
- 对于 MCP `alice-feishu` 工具（`send_image`/`send_file`），发送目标始终由当前会话上下文自动决定，且不能由工具参数覆盖：私聊发送到当前私聊；群聊/话题群存在 `source_message_id` 时按该消息引用回复（优先 thread）。
- 收到用户消息后，机器人会第一时间引用回复 `收到！`。
- Codex 执行期间，流式 `agent_message` 会优先以卡片回复；若卡片失败，会依次回退到富文本（`post`）和纯文本回复。
- 若回复内容中包含可解析的 @提及，连接器会直接发送纯文本消息（不走卡片/富文本），以确保飞书侧正确触发 mention。
- Codex 执行期间，流式 `file_change` 事件也走同样的“卡片优先”回复链路，例如：`internal/x.go已更改，+23-34`。
- 若当前 Codex CLI 未输出原生 `file_change` 事件，连接器会回退到仓库 diff 快照（git numstat）生成同格式的 `file_change` 通知。
- 同一会话内若收到新的用户消息，会立即中断旧任务并切换到最新消息（steer）。
- 若执行过程中没有任何流式 `agent_message`，完成后会走同样的“卡片优先”回退链路发送最终答案。
- 回复目标优先级（回退路径）：`chat_id`，没有则回退到发送者 `open_id`。
- Codex 超时或失败时，发送 `failure_message`。

说明：当前会话输出采用“卡片优先 + 富文本/文本回退”回复链路，不再依赖卡片增量更新链路。

## 飞书 API 参考

- 回复消息: https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/reference/im-v1/message/reply
- API 目录: https://open.feishu.cn/api_explorer/v1/api_catalog

## 项目结构

- `cmd/connector/main.go`：启动与生命周期
- `internal/config/config.go`：配置文件读取与校验（`viper`）
- `internal/llm/`：LLM 后端抽象与工厂
- `internal/memory/memory.go`：记忆模块（长期记忆 + 按日期短期记忆文件）
- `internal/codex/codex.go`：Codex CLI 调用与 JSONL 解析
- `internal/connector/connector.go`：长连接、队列、worker、飞书发消息
