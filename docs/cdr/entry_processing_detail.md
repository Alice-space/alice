# ReceptionAgent 入口处理概念设计

本文基于最新的 `draft.md` 展开，只细化“入口信息触发”这一步，同时把新增的全局约束同步进来，不修改人工设计文档，也不下沉到实现细节。

## 1. 目标与范围

当前 `draft.md` 已明确三件事：

- 多来源用户输入统一先进入 BUS
- `ReceptionAgent` 负责入口识别与分流
- 代码类任务不在入口内直接完成，而是先在 BUS 建立任务，再镜像到 issue，后续仍以 BUS 状态为准

因此，这份文档只回答下面几个问题：

1. `ReceptionAgent` 的输入输出是什么。
2. 入口层如何在同步闭环和异步代码任务之间分流。
3. BUS、Issue、PR、评论在入口阶段分别扮演什么角色。
4. 入口阶段最少需要哪些状态、审批和异常策略。
5. 入口阶段与后续 issue 驱动工作流如何衔接。

这份文档不覆盖：

- Planner、审核、编码、PR 合并的完整生命周期
- MCP 具体协议
- JSONL 持久化格式

这些内容在新的 issue 工作流概念设计文档里继续展开。

## 2. 系统前提

入口概设需要服从最新 `draft.md` 的全局前提。

### 2.1 状态真源

- BUS 中的任务状态是系统真实状态
- Issue、PR、评论主要是人类协作界面和外部事件来源
- 外部平台上的表现不直接替代 BUS 内状态

这意味着入口层在接受异步代码任务时，必须先让 BUS 中出现正式任务状态，再去创建 issue 镜像，而不是反过来。

### 2.2 BUS 一致性与副作用控制

- 第一版 BUS 在概念上分为 `Event Bus` 和 `State Store` 两层
- `Event Bus` 负责事件流转，`State Store` 负责真实状态、持久化和只读视图
- 同一 `task_id` 的事件按顺序串行处理，状态推进必须检查当前状态和对象版本
- `task_version` 负责单任务乐观锁和并发写保护；`global_hlc` 预留给跨任务因果追踪与后续分布式扩展
- 所有外部事件都按“至少一次投递”处理，必须有稳定外部事件ID并做幂等去重
- 所有外部副作用都先写入持久化 `outbox`，再通过 MCP 执行，完成后再回写 BUS
- 当分片队列或单任务积压超过阈值时，BUS 进入背压，对低优先级流量执行拒绝或延迟入队，并持续告警

这意味着入口层虽然看起来像“接待 Agent”，但真正拥有状态推进权和副作用出站权的，是 BUS 内部的 `State Store` 与策略层，而不是 `ReceptionAgent` 直接改共享状态。

### 2.3 执行边界

- 所有涉及仓库、凭据、系统设置的 Agent 都运行在受限沙箱中
- 访问外部系统统一通过 MCP

对入口层来说，这意味着：

- 同步直接处理只能发生在受控沙箱内
- 设置修改、记忆修改、定时任务、用量查询等能力都应通过 MCP 访问
- 即使是创建 issue，也应被视为一次 MCP 驱动的外部操作
- 所有来自飞书、Web、IM 和仓库评论的自然语言输入都只是不可信候选意图，真正的高风险动作仍需 BUS 策略层二次校验和确认

### 2.4 架构定位

- 第一版是单二进制系统
- BUS、任务状态、Webhook、定时任务内置在单二进制中
- 持久化以人类可读 JSONL 追加日志加周期性快照为主
- `outbox`、去重记录、定时任务、确认令牌等也属于需要持久化的入口相关对象

入口层因此是单二进制内部的一个接待与路由模块，而不是一个独立控制平面。

## 3. 上下文图

