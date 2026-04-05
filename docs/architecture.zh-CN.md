# Alice 运行时架构

[English](./architecture.md)

本文档描述当前仓库里真实存在的 Alice 架构，而不是历史设计稿。文中提到的对象名、包名和路径都对应现在的 `cmd/connector`、`internal/`、`prompts/`、`skills/` 实现。

## 1. 进程模型

Alice 是一个多 bot runtime。一个 `alice` 进程可以从同一个 `config.yaml` 启动多个 bot。

启动时，进程会：

1. 读取 `config.yaml`
2. 把 `bots.*` 展开成多个 bot 级 runtime config
3. 按需检查 CLI 登录状态
4. 同步内嵌 bundled skills 到本地 skill 目录
5. 为每个 bot 构建一个 `ConnectorRuntime`
6. 用一个 `RuntimeManager` 统一托管这些 runtime

每个 bot 的主运行时对象是：

```text
ConnectorRuntime
  ├─ App
  ├─ Processor
  ├─ llm.MultiBackend
  ├─ LarkSender
  ├─ automation.Engine
  ├─ runtimeapi.Server
  ├─ automation.Store
  └─ campaign.Store
```

启动模式现在是显式的：

- `--feishu-websocket`：连真实飞书 WebSocket，处理在线消息
- `--runtime-only`：不连飞书，只运行 automation 和本地 runtime API
- `alice-headless`：只能配合 `--runtime-only` 使用，不能启动飞书连接

## 2. 启动与装配链路

进程入口在 `cmd/connector`。

关键装配职责：

- `cmd/connector/root.go`
  负责 CLI flag、启动模式选择、首次生成 config、PID 锁、日志初始化、认证预检查、bundled skill 同步，以及 runtime manager 启动。
- `internal/config`
  负责纯多 bot 配置模型、默认值、校验、路径推导，以及每个 bot 的 runtime 展开。
- `internal/bootstrap`
  负责构建每个 bot 的运行时对象，并把 prompt loader、runtime API 鉴权、campaign reconcile、config reload 等横切能力接起来。

`BuildRuntimeManager` 会先通过 `RuntimeConfigs()` 把总配置展开成多个 bot 级 `Config`，再逐个构建 `ConnectorRuntime`。

当前热更新行为：

- 单 bot 模式：支持部分配置热更新
- 多 bot 模式：故意关闭热更新；改配置后应直接重启进程

## 3. 运行目录与持久化状态

每个 bot 默认拥有自己的运行目录：

```text
${ALICE_HOME}/bots/<bot_id>/
```

重要路径：

- `workspace/`
  bot 工作区和 `SOUL.md`
- `prompts/`
  bot 级 prompt 覆盖目录
- `run/connector/automation.db`
  automation task 持久化库
- `run/connector/campaigns.db`
  轻量 campaign 索引库
- `run/connector/session_state.json`
  session alias、provider thread id、usage 统计、work-thread 元数据
- `run/connector/runtime_state.json`
  connector 自身的可变运行时状态
- `run/connector/resources/scopes/<scope_type>/<scope_id>/`
  当前会话范围内下载下来的附件，以及允许再上传回飞书的本地产物

源码树里还内嵌了这些资源：

- `prompts/`
- `skills/`
- `config.example.yaml`
- `SOUL.md.example`

prompt 的优先级是磁盘优先、内嵌兜底。

## 4. 包级职责图

核心包：

- `cmd/connector`
  CLI 入口、`runtime` 子命令、`skills sync`
- `internal/bootstrap`
  runtime 构建、路径解析、认证检查、skill 释放、campaign reconcile 桥接、config reload
- `internal/config`
  配置结构、校验、默认值、路径推导、多 bot 展开
- `internal/connector`
  飞书接入、消息归一化、scene 路由、排队、按 session 串行、抢占中断、prompt 组装、回复派发、附件下载、session 落盘、内建命令、可选图片生成
- `internal/llm`
  provider 无关的 backend 接口，以及 `codex` / `claude` / `gemini` / `kimi` 适配器
- `internal/prompting`
  模板加载器，支持磁盘优先 / 内嵌兜底、`sprig` helper、编译缓存
- `internal/runtimeapi`
  本地鉴权 HTTP server/client，供 bundled skills 和 runtime 脚本复用
- `internal/automation`
  task 模型、持久化、claim、执行、system task 调度、workflow dispatch
- `internal/campaign`
  会话范围内的轻量 campaign 索引
- `internal/campaignrepo`
  repo-first campaign 加载、校验、reconcile、dispatch 规划、post-run 校验、live-report 生成
- `internal/statusview`
  为 `/status` 等卡片聚合 usage、automation、campaign 视图
- `internal/imagegen`
  可选的 OpenAI 图片生成 / 编辑链路

支撑包：

- `internal/mcpbridge`
  会话上下文环境变量桥；虽然业务能力不再走 MCP，但 `ALICE_MCP_*` 命名仍然保留以兼容已有 skill
- `internal/runtimecfg`
  scene 到 profile 的解析，以及 thread reply 偏好判断
