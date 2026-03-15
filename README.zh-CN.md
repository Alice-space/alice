# 飞书 -> LLM 连接器（Codex / Claude / Kimi，Go，长连接）

[English](./README.md)
[![Dev CI](https://github.com/Alice-space/alice/actions/workflows/ci.yml/badge.svg)](https://github.com/Alice-space/alice/actions/workflows/ci.yml)
[![Main Release](https://github.com/Alice-space/alice/actions/workflows/main-release.yml/badge.svg)](https://github.com/Alice-space/alice/actions/workflows/main-release.yml)
[![Release On Tag](https://github.com/Alice-space/alice/actions/workflows/release-on-tag.yml/badge.svg)](https://github.com/Alice-space/alice/actions/workflows/release-on-tag.yml)

一个最小可用连接器，流程如下：

1. 使用 **飞书官方 Go SDK**（`github.com/larksuite/oapi-sdk-go/v3`）的长连接（`ws`）模式。
2. 接收 `im.message.receive_v1` 文本消息事件。
3. 每条消息调用当前 `llm_provider` 对应 CLI（`codex` / `claude` / `kimi`）。
4. 将回复发送回飞书。

该模式**不需要公网 IP**，因为它走的是飞书长连接（WebSocket），不是公网 webhook 回调。

## 为什么用 Go 而不是 Rust

飞书当前官方服务端 SDK 提供 Go/Java/Python/Node，且官方长连接能力在 Go SDK 中可直接使用。Rust 暂无官方 SDK。

## 运行要求

- Go 1.25+（源码构建，需与 `go.mod` 一致）
- 已安装并登录所选后端 CLI（`codex` / `claude` / `kimi`）
- Linux 主机且可用 `systemd --user`（用于一键安装脚本）
- 飞书应用侧需要：
  - 开启机器人能力
  - 订阅 `im.message.receive_v1` 事件
  - 开通所需消息权限
  - 在飞书开放平台开启长连接模式

## 快速开始

```bash
mkdir -p ~/.alice
cp config.example.yaml ~/.alice/config.yaml
# 编辑 ~/.alice/config.yaml

# 安装依赖
go mod tidy

# 运行测试
go test ./...

# 启动连接器
go run ./cmd/connector
```

## 一句话安装 / 更新 / 卸载（推荐）

安装脚本位于仓库：[`scripts/alice-installer.sh`](./scripts/alice-installer.sh)

安装最新版本（重复执行同一命令即更新）：

```bash
curl -fsSL https://raw.githubusercontent.com/Alice-space/alice/main/scripts/alice-installer.sh | bash -s -- install
```

显式更新到最新版本：

```bash
curl -fsSL https://raw.githubusercontent.com/Alice-space/alice/main/scripts/alice-installer.sh | bash -s -- update
```

安装/更新到指定版本：

```bash
curl -fsSL https://raw.githubusercontent.com/Alice-space/alice/main/scripts/alice-installer.sh | bash -s -- install --version vX.Y.Z
```

显式安装 dev 预发布（默认始终安装 stable release）：

```bash
curl -fsSL https://raw.githubusercontent.com/Alice-space/alice/main/scripts/alice-installer.sh | bash -s -- install --channel dev
```

使用 `--channel dev` 时，若未显式传 `--home` 且未设置 `ALICE_HOME`，默认目录为 `~/.alice-dev`。

卸载（删除服务、二进制、`~/.alice`）：

```bash
curl -fsSL https://raw.githubusercontent.com/Alice-space/alice/main/scripts/alice-installer.sh | bash -s -- uninstall
```

卸载但保留运行数据：

```bash
curl -fsSL https://raw.githubusercontent.com/Alice-space/alice/main/scripts/alice-installer.sh | bash -s -- uninstall --keep-data
```

脚本会自动完成：

- 默认下载 stable GitHub Release 并安装到 `${ALICE_HOME:-~/.alice}/bin/alice`；显式 `--channel dev` 时切换到 dev 预发布（默认目录 `~/.alice-dev`）
- 若 release 提供 `SHA256SUMS`，会先校验校验和再解压安装
- 初始化 `${ALICE_HOME:-~/.alice}` 目录和默认 `config.yaml`（若不存在）
- 检测并复制已有 Codex 登录凭证 `auth.json` 到 `${ALICE_HOME}/.codex/`
- 安装并管理 `systemd --user` 服务（默认 `alice.service`，自动拉起与崩溃重启）
- 尝试开启 linger，尽量保证退出登录后服务仍保持活跃

首次安装后请先配置 `${ALICE_HOME:-~/.alice}/config.yaml` 中的 `feishu_app_id` 和 `feishu_app_secret`，然后再次执行安装命令触发启动/重启。

## 编译

编译当前平台可执行文件：

```bash
go build -o bin/alice ./cmd/connector
```

运行：

```bash
./bin/alice
```

同一个二进制还提供给 skill 使用的 runtime CLI：

```bash
./bin/alice runtime memory context
```

## 提交前检查

手动运行全部检查：

```bash
make check
```

`make check` 包含：

- 密钥扫描（`make secret-check`），用于拦截误提交的 key/token
- shell 脚本语法检查
- `gofmt` 格式检查
- `go vet ./...`
- `go test ./...`
- `go test -race ./internal/connector`

安装 git hooks：

- `pre-commit`：提交前自动执行 `make check`
- `commit-msg`：校验 Conventional Commits 提交信息格式

```bash
make precommit-install
```

## 贡献规则

贡献规范见 [CONTRIBUTING.md](./CONTRIBUTING.md)。

## 分支与 CI 策略

- 日常开发统一提交到 `dev`。
- 指向 `main` 的 PR 仅允许 `dev -> main`（workflow 强制校验）。
- `main` 的 push 必须是来自 `dev` 的 merge commit（workflow 校验）。
- `dev` 合并到 `main` 后，自动执行质量门禁、计算下一个 `vX.Y.Z`、打 tag 并发布 GitHub Release。
- 手动 push `v*` tag 仍保留支持（`release-on-tag.yml`）。
- 建议在 GitHub 仓库设置里给 `main` 开启 branch protection，并禁用 direct push，做强约束。

## 架构文档

- [架构设计与重构规划](./docs/architecture.zh-CN.md)

## 仓库自带 Skill

本仓库已内置可复用 skill（目录 [`skills/`](./skills)）：

- `alice-memory`
- `alice-message`
- `alice-scheduler`
- `alice-code-army`
- `file-printing`
- `feishu-task`

连接器启动时会把内嵌的自带 skill 自动释放到 `$CODEX_HOME/skills`（默认 `~/.alice/.codex/skills`）。非托管的自定义同名目录保持不变。

## 配置文件

程序从 YAML 配置文件读取参数（默认路径：`~/.alice/config.yaml`，可通过 `alice_home` 或 `--alice-home` 覆盖）。

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
claude_command: "claude"
claude_timeout_secs: 120
kimi_command: "kimi"
kimi_timeout_secs: 120
runtime_http_addr: "127.0.0.1:7331"
runtime_http_token: ""
alice_home: ""
workspace_dir: ""
env:
  HTTPS_PROXY: "http://127.0.0.1:7890"
  ALL_PROXY: "socks5://127.0.0.1:7891"
memory_dir: ""
prompt_dir: ""

codex_prompt_prefix: ""
claude_prompt_prefix: ""
kimi_prompt_prefix: ""
failure_message: "Codex 暂时不可用，请稍后重试。"
thinking_message: "正在思考中..."
immediate_feedback_mode: "reply"
immediate_feedback_reaction: "SMILE"

queue_capacity: 256
worker_concurrency: 1
automation_task_timeout_secs: 600
idle_summary_hours: 8
group_context_window_minutes: 5

log_level: "info"
log_file: ""
log_max_size_mb: 20
log_max_backups: 5
log_max_age_days: 7
log_compress: false
```

必填项：

- `feishu_app_id`
- `feishu_app_secret`

可选项：

- `llm_provider`：LLM 后端类型选择。支持 `codex`（默认）、`claude`、`kimi`。
- `codex_command` / `codex_timeout_secs`、`claude_command` / `claude_timeout_secs`、`kimi_command` / `kimi_timeout_secs`：对应后端 CLI 命令路径与超时秒数。
- `runtime_http_addr` / `runtime_http_token`：Alice 本地 runtime HTTP API 的监听地址和鉴权 token。若 `runtime_http_token` 为空，Alice 会在每次启动时自动生成一个 token 并注入 agent 环境变量。
- `alice_home`：运行时根目录（release 默认 `~/.alice`；dev 预发布二进制默认 `~/.alice-dev`）。
- `workspace_dir` / `memory_dir` / `prompt_dir`：运行时目录。默认在 `alice_home` 下，分别是 `workspace/`、`memory/`、`prompts/`。
- `CODEX_HOME`：Alice 启动时会强制设置为 `alice_home/.codex`；子进程默认继承该值（若在 `env` 里显式设置则以显式值为准）。
- `env`：注入到所选 LLM 子进程的环境变量键值（例如 HTTP/HTTPS/SOCKS 代理配置）。
- `codex_prompt_prefix` / `claude_prompt_prefix` / `kimi_prompt_prefix`：仅在新线程中追加的全局指令前缀，默认为空。
- `immediate_feedback_mode`：收到引用回复消息后给用户的即时反馈方式。支持 `reply`（默认，直接回复 `收到！`）和 `reaction`（优先给原消息加表情，失败再回退 `收到！`）。
- `immediate_feedback_reaction`：`immediate_feedback_mode=reaction` 时使用的飞书 reaction 类型，默认 `SMILE`。
- 自动化 cron 调度使用运行机器的操作系统时区（`time.Local`）。
- `automation_task_timeout_secs`：单次自动化用户任务（`send_text`/`run_llm`）的执行超时秒数，默认 `600`。
- `idle_summary_hours`：触发后台分日期摘要落盘的空闲阈值（小时，默认 `8`）。
- `group_context_window_minutes`：群聊未触发消息的缓存窗口（分钟，默认 `5`）。窗口内文本与多媒体会在后续触发时并入上下文（`at`/`prefix` 模式）。
- `log_file` / `log_max_size_mb` / `log_max_backups` / `log_max_age_days` / `log_compress`：滚动日志配置；`log_file` 为空时默认写入 `alice_home/log/YYYY-MM-DD.log`，底层使用 `zerolog + lumberjack`。
- `trigger_mode`：群聊触发策略，支持 `at`（默认）、`active`、`prefix`。
- `trigger_prefix`：群聊触发前缀。`active` 下命中此前缀会忽略，但艾特机器人仍会触发；`prefix` 下“命中前缀”或“艾特机器人”任一满足即触发。`prefix` 模式下命中前缀时会在发送给 Codex 前剥离前缀。
- `feishu_bot_open_id` / `feishu_bot_user_id`：群聊/话题群中用于匹配机器人艾特的 ID。

## 隔离运行（独立用户）

如果你希望把本项目放到独立账号下自动运行，降低误改主账号文件风险，参考：

- [在独立用户下隔离运行本项目（Codex 自动运行）](./docs/run-with-isolated-user.zh-CN.md)

## 运行行为

- 二进制可直接前台运行；生产部署建议使用安装脚本创建 `systemd --user` 服务做自动拉起与保活。
- 支持接收消息类型：`text`、`image`、`sticker`、`audio`、`file`。
- 群聊/话题群触发由 `trigger_mode` 控制：
  - `at`：仅处理艾特机器人的消息。若 `feishu_bot_open_id` 与 `feishu_bot_user_id` 都为空，则群聊/话题群消息全部忽略。
  - `active`：默认处理所有消息，但命中 `trigger_prefix` 的消息会忽略；若同时艾特机器人，仍会处理。
  - `prefix`：命中 `trigger_prefix` 或艾特机器人的消息会处理。
- 在 `trigger_mode=at` 和 `trigger_mode=prefix` 下，群聊/话题群中未触发消息会按“同群同人”进入滑动窗口缓存（时长由 `group_context_window_minutes` 配置），缓存内容包含文本与多媒体。
- 同一用户后续在该群再次触发时，会把窗口内缓存的文本与多媒体并入本次上下文；thread 消息会按 `thread_id`/`root_id` 隔离，不跨 thread 混入。
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
- 自动化任务会通过 `bbolt` 持久化到 `memory_dir/automation.db`。
- 每次调用 Codex 前，只会注入长期记忆；分日期记忆只提供目录位置，让 Codex 按需检索。
- 会话复用改为“按话题优先”：
  - 同一飞书话题线程（`thread_id`，没有则回退 `root_id`）内的消息复用同一个 Codex 线程。
  - 不属于任何话题线程的消息，每条消息都会新建一个 Codex session。
- 若某聊天连续空闲达到 `idle_summary_hours`（默认 8 小时），后台会异步 resume 该线程并将“空闲摘要”追加到 `daily/YYYY-MM-DD.md`，同一段空闲期仅写一次。
- 消息主处理路径不会等待空闲摘要落盘，新消息会被立即处理。
- 在“引用回复”链路里，机器人会优先使用“话题回复”（`reply_in_thread=true`）发送收到/进度/结果；若飞书拒绝话题模式，则自动回退普通引用回复。
- 仓库自带 skill 通过本地 runtime HTTP API 发送文本/图片/文件，发送目标始终由当前会话上下文自动决定：私聊发送到当前私聊；群聊/话题群存在 `source_message_id` 时按该消息引用回复（优先 thread）。
- 收到用户消息后，机器人会按 `immediate_feedback_mode` 立即反馈：默认引用回复 `收到！`，也可改成优先给原消息添加 reaction。
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
- `cmd/connector/runtime_*.go`：挂在同一个 `alice` 二进制上的 skill 运行时子命令
- `internal/config/config.go`：配置文件读取与校验（`viper`）
- `internal/bootstrap/`：两个二进制共享的启动/装配辅助模块，包含分阶段 connector runtime builder
- `internal/automation/`：Alice 自动化任务的调度、存储与执行
- `internal/llm/`：LLM 后端抽象与工厂
- `internal/memory/memory.go`：记忆模块（长期记忆 + 按日期短期记忆文件）
- `internal/llm/codex/codex.go`：Codex CLI 调用与 JSONL 解析
- `internal/connector/app.go`：长连接应用循环、WebSocket 生命周期与 worker 编排
- `internal/connector/app_queue.go`：会话路由、任务入队与 active-run 抢占
- `internal/connector/processor.go`：Prompt 组装、Codex 调用与任务级编排
- `internal/connector/reply_dispatcher.go`：集中管理卡片/富文本/纯文本的回复回退策略