```text
[Feishu / Web Admin / Other IM]
                |
                | UserRequest
                v
      [BUS / Policy / State Store]
                |
                | UserRequest
                v
        [ReceptionAgent]
          |        |        |
          |        |        +------------------------------+
          |        |                                       |
          |        | Task(control)                         | Task(async_code)
          |        v                                       v
          | [Alice Control MCP]                   [GitHub / GitLab MCP]
          |        |                                       |
          |        | StatusUpdate / Result                 | issue mirror created
          |        v                                       v
          +-----------------------> [BUS] <----------------+
                                      |
                                      | StatusUpdate / Result
                                      v
                               [Reply Consumer]
                                      |
                                      v
                        [Feishu / Web Admin / Other IM]
```

这张图表达四个关键点：

- `ReceptionAgent` 的直接上游是 BUS，而不是具体 IM 平台。
- 同步控制动作和异步 issue 创建都属于“通过 MCP 访问外部能力”。
- 对异步代码任务，先有 BUS 中的 `Task`，再有外部 issue 镜像。
- 对用户和其他组件可见的真实进度，统一通过 BUS 回写。

## 4. 最小概念对象

入口层至少需要七个统一概念对象。

| 对象 | 含义 | 由谁产生 | 由谁消费 |
| --- | --- | --- | --- |
| `UserRequest` | 对外部用户输入的统一抽象，承载来源、上下文、原始意图和关联标识 | 接入侧或 BUS 入口适配层 | `ReceptionAgent` |
| `RouteDecision` | `ReceptionAgent` 对请求做出的识别结果，至少包含任务类别、风险等级、建议路径、是否建议等待人工，以及是否适合与前序请求合并 | `ReceptionAgent` | BUS 策略层、同步处理分支、控制类 MCP、异步分发分支 |
| `Task` | 被 BUS 正式接受并进入系统生命周期管理的任务对象 | BUS `State Store` | 后续 Agent、观测系统、Reply Consumer |
| `ExternalIssueBinding` | `task_id` 与外部 issue 的绑定对象；既可以表示“直接绑定已有 GitHub / GitLab issue”，也可以表示“为非 issue 来源任务成功创建镜像后形成绑定” | BUS `State Store` / Git 平台桥接层 | Planner、展示区、后续 issue 工作流 |
| `Confirmation` | 一次性人工确认对象，承载确认人、过期时间、鉴权快照和 `confirmation_token` | BUS 策略层 | 人类确认链路、入口工作流 |
| `ScheduledTask` | 通过入口创建的定时任务定义，由 scheduler 到点触发生成普通任务 | BUS 策略层 / scheduler | scheduler、观测系统 |
| `StatusUpdate` | 对任务某一状态的统一回写事件，可附带结果摘要或失败原因 | `ReceptionAgent`、MCP 执行方、后续 Agent | BUS、Reply Consumer、观测系统 |

这些对象的关系是：

`UserRequest -> RouteDecision -> Task -> StatusUpdate`

其中要特别强调：

- 对同步一次性任务，`Task` 是短生命周期对象。
- 对异步代码任务，`Task` 先在 BUS 中建立，再由 issue 作为外部协作镜像。
- 对高风险副作用，请求会先转成 `Confirmation`，待确认通过后再继续推进。

## 5. ReceptionAgent 的输入输出契约

从概念上，`ReceptionAgent` 的输入和输出应保持非常清晰。

`ReceptionAgent` 自身只负责三件事：

- 识别任务类型
- 做风险分级和上下文整理
- 产出建议性的 `RouteDecision`

它不直接决定“是否允许调用工具”或“是否可以跳过审批”；这些都由 BUS 内策略层根据身份、风险级别和策略配置裁决。
它也不能把用户的自然语言原文直接当作执行指令；自然语言只用于生成候选 `RouteDecision`。

输入：

- 来自 BUS 的 `UserRequest`

输出：

- 一份 `RouteDecision`
- 一份已经被 BUS 接受的 `Task`；第一版要求所有入口请求在进入执行、等待或异步分发前，都先创建或绑定正式 `Task` 并分配 `task_id`
- 至少一条 `StatusUpdate`

为避免路由结果无限膨胀，`RouteDecision` 建议只保留四种结果：