- `internal/sessionkey`
  canonical session key / visibility key 辅助函数
- `internal/messaging`
  connector 与 runtime API 共用的窄 sender/uploader 接口
- `internal/storeutil`
  bbolt 和字符串工具
- `internal/logging`
  zerolog 与滚动日志配置
- `internal/buildinfo`
  版本信息

## 5. 入站消息主链路

`internal/connector.App` 负责单个 bot 的飞书连接和作业队列。

高层流程：

1. 飞书通过 WebSocket 推送 `im.message.receive_v1`
2. `App` 把事件归一化成 `Job`
3. `routeIncomingJob` 决定这条消息是忽略、内建命令、`chat` 还是 `work`
4. job 入队
5. 同一个 session 上如果来了更新的 job，旧 job 会被打断
6. 被接受的 job 交给 `Processor` 执行

scene 路由规则：

- 群聊 / 话题群优先看 `group_scenes.chat` 和 `group_scenes.work`
- work thread 通过显式 trigger 和稳定的 work-scene session key 识别
- 如果两个 scene 都关掉，则回退到旧的 `trigger_mode` / `trigger_prefix`
- `/help`、`/status`、`/clear`、`/stop`、`/codearmy ...` 这类内建命令会直接绕过 LLM 主链路

## 6. Session Key、Alias 与串行控制

Alice 用 canonical session key 和 alias 做会话恢复与路由。

常见格式：

- `{receive_id_type}:{receive_id}`
- `{receive_id_type}:{receive_id}|scene:{scene}`
- `{receive_id_type}:{receive_id}|scene:{scene}|thread:{thread_id}`
- `{receive_id_type}:{receive_id}|scene:{scene}|message:{message_id}`

特殊格式：

- work scene 种子 key：`{receive_id_type}:{receive_id}|scene:work|seed:{source_message_id}`
- chat reset alias：`{chat_key}|reset:{message_id}`

`session_state.json` 里当前会落这些状态：

- provider thread id
- work-thread id alias
- session aliases
- usage 统计
- 最后消息时间
- status 聚合要用的 scope key

`internal/connector/runtime_store.go` 维护的是进程内协调状态：

- 每个 session 的最新版本号
- 每个 session 的 pending job
- 当前活跃运行的 cancel handle
- 每个 session 的串行 mutex
- superseded version 跟踪

## 7. Prompt 组装与 LLM 执行

`internal/connector.Processor` 是单条 job 的执行核心。

在调用 LLM 前，它会：

- 按需读取并解析 `SOUL.md`
- 把入站附件下载到当前 scope 的资源目录
- 组装当前会话的运行环境变量
- 生成 prompt 文本

当前 prompt 资源：

- `prompts/llm/initial_prompt.md.tmpl`
- `prompts/connector/bot_soul.md.tmpl`
- `prompts/connector/current_user_input.md.tmpl`
- `prompts/connector/reply_context.md.tmpl`
- `prompts/connector/runtime_skill_hint.md.tmpl`
- `prompts/connector/synthetic_mention.md.tmpl`
- `prompts/campaignrepo/planner_dispatch.md.tmpl`
- `prompts/campaignrepo/planner_reviewer_dispatch.md.tmpl`
- `prompts/campaignrepo/executor_dispatch.md.tmpl`
- `prompts/campaignrepo/reviewer_dispatch.md.tmpl`
- `prompts/campaignrepo/wake_dispatch.md.tmpl`

当前 prompt 行为里最关键的点：

- 首轮或未恢复 thread 的执行，会渲染 current-user-input，并按情况追加 reply context、bot soul、runtime skill hint
- 已恢复 provider thread 的执行，只发送当前用户输入；上下文主要依赖 provider 自己的 thread/session
- `chat` 可以注入 `SOUL.md`
- `work` 则故意跳过 bot soul 注入

LLM 选择链路：

1. 先由 scene 选出外层 `llm_profiles.<name>`
2. 该 profile 决定 provider、model、profile、reasoning、personality、prompt prefix
3. `llm.MultiBackend` 再把请求派发到具体 provider adapter

当前 provider：

- `codex`
- `claude`
- `gemini`
- `kimi`

## 8. 回复派发与可选图片生成

Alice 现在明确区分：

- 立即 ack
- backend 流式进度消息
- 最终回复
- 文件 / 图片后续发送

当前行为：

- work 场景通常会先发 reaction 或 `收到！`
- backend 进度消息尽量走 threaded reply
- 最终回复经由 reply dispatcher 发出
- 如果飞书目标不支持 thread reply，会自动回退成普通 reply
- 文本回复完成后，可选触发角色生图链路

`internal/connector/sender.go` 及相关文件负责：

- send / reply / patch card
- reaction
- 图片和文件上传
- 入站附件下载
- 按 scope 解析资源目录

## 9. Runtime API 与 Bundled Skills

Alice 暴露本地鉴权 runtime API，主要给 bundled skills 和 runtime 脚本使用。

当前 HTTP 面：

