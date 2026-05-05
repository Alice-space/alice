# 架构概览

这是面向代码的 Alice 架构参考。包名、运行时对象和文件路径与 `cmd/connector`、`internal/`、`prompts/` 和 `skills/` 下的实际代码一致。

## 阅读路径

根据你的目标从对应部分开始：

| 目标 | 入口 |
|------|-----------|
| 理解整个系统 | §1 进程模型 → §2 引导路径 → §5 消息流水线 |
| 新增 LLM 后端 | §2 引导路径 → §7 Prompt 拼装 → [新增 LLM Backend](add-backend.md) |
| 修改消息处理 | §5 收信消息流水线 → §6 Session Key → §8 回复分发 |
| 新增 Runtime API 端点 | §9 Runtime API |
| 新增或修改自动化 | §10 自动化子系统 |
| 理解配置 | §2 引导路径 → §12 配置模型 |

## 1. 进程模型

Alice 是一个多 bot 运行时。一个 `alice` 进程可以通过一份 `config.yaml` 托管多个 bot。

启动时，进程：

1. 加载 `config.yaml`
2. 将 `bots.*` 展开为每个 bot 的运行时配置
3. 按需验证 CLI 认证
4. 将内嵌 bundled skill 同步到本地 skill 目录
5. 为每个 bot 构建一个 `ConnectorRuntime`
6. 在一个 `RuntimeManager` 下运行所有运行时

每个 bot 的主运行时对象：

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

启动模式是显式的：
- `--feishu-websocket`：连接飞书并处理实时事件
- `--runtime-only`：运行自动化和本地 Runtime API，不启动飞书 WebSocket
- `alice-headless`：仅 runtime-only；不得启动飞书连接器

## 2. 引导路径

进程入口点是 `cmd/connector`。

关键引导步骤：
- `cmd/connector/root.go`：CLI 参数、启动模式选择、配置创建、PID 锁定、日志、认证预检、bundled skill 同步和运行时管理器启动。
- `internal/config`：纯多 bot 配置模型、路径派生、标准化、验证和每个 bot 的运行时展开。
- `internal/bootstrap`：构建每个 bot 的运行时图并连接横切功能，如 prompt 加载、Runtime API 认证、campaign 调和循环和配置热重载。

`BuildRuntimeManager` 通过 `RuntimeConfigs()` 将 `Config` 展开为 `[]Config`，然后为每个 bot 构建一个 `ConnectorRuntime`。

当前热重载行为：
- 单 bot 模式：支持部分配置热重载
- 多 bot 模式：热重载被刻意禁用；配置更改后重启进程

## 3. 运行时布局与持久化状态

每个 bot 拥有自己的运行时根目录：

```text
${ALICE_HOME}/bots/<bot_id>/
```

重要的每个 bot 路径：
- `workspace/` — Bot 工作空间
- `prompts/` — 该 bot 的可选 prompt 覆盖
- `run/connector/automation.db` — 持久化自动化任务存储（bbolt）
- `run/connector/campaigns.db` — 持久化轻量级 campaign 索引（bbolt）
- `run/connector/session_state.json` — Session 别名、provider thread id、用量计数器、work-thread 元数据
- `run/connector/runtime_state.json` — 可变连接器运行时状态
- `run/connector/resources/scopes/<scope_type>/<scope_id>/` — 下载的附件和可上传的本地产物，作用域限定在当前对话

源码树也内嵌了：
- `prompts/`
- `skills/`
- `config.example.yaml`
- `prompts/SOUL.md.example`

磁盘文件在存在时会覆盖内嵌 prompt 文件；内嵌资产是后备方案。

## 4. 包映射

### 核心包

| 包 | 职责 |
|---------|---------------|
| `cmd/connector` | CLI 入口、`runtime` 子命令和 `skills sync` |
| `internal/bootstrap` | 运行时构建、路径解析、认证检查、skill 物化、campaign 调和桥接和配置重载 |
| `internal/config` | 配置模式、验证、默认值、路径派生和多 bot 展开 |
| `internal/connector` | 飞书收信、消息标准化、场景路由、排队、session 序列化、原生注入回退、`/stop` 中断、prompt 拼装、回复分发、附件下载、session 持久化和内置命令 |
| `internal/llm` | 与 provider 无关的 Backend 接口及 `codex`、`claude`、`gemini`、`kimi`、`opencode` 的 provider 适配器 |
| `internal/prompting` | 模板加载器（磁盘优先 / 内嵌后备）、`sprig` 辅助函数和编译模板缓存 |
| `internal/runtimeapi` | 本地认证 HTTP 服务器和客户端，供 bundled skill 和面向运行时的 shell 脚本使用 |
| `internal/automation` | 任务模型、持久化、认领、执行、系统任务调度和工作流分派 |
| `internal/statusview` | 为 `/status` 聚合用量和自动化数据 |
| `internal/platform/feishu` | 飞书发送器实现、附件 I/O、bot 自我信息查找、消息查找和用户名解析辅助 |

