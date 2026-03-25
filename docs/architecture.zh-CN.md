# Alice 运行时架构

[English](./architecture.md)

本文档描述 2026 年 3 月 13 日这轮 runtime/skills 重构后的目标架构。

## 设计目标

- 让 connector 只负责编排，不再承载大段 prompt 字面量和具体工具业务。
- 把 LLM 后端明确做成可替换适配层。
- 把会话级运维能力迁移成可复用的外置 skill，通过本地 HTTP API 与 Alice 通信。
- 让 runtime skill 和本地 HTTP API 成为唯一受支持的扩展入口。
- 让调试链路可审计：每次 agent 调用都能看到 Markdown 格式的输入、输出、tool 调用。

## 组件地图

- `cmd/connector`
  启动 Feishu connector、automation engine、本地 runtime HTTP API，以及同一二进制上的 `runtime` skill 子命令。
- `internal/connector`
  负责 Feishu websocket 接入、排队、按 session 串行、抢占中断、回复派发，以及 agent 运行时环境变量注入。
- `internal/llm`
  后端工厂与 provider 适配层，当前支持 `codex`、`claude`、`gemini`、`kimi`。
- `internal/prompting`
  基于磁盘模板文件的渲染器，使用 Go template + `sprig`。
- `internal/memory`
  管理 scoped memory、memory prompt 组装，以及 HTTP 暴露用的 snapshot/update 能力。
- `internal/automation`
  定时任务的持久化与执行，支持 `send_text`、`run_llm`。
- `internal/runtimeapi`
  本地鉴权 HTTP server/client，供 skills 复用。
- `internal/statusview`
  聚合当前 scope 的 usage、automation、campaign 视图，供 `/status` 一类展示使用。
- `internal/imagegen`
  图片生成/编辑适配层，以及输出大小保护逻辑。
- `internal/messaging`
  runtime 与 connector 共享的 sender/uploader 窄接口定义。
- `internal/storeutil`
  `bbolt` 的打开、版本、父目录创建等通用 helper。
- `skills/`
  外置运行时技能，如 `alice-memory`、`alice-message`、`alice-scheduler`、`alice-code-army`。

## Prompt 体系

prompt 不再以内联大字符串散落在代码里。

- Prompt 根目录：`prompts/`
- LLM 首轮 prompt 模板：`prompts/llm/initial_prompt.md.tmpl`
- Memory prompt 模板：`prompts/memory/prompt.md.tmpl`
- Connector 上下文模板：
  - `prompts/connector/current_user_input.md.tmpl`
  - `prompts/connector/reply_context.md.tmpl`
  - `prompts/connector/runtime_skill_hint.md.tmpl`
  - `prompts/connector/idle_summary.md.tmpl`
  - `prompts/connector/synthetic_mention.md.tmpl`
- Campaign repo 工作流模板：
  - `prompts/campaignrepo/executor_dispatch.md.tmpl`
  - `prompts/campaignrepo/reviewer_dispatch.md.tmpl`

`internal/prompting` 从磁盘读取模板，使用 `xxhash` 做编译缓存，并暴露 `sprig` 函数。

当前约束是：

- `App`、`Processor`、各个 LLM runner 都优先接收显式注入的 prompt loader。
- 如果调用链上没有显式注入，会回退到 `internal/prompting.DefaultLoader()`，自动向上查找仓库 `prompts/` 目录。
- 非测试代码里的业务 prompt 已经统一收进模板，不再保留字符串版 fallback。

## 后端抽象

当前后端支持：

- `codex`
- `claude`
- `gemini`
- `kimi`

关键约束：

- 统一保持 `llm.Backend` 高层接口不变。
- `kimi` 通过本机 `kimi` CLI 的 `print/stream-json` 模式运行，并把 Alice 的 thread/session 直接映射到 Kimi 的 session。

## Runtime HTTP API

connector 进程现在会同时暴露本地鉴权 HTTP API，供 skills 和轻量代理使用。

当前 API 分组：

- `/api/v1/messages/*`
  向当前会话发送 text/image/file。
- `/api/v1/memory/*`
  查看 memory 上下文、覆盖长期记忆、追加 daily summary。
- `/api/v1/automation/*`
  创建、列出、查询、补丁更新、删除定时任务。
- `/api/v1/campaigns/*`
  管理当前会话范围内的 code-army campaign、trial、guidance、review 和 pitfall。

配置项：

- `runtime_http_addr`
- `runtime_http_token`

如果没有显式配置 `runtime_http_token`，connector 会在启动时生成一次性 token，并注入到 agent 运行环境。

当前 runtime API 的防护约束：

- 请求体大小限制在小型 JSON 范围内，因为接口传的是路径和元数据，而不是大文件二进制。
- Bearer token 本地接口增加了进程内速率限制，用来降低 token 被暴力猜测的风险。
- 图片/文件通过本地路径上传，不走 runtime API 直接传原始大文件。

## Session Key 路由格式

Alice 通过 canonical session key 及其别名来做会话路由。

常见格式：