- `sync_direct`：在入口同步闭环
- `sync_mcp`：交给 `Alice Control MCP`，仍按同步路径闭环
- `async_issue`：先在 BUS 中创建代码任务，再镜像到 issue
- `wait_human`：当前不执行，进入等待人类补充信息、身份校验或一次性确认令牌回流

## 6. 路由设计原则

### 6.1 总原则

入口分流优先追求三件事：

- 稳定
- 可解释
- 安全

第一版不追求高度智能的动态编排，而是先守住清晰边界。

此外还要补一条：

- 是否真的执行外部副作用，由 BUS 策略层决定，而不是 `ReceptionAgent` 单独决定
- 同一会话短时间内的同类修改请求可以按 `coalescing_key` 合并，优先避免重复拉起同步执行或重复补建 issue

### 6.2 第一版边界

结合最新 `draft.md`，第一版可采用如下边界：

- 简单查询和单步控制类任务，优先走同步
- 明确代码任务，统一走 `async_issue`
- 信息不足、高风险或超出同步边界但又不是代码任务的请求，不强行塞进 issue 通道，优先走 `wait_human`

这条边界很重要，因为当前 draft 中定义的异步主通道，是“代码任务异步通道”，而不是“所有复杂任务的统一延迟通道”。

## 7. 任务分类与路由决策矩阵

| 任务类型 | 典型判定信号 | 默认路径 | 升级/降级条件 | 兜底策略 |
| --- | --- | --- | --- | --- |
| 简单查询 / 轻量分析 | 单轮可回答；无持久副作用；不依赖仓库改动 | `sync_direct` | 若执行中发现问题扩展为多阶段调查，则停止扩展 | 返回“范围过大，请缩小问题” |
| 长耗时查询 / 大范围调研 | “全面调研”“持续跟踪”“跨大量来源汇总”“生成长报告” | `wait_human` | 若用户收敛为单轮可答问题，可降为 `sync_direct` | 第一版不自动转 issue，避免把非代码任务混入代码通道 |
| 单步 Alice 控制操作 | 修改设置、修改记忆、发布定时任务、查询用量等明确控制意图 | `sync_mcp` | 若请求变成多步复合操作，则不再视为单步控制 | 返回“请拆分成更明确的控制指令” |
| 高风险副作用操作 | 修改关键设置、敏感记忆、集群操作、创建高影响定时任务等，需要鉴权或审批 | `wait_human` | 若身份校验通过且确认令牌有效，可升级为 `sync_mcp` | 默认不执行副作用，先请求确认 |
| 多步工具编排 | 一个请求要求查询、判断、改设置、再发任务等串联动作 | `wait_human` | 若可收敛为一个主动作加少量前置判断，可降为 `sync_direct` 或 `sync_mcp` | 第一版不在入口承担复杂编排 |
| 带文件但只读任务 | 上传日志、配置、文档，希望解释、总结、比对，但不要求改代码 | `sync_direct` | 若范围扩大为仓库级排查或持续跟进，则升级为 `wait_human` | 保持只读，不自动进入 issue |
| 代码修改 / 调试 / 测试 / 构建 / 仓库操作 | “改代码”“修 bug”“补测试”“提交 patch”“创建 PR”“改仓库文件” | `async_issue` | 若最终确认只是阅读解释且不做改动，可降为 `sync_direct` | 默认进入 BUS 任务 + issue 镜像流程 |
| 信息不足 / 无法稳定分类 | 语义模糊、意图冲突、上下文缺失 | `wait_human` | 若补充上下文后可识别，再进入对应路径 | 默认不产生副作用；若存在明显代码信号，则按 `async_issue` 接收 |

这张表体现三个边界：

- “复杂”不等于“一律异步”
- issue 通道在第一版主要承接代码任务
- 不确定时优先避免副作用，而不是强行猜测

## 8. 入口阶段最小状态模型