### 支持包

| 包 | 职责 |
|---------|---------------|
| `internal/sessionctx` | Runtime API 调用和 bundled skill 的 session 上下文环境桥接 |
| `internal/runtimecfg` | 场景派生的 profile 选择和话题回复偏好的辅助 |
| `internal/sessionkey` | 规范 session key 和可见性 key 的辅助 |
| `internal/messaging` | 连接器和 Runtime API 层共享的窄发信/上传接口 |
| `internal/storeutil` | 共享 bbolt 辅助和字符串工具 |
| `internal/logging` | Zerolog 加滚动文件输出配置 |
| `internal/buildinfo` | 版本报告 |

## 5. 收信消息流水线

`internal/connector.App` 拥有实时飞书连接和每个 bot 的 job 队列。

高层流程：

1. 飞书通过 WebSocket 投递 `im.message.receive_v1`
2. `App` 将事件标准化为 `Job`
3. `routeIncomingJob` 决定消息应被忽略、作为内置命令处理、作为 `chat` 处理还是作为 `work` 处理
4. 如果同一 session 有活跃的 provider 原生交互式运行，Alice 首先尝试将新输入注入到该运行中
5. 如果原生注入不可用，job 排队并按 session 序列化；较新的排队 job 取代较旧 job，不中断活跃的 LLM 运行
6. `/stop` 仍会中断活跃运行，用户消息仍可中断获取了 session 锁的自动化任务
7. `Processor` 执行被接受的 job

场景路由规则：
- 群聊/话题群可以使用 `group_scenes.chat` 和 `group_scenes.work`
- Work 话题通过触发器加上稳定的 work-scene session key 来标识
- 如果两个场景都被禁用，Alice 回退到旧版 `trigger_mode` / `trigger_prefix`
- 内置命令如 `/help`、`/status`、`/clear`、`/stop` 绕过 LLM 路径

## 6. Session Key、别名和序列化

Alice 通过规范 session key 和别名来路由和恢复工作。

常见格式：
- `{receive_id_type}:{receive_id}`
- `{receive_id_type}:{receive_id}|scene:{scene}`
- `{receive_id_type}:{receive_id}|scene:{scene}|thread:{thread_id}`
- `{receive_id_type}:{receive_id}|scene:{scene}|message:{message_id}`

特殊情况：
- Work-scene 种子 key：`{receive_id_type}:{receive_id}|scene:work|seed:{source_message_id}`
- Chat 重置别名：`{chat_key}|reset:{message_id}`

持久化到 `session_state.json`：
- Provider thread id
- Work-thread id 别名
- Session 别名
- 用量计数器
- 最后消息时间戳
- 状态聚合的作用域 key

`internal/connector/runtime_store.go` 维护实时的内存协调状态：
- 每个 session 的最新版本
- 每个 session 的待处理 job
- 活跃运行取消句柄
- 每个 session 的序列化互斥锁
- 已取代版本跟踪

## 7. Prompt 拼装与 LLM 执行

`internal/connector.Processor` 是每个被接受 job 的执行核心。

在 LLM 调用之前它：
- 如果需要，加载并解析 `SOUL.md`
- 将收到的附件下载到作用域资源目录
- 为当前对话推导运行时环境变量
- 准备 prompt 文本

当前 prompt 资产：
- `prompts/llm/initial_prompt.md.tmpl`
- `prompts/connector/bot_soul.md.tmpl`
- `prompts/connector/current_user_input.md.tmpl`
- `prompts/connector/reply_context.md.tmpl`
- `prompts/connector/runtime_skill_hint.md.tmpl`
- `prompts/connector/synthetic_mention.md.tmpl`