- `POST /api/v1/messages/image`
- `POST /api/v1/messages/file`
- `GET|POST|PATCH|DELETE /api/v1/automation/tasks`
- `GET|POST|PATCH|DELETE /api/v1/campaigns`

当前 runtime API 里没有独立的 text-send 接口。纯文本回复正常应走主回复链路；message API 主要用于附件和附件 caption。

当前防护：

- bearer token 鉴权
- request body 大小限制
- 进程内 auth rate limit
- 上传前会校验本地路径必须落在当前 session 的 resource root 下

runtime 脚本入口：

- `alice runtime message ...`
- `alice runtime automation ...`
- `alice runtime campaigns ...`

当前树里实际自带的 bundled skill：

- `skills/alice-message`
- `skills/alice-scheduler`
- `skills/alice-code-army`
- `skills/file-printing`

运行时上下文通过这些变量注入：

- `ALICE_RUNTIME_API_BASE_URL`
- `ALICE_RUNTIME_API_TOKEN`
- `ALICE_RUNTIME_BIN`
- `ALICE_MCP_RECEIVE_ID_TYPE`
- `ALICE_MCP_RECEIVE_ID`
- `ALICE_MCP_SOURCE_MESSAGE_ID`
- `ALICE_MCP_ACTOR_USER_ID`
- `ALICE_MCP_ACTOR_OPEN_ID`
- `ALICE_MCP_CHAT_TYPE`
- `ALICE_MCP_SESSION_KEY`

## 10. Automation 子系统

`internal/automation` 用 bbolt 持久化 task，并在进程内执行。

当前 task scope：

- `user`
- `chat`

当前 action 类型：

- `send_text`
- `run_llm`
- `run_workflow`

执行模型：

- 周期性 tick claim 到期任务
- 长驻 system task 单独调度
- task env 会继承和交互式运行相同的会话上下文桥
- workflow task 复用同一套 LLM backend，但使用 workflow 专属的 agent name、env 和 workspace hint

bootstrap 阶段注册的 system task：

- 周期性 flush session/runtime state
- 周期性 campaign-repo reconcile

## 11. Campaign 索引与 Repo-First 编排

Alice 的 code-army 路径现在明确分成两层。

runtime 层：

- `internal/campaign`
  保存会话范围内的轻量 campaign 记录
- `internal/runtimeapi/campaign_handlers.go`
  对外暴露这些记录的 CRUD 和管理入口

repo-first 层：

- `internal/campaignrepo`
  负责读取 campaign repo、校验 contract、推进状态、生成 dispatch spec、刷新 live report、应用 review verdict、修复非法 task 状态、恢复 wake task，并做 post-run 校验

桥接层：

- `internal/bootstrap/campaign_repo_runtime.go`
  把 reconcile 结果接到 runtime automation task、通知、启动恢复、wake 调度和 runtime summary 更新上
- `internal/bootstrap/campaign_repo_workflow_guard.go`
  在 plan gate 和 terminal campaign 周围阻止或改写不合法的 workflow 运行

设计原则：

- runtime DB 只是轻量索引层
- campaign repo 才是主事实源
- source repo 才是真实代码改动面

## 12. 配置模型

当前配置模型是纯多 bot。

关键配置项：

- `bots.<id>`
- `llm_profiles`
- `group_scenes.chat`
- `group_scenes.work`
- `permissions`
- `campaign_role_defaults`
- `runtime_http_addr`
- `workspace_dir`
- `prompt_dir`
- `codex_home`

值得强调的行为：

- `RuntimeConfigs()` 会补齐 bot 默认路径，并给多个 bot 递增默认 runtime API 端口
- 外层 `llm_profiles` key 是稳定的运行时选择器
- provider-specific profile selector 仍然放在每个 profile 的内层 `profile` 字段
- runtime permission 会分别控制 bundled skill 暴露面和 runtime API 能力

当前 work 型 profile 的默认执行姿态是有意偏宽松的。比如 Claude 和 Kimi 的默认值会贴合它们当前 CLI 的非交互行为，Codex 的 work profile 也经常配成 `danger-full-access` + `never`。如果运行环境更敏感，应在 profile 级显式收紧 permissions。

## 13. 可观测性与调试

当前可观测面：

- `zerolog` 结构化日志
- `lumberjack` 滚动日志文件
- `session_state.json` 中的 usage 计数
- `/status` 和 `/codearmy ...` 内建卡片，底层由 `statusview` 聚合
- `log_level=debug` 时的每轮 markdown trace

debug trace 在 backend 能暴露时会包含：

- provider
- agent 名称
- thread/session id
- model/profile
- 渲染后的输入
- 观察到的 tool 活动
- 最终输出或错误

## 14. 当前受支持的扩展边界

当前代码里真正受支持的扩展面是：

- `llm` provider adapter
- `prompts/` 下的模板
- `skills/` 下的 bundled skill
- runtime API handler
- `internal/campaignrepo` 的 repo-first 编排逻辑

当前实现里明确不存在的东西：

- 没有在用的 `internal/memory` 包
- 没有 runtime memory API
- 没有承载业务逻辑的 MCP server；只保留了向后兼容的 session env 命名