由于最新 `draft.md` 明确“BUS 中的任务状态是系统真实状态”，入口阶段最小状态模型需要和下游 issue 工作流共享同一组核心状态名。第一版还额外规定：所有入口请求在进入任一执行、等待或异步分发分支前，都必须先由 BUS 创建或绑定正式 `Task` 并拿到 `task_id`。因此这里要先把“入口请求被接住”“已完成分类”“BUS 已建正式任务”“等待 issue 镜像完成”和“issue 镜像已建立”区分开。

### 8.1 状态语义

| 状态 | 含义 | 推进者 | 适用路径 |
| --- | --- | --- | --- |
| `received` | 请求已进入系统并被 ReceptionAgent 接住 | `ReceptionAgent` | 全部 |
| `classified` | 已完成任务识别与路由判断 | `ReceptionAgent` | 全部 |
| `task_created` | BUS 已正式接受该入口请求，并创建或绑定正式 `Task` 与 `task_id` | BUS `State Store` | 全部 |
| `waiting_human` | 等待人类补充信息、完成身份校验或提交一次性 `confirmation_token`，并携带 `waiting_reason` | BUS 策略层 / 人类确认链路 | 全部 |
| `sync_running` | 入口同步链路正在执行，包括直接处理和单步 MCP 转发 | `ReceptionAgent` / `Alice Control MCP` | 同步 |
| `issue_sync_pending` | BUS 任务已存在，但当前请求并非来自现成 issue，因此系统正在创建或补建外部 issue 镜像 | `ReceptionAgent` / Git 平台桥接组件 | 异步 |
| `issue_created` | `ExternalIssueBinding` 已存在；要么直接绑定了已有外部 issue，要么镜像已成功建立，可供后续 hook 接管 | `ReceptionAgent` 或 Git 平台桥接组件 | 异步 |
| `closed` | 入口阶段已经完成当前请求处理，或已完成与下游状态机共享的终态收口 | 当前执行主体 | 同步 |
| `failed` | 入口阶段终止，且已有明确失败原因 | 当前执行主体 | 全部 |
| `cancelled` | 请求被人类取消，或等待人工超时后按策略取消 | 人类或 BUS 策略层 | 全部 |

### 8.2 状态流转图

```text
                    +--------------------+
                    |      received      |
                    +---------+----------+
                              |
                              v
                    +--------------------+
                    |     classified     |
                    +---------+----------+
                              |
                              v
                    +--------------------+
                    |    task_created    |
                    +----+-------+---+---+
                         |       |   |
                         |       |   +--------------------+
                         |       |                        |
                         |       v                        v
                         |  +---------------+    +--------------------+
                         |  | issue_created |    | issue_sync_pending |
                         |  +-------+-------+    +----+-----------+---+
                         |          |                 |           |
                         v          |                 |           v
                +----------------+  |                 |    +---------------+
                |  sync_running  |  |                 |    |    failed     |
                +----+-------+---+  |                 |    +---------------+
                     |       |      |                 |
                     |       v      |                 v
                     |    +--------+ |          +---------------+
                     |    | failed | |          | issue_created |
                     |    +--------+ |          +-------+-------+
                     |               |                  |
                     v               v                  v
               +--------+     handoff to issue  handoff to issue
               | closed |     driven workflow   driven workflow
               +--------+

                    task_created
                         |
                         v
                +----------------+
                | waiting_human  |
                +----+-------+---+
                     |       |
                     |       v
                     |   +----------+
                     |   |cancelled |
                     |   +----------+
                     |
                     v
                 classified
```

### 8.3 状态模型设计原则