重要 prompt 行为：
- 首轮或非恢复运行渲染 current-user-input 模板，并可能附加回复上下文、bot soul 和运行时 skill 提示
- 已恢复的 provider thread 仅发送当前用户输入；Alice 依赖 provider 端 thread/session 持有之前的上下文
- `chat` 运行可在 resume 时前置 `SOUL.md`；`work` 运行刻意跳过 bot-soul 注入

LLM 层选择方式：
1. 场景选择一个外部 `llm_profiles.<name>`
2. 外部 profile 选择 provider / model / profile / reasoning / personality / prompt prefix
3. `llm.MultiBackend` 分派到正确的 provider 适配器

当前支持的 provider：`codex`、`claude`、`gemini`、`kimi`、`opencode`

## 8. 回复分发

Alice 区分：
- 即时确认
- 来自后端的流式进度消息
- 最终回复
- 文件/图片跟进

当前行为：
- Work-scene 消息通常收到即时 reaction 或 `收到！`
- 后端进度消息在可能时以话题回复方式发送
- 最终回复通过回复分发器发布
- 当飞书不支持对该目标的话题回复时，回退到直接回复

`internal/connector/card.go`、`internal/connector/outgoing_mentions.go`、`internal/connector/outgoing_plaintext.go` 及相关文件拥有：
- 消息发送 / 回复 / 补丁卡片操作
- Reaction
- 图片和文件上传
- 附件下载
- 作用域资源根解析

## 9. Runtime API 与 Bundled Skill

Alice 暴露一个本地认证的 Runtime API，面向 bundled skill 和薄运行时脚本。

当前 HTTP 接口：
- `POST /api/v1/messages/image`
- `POST /api/v1/messages/file`
- `GET|POST|PATCH|DELETE /api/v1/automation/tasks`
- `GET|POST /api/v1/goal` + pause/resume/complete/delete

没有独立的纯文本发送端点。纯文本通常通过主回复流水线返回。

当前安全措施：
- Bearer token 认证
- 请求体大小限制（1 MB）
- 进程内认证速率限制（120 req/min）
- 本地上传需要可读、非空的常规文件，且仍受飞书大小限制约束

面向运行时的 shell 入口点：
- `alice runtime message ...`
- `alice runtime automation ...`
- `alice runtime goal ...`

当前源码树中内置的 bundled skill：
- `skills/alice-message`
- `skills/alice-scheduler`
- `skills/alice-goal`

运行时上下文通过环境变量注入（见 [Runtime API 设计](../explanation/runtime-api-design.md)）。

## 10. 自动化子系统

`internal/automation` 将任务持久化在 bbolt 中并在进程内执行。

当前任务作用域：`user`、`chat`
当前任务操作：`send_text`、`run_llm`、`run_workflow`

执行模型：
- 到期任务在周期性 tick 中被认领
- 长期系统任务单独调度
- 任务环境继承与交互式运行相同的对话上下文桥接
- Workflow 任务调用相同的 LLM 后端，但使用 workflow 专属的 agent name、环境变量和工作空间提示

引导期间注册的内建系统任务：
- 周期性 session/runtime 状态刷新
- 周期性 campaign-repo 调和

## 11. 配置模型

配置模型是纯多 bot 的。

重要 key：
- `bots.<id>`
- `llm_profiles`
- `group_scenes.chat`、`group_scenes.work`
- `private_scenes.chat`、`private_scenes.work`
- `permissions`
- `runtime_http_addr`
- `workspace_dir`、`prompt_dir`、`codex_home`

值得注意的行为：
- `RuntimeConfigs()` 为 bot 派生缺失路径，并跨 bot 递增默认 Runtime API 端口
- 每个外部 `llm_profiles` key 是一个稳定的运行时选择器
- Provider 专属的 profile 选择器仍通过内部 `profile` 字段存在于每个 profile 内部
- 运行时权限独立控制 bundled skill 和 Runtime API 接口

## 12. 可观测性与调试

当前可观测性接口：
- 通过 `zerolog` 的结构化日志
- 通过 `lumberjack` 的滚动日志文件
- 存储在 `session_state.json` 中的 session 用量计数器
- 由 `statusview` 驱动的 `/status`
- `log_level=debug` 时每次运行的 markdown debug 跟踪

Debug 跟踪在后端暴露时包括：
- Provider、agent name、thread/session id、model/profile
- 渲染后的输入、观察到的工具活动、最终输出或错误

## 13. 扩展边界

支持的扩展面：
- `llm` provider 适配器
- `prompts/` 下的 prompt 模板
- `skills/` 下的 bundled skill
- Runtime API handlers