- `{receive_id_type}:{receive_id}`
- `{receive_id_type}:{receive_id}|scene:{scene}`
- `{receive_id_type}:{receive_id}|scene:{scene}|thread:{thread_id}`
- `{receive_id_type}:{receive_id}|scene:{scene}|message:{message_id}`

特殊形式：

- work scene 种子 key：`{receive_id_type}:{receive_id}|scene:work|seed:{source_message_id}`
- chat reset alias：会话轮转时追加 `|reset:{message_id}`

这些 alias 让 Alice 在后续 Feishu 事件以根消息、普通回复或 thread reply 形式返回时，仍然能映射回同一个逻辑 session。

## Codex 执行策略默认值

当前内置默认配置里，Codex 的 work scene 有意使用高权限执行策略：

- sandbox：`danger-full-access`
- approval mode：`never`

这是为了让 work scene agent 能直接修改 workspace 并运行本地工具，避免每轮都卡在交互审批。前提是假设当前本地工作区和内置 skill 是可信的；如果运行环境更敏感，应在配置里显式收紧 Codex 场景策略。

## Skills 形态

memory 和定时任务能力现在以外置 skill 的形式暴露，而不是只能从 MCP 进入：

- `skills/alice-memory`
  通过 `alice runtime memory ...` 查看和更新当前会话 memory。
- `skills/alice-message`
  通过 `alice runtime message ...` 发送图片、文件等附件；纯文本由主回复链路直接转发。
- `skills/alice-scheduler`
  通过 `alice runtime automation ...` 管理 automation task。
- `skills/alice-code-army`
  作为长期优化任务的编排 skill，当前通过 `alice runtime campaigns ...` 管理轻量 campaign 索引，并以 campaign 仓库中的 markdown/frontmatter 结构做主事实源；Alice backend 会周期性 reconcile campaign repo，刷新 live-report 和 wake task；planner / executor / reviewer workflow 也直接围绕这套 repo-first 状态运行。

这些 skill 依赖的运行时环境变量：

- `ALICE_RUNTIME_API_BASE_URL`
- `ALICE_RUNTIME_API_TOKEN`
- `ALICE_RUNTIME_BIN`
- 既有会话环境变量，如 `ALICE_MCP_RECEIVE_ID`、`ALICE_MCP_SESSION_KEY`、actor 元数据等

仓库自带 skill 脚本解析运行时二进制的顺序是：

1. `ALICE_RUNTIME_BIN`
2. `${ALICE_HOME:-$HOME/.alice}/bin/alice`
3. `PATH` 里的 `alice`

## MCP 策略

Alice 不再通过 MCP 暴露业务能力。

当前策略：

- memory / scheduler / campaign / message 以 skills + runtime HTTP 为主路径。
- 仓库自带 skill 统一调用同一个 `alice` 二进制的 `runtime ...` 子命令。
- 现在残留的 `mcp` 命名只限于会话上下文环境变量，例如 `ALICE_MCP_RECEIVE_ID`，它们继续作为稳定的运行时上下文字段存在。

这样做的结果是：外置 skill、Alice 核心运行时和会话上下文注入链路共享同一份会话鉴权和消息 API，而不是各自复制一套逻辑。

## Debug Trace

当 `log_level=debug` 时，每次 agent 调用都会输出一份 Markdown trace，至少包含：

- provider
- agent 名称
- thread/session id
- model / profile
- 渲染后的输入
- 观察到的 tool 调用
- 最终输出或错误

覆盖范围：

- 普通 assistant 调用
- scheduler 触发的 `run_llm`
- 能暴露 tool 活动的后端适配层（`codex`、`kimi`，以及部分 `claude`）

## 本次已采用的库

实际落地使用：

- `github.com/Masterminds/sprig/v3`
- `github.com/cespare/xxhash/v2`
- `github.com/evanphx/json-patch/v5`
- `github.com/go-co-op/gocron/v2`
- `github.com/go-playground/validator/v10`
- `github.com/gin-gonic/gin`
- `github.com/go-resty/resty/v2`
- `github.com/oklog/run`
- `github.com/oklog/ulid/v2`
- `github.com/rs/zerolog`
- `github.com/spf13/cobra`
- `github.com/cenkalti/backoff/v4`
- `github.com/cyphar/filepath-securejoin`
- `go.etcd.io/bbolt`
- `gopkg.in/natefinch/lumberjack.v2`
- `gopkg.in/yaml.v3`

## 端到端链路

1. Feishu 事件进入 `internal/connector`。
2. Connector 按 session 串行调度，并组装本轮 agent 环境变量。
3. 环境变量里同时带上当前会话上下文和 runtime HTTP 鉴权信息。
4. LLM backend 从磁盘模板渲染 prompt，并调用 `codex` / `claude` / `gemini` / `kimi`。
5. agent 使用的外置 skill 通过 `alice runtime ...` 调用 runtime HTTP API。
6. runtime HTTP API 复用同一份 session context 操作 memory、automation、campaign 状态和消息发送。
7. automation task 通过 `bbolt` 持久化到 `automation.db`。
8. 运行时日志统一经由 `zerolog` 输出，可选文件滚动由 `lumberjack` 负责。
9. debug trace 以 Markdown 形式记录每次 agent 调用，便于追踪和审计。