- `received` 和 `classified` 是统一前缀状态。
- 除命中去重并直接复用既有 `task_id` 的情况外，所有入口请求都必须在 `classified` 之后进入 `task_created`，再分流到同步、等待人工或异步 issue 路径。
- `waiting_human` 用于承载澄清、身份校验、审批和一次性确认令牌回流。
- `waiting_human` 必须携带明确的 `waiting_reason`，至少区分信息补充、风险确认、预算决策和恢复处理，方便展示区排序与通知。
- `task_created` 对同步、等待人工和异步代码任务都适用；区别只在于后续进入哪个分支。
- `task_created` 先于 `issue_created`，体现 BUS 才是真实状态。
- 如果请求本身来自 GitHub / GitLab 上已经存在的 issue，则 BUS 应直接创建 `ExternalIssueBinding` 并进入 `issue_created`，而不是再走 `issue_sync_pending`。
- `issue_sync_pending` 是一个可恢复状态，只用于“非 issue 来源任务需要新建或补建外部 issue 镜像”的补偿过程。
- `issue_sync_pending` 的补偿应采用指数退避；若外部平台返回 403、404 等明确致命错误，应尽快结束补偿并上报，而不是无限重试。
- 同步请求和 `wait_human` 请求统一以短生命周期 `Task` 承载，便于复用统一观测、告警、超时/重试和回复链路。
- 所有真实状态推进都应通过 `State Store` 完成，同一 `task_id` 不允许多个执行主体并发改状态。
- `issue_created` 是入口阶段的异步交接点，不是整个代码任务生命周期的终点。

## 9. 回写 BUS 的原则

`StatusUpdate` 至少承担三种作用：

- 给回复链路提供用户可见反馈
- 给系统提供可观测生命周期信号
- 给后续 Planner / Audit / Coding 工作流提供真实起点

因此入口层至少要保证：

- 一旦接住请求，就写 `received`
- 一旦形成分流判断，就写 `classified`
- 一旦 BUS 正式接受请求，就写 `task_created` 并分配或绑定正式 `task_id`
- 一旦需要澄清、审批或身份确认，就写 `waiting_human`
- 同步任务在完成或失败时写回终态，其中成功终态统一为 `closed`
- 请求若来自已有 GitHub / GitLab issue，应在 `task_created` 后直接写 `issue_created`
- 异步代码任务若来自飞书 / Web 等非 issue 入口，则先写 `task_created`，再写 `issue_sync_pending`，镜像建立成功后再写 `issue_created`
- 不允许出现“issue 已创建，但 BUS 中没有正式任务状态”的情况
- 所有外部副作用都先登记到 `outbox`，执行成功或失败后都要回写 BUS
- 任何经由 MCP 产生副作用的请求都必须显式携带幂等键，并把该键作为 MCP 协议契约字段透传到底层执行器

对入口阶段尤其要强调两条额外规则：

- `ReceptionAgent` 给出的是建议性分流，真正是否放行副作用由 BUS 策略层裁决
- 高风险副作用不能只靠自然语言“像是用户本人”就执行，必须经过身份校验，并在需要时等待一次性 `confirmation_token`
- 人类确认按钮或 Web 审批动作必须做幂等去重，并绑定鉴权快照，防止双击或过期操作重复生效

其中身份校验在概念上至少支持三类可信来源：

- 飞书用户ID
- Web 会话
- 仓库平台账号映射

哪些操作需要审批、由谁审批以及确认超时时间，属于 BUS 策略配置的一部分，而不是 `ReceptionAgent` 的 Prompt 内规则。

人工确认和驳回在交互表面上，第一版优先通过飞书卡片加按钮完成，Web 管理页作为补充后台；两者本质上都只是把确认结果回写 BUS。

## 10. 异常场景与默认策略

| 异常场景 | 默认系统行为 |
| --- | --- |
| 重复消息 | 不重复创建 BUS 任务，不重复创建 issue；优先复用既有 `task_id` 并返回已有状态或已有结果 |
| 无法解析或无法稳定分类 | 不做副作用操作；优先进入 `waiting_human` 请求补充；若存在明显代码信号，则按 `async_issue` 接收 |
| 同步执行中发现复杂度超出边界 | 不把入口层拖成长任务；非代码任务返回要求缩小范围，明确代码任务可升级为 `async_issue` |
| 身份校验失败或策略拒绝 | 不执行副作用，保持在 `waiting_human` 或直接转 `failed`，并给出明确拒绝原因 |
| 确认令牌超时或人类取消 | 从 `waiting_human` 进入 `cancelled`，不继续执行任何副作用 |
| `Alice Control MCP` 执行失败 | 直接回写 `failed`，不自动切换到其他路径 |
| BUS 建任务失败 | 不创建 issue，也不宣称“已受理”；直接回写 `failed` |
| issue 创建失败 | BUS 任务已经存在时，先保持在 `issue_sync_pending`，记录补建动作并告警；采用指数退避补建，403/404 等致命错误立即转 `failed`，不能伪装为成功交接 |
| BUS 回写失败 | 优先重试回写，因为 BUS 是状态真源；不能仅凭外部 side effect 当作成功 |
| 外部副作用已发出但 BUS 未写回 | 依赖 `outbox` 记录恢复动作；补写状态并及时告警，不能把任务留在未知状态 |
| 入口流量积压触发背压 | 暂停低优先级同步请求和 issue 镜像补建，返回可重试信号并持续告警，高优先级确认与取消请求优先保留 |
| 同一会话短时间内连续发来同类修改请求 | 优先按 `coalescing_key` 合并或覆盖，避免重复建任务、重复补建 issue 或重复发起确认 |
| 长时间阻塞或遇到无法预料的问题 | 不要求入口层无限卡住；形成失败摘要或阶段性报告，并触发对人类的通知 |

这些策略背后的共同原则是：

- 不重复制造副作用
- 不让外部协作界面反客为主
- 不让用户看见静默失败

对于入口阶段的严重异常，还应遵循两条补充原则：

- 可短暂重试的瞬时错误，例如网络抖动或短时 MCP 不可用，优先在当前阶段内有限重试
- 超过入口可容忍时间窗口后，及时结束本次尝试，并通过回复链路或告警链路通知人类，而不是让请求无声悬挂

## 11. 方案取舍

### 11.1 为什么 BUS 而不是 Issue 作为真实状态

- BUS 是系统内部统一状态通道，适合承载所有来源和所有阶段的真实进度。
- Issue、PR、评论只覆盖代码协作场景，不覆盖全部任务类型。
- 先把状态建在 BUS 里，才能保证入口、审核、编码、通知等模块共享同一事实基础。

### 11.2 为什么入口只做分流，不直接进入完整代码执行

- 入口链路的目标是快速、稳定接待请求，而不是承载长生命周期代码流程。
- 代码任务需要计划、审核、编码、合并等多阶段控制，天然更适合在异步工作流里推进。
- 把代码执行留给后续工作流，入口层才能保持简单和可预测。

### 11.3 为什么 issue 是“协作镜像”，不是“状态真源”

- issue 很适合承载人类讨论、计划说明和审核意见。
- issue 也是很自然的外部事件来源，适合触发后续 Agent。
- 但 issue 平台并不适合作为系统唯一真实状态源，否则会把内部流程绑死在外部协作工具上。

## 12. 与下游文档的关系

本文件只定义入口阶段，止步于 `issue_created` 这个交接点。

从 `issue_created` 之后开始的：

- PlannerAgent 读取 issue 并形成计划
- PlanAuditAgent 审核计划
- CodingAgent 编码并创建 PR
- CodeAuditAgent 审核 PR
- 合并、关闭 issue、通知人类

这些内容在新文档 [issue_workflow_detail.md](./issue_workflow_detail.md) 中展开。

## 13. 结论

基于最新 `draft.md`，入口处理可以收敛为一句话：

`ReceptionAgent` 负责把多来源用户请求收敛成 BUS 内真实任务，并给出带风险分级的分流建议；真正的副作用放行由 BUS 策略层裁决。同步请求在受控沙箱中收口，信息不足或高风险请求先进入 `waiting_human`；来自现成 issue 的异步代码任务经 `task_created -> issue_created` 交接，下游缺少外部载体的代码任务才经 `task_created -> issue_sync_pending -> issue_created` 交接给后续工作流。
