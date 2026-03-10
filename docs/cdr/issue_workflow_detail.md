# Issue 驱动代码工作流概念设计

本文基于 `draft.md` 中以下新增内容展开：

- `代码Issue触发`
- `审核需求触发`
- `Coding Agent`
- `Evaluation Agent`
- 总状态机与架构图

本文只做概念设计细化，不展开到接口、Prompt、存储结构和调度实现。

## 1. 目标与范围

这份文档的目标，是把“代码任务从 issue 开始，经过计划、审核、编码、可选评测、代码审核，最后合并关闭或产出报告”的链路整理成一套可评审的概念模型。

它重点回答八个问题：

1. 在这条链路里，BUS、Issue、PR、评论分别是什么角色。
2. `PlannerAgent`、`PlanAuditAgent`、`CodingAgent`、`EvaluationAgent`、`CodeAuditAgent` 的职责边界是什么。
3. 从 issue 到 merge 或报告输出的最小生命周期状态是什么。
4. 人类评论、自动评论、审核意见、预算决策如何推进、打回或挂起流程。
5. 多个审核结论不一致时如何先按不通过处理，并在连续 3 轮后升级人工仲裁。
6. 需要实验验证的任务怎样在 `coding -> evaluation -> coding` 回路中推进。
7. 预算硬熔断、评测恢复和取消传播怎样影响状态机。
8. 出现重复事件、失败事件、外部状态不一致时系统遵循什么默认原则。

## 2. 全局设计前提

### 2.1 BUS 是真实状态

- BUS 中的任务状态是系统真实状态
- JSONL 追加日志和周期性快照负责持久化这些状态变化
- GitHub、GitLab 上的 Issue、PR、评论是协作界面和外部事件来源
- `task_version` 用于单任务乐观锁和并发写保护，`global_hlc` 用于跨任务因果追踪与后续分布式扩展

因此，这条工作流里的所有关键阶段推进，最终都必须体现为 BUS 状态变化，而不是只体现在外部平台上。

### 2.2 事件一致性与外部副作用

- 所有外部事件都按“至少一次投递”处理
- Webhook、评论、Review 必须带稳定外部事件ID，先验签，再做幂等去重
- 所有回流事件都应保留 `origin_actor`、`causation_id` 和关联外部对象引用，用于判定是否属于 Alice 自己触发的回执
- 去重记录、`outbox` 和外部对象关联关系都必须持久化
- 所有外部副作用都先写入本地持久化 `outbox`，再调用 MCP，完成后再回写 BUS
- 幂等键必须成为 MCP 协议契约的一部分，并通过 gRPC metadata、HTTP header 或等价显式字段传递
- Alice 自己发出的 issue 评论、Review、PR 状态变更和合并请求，其回流事件只用于确认回执与同步镜像状态，不再次触发同类 Agent 开工

这意味着 issue 工作流不能假设“外部平台只会发一次事件”或“调用 MCP 成功后 BUS 一定已经同步写回”。

### 2.3 版本绑定与单任务串行

- 同一 `task_id` 的事件按顺序串行处理，不允许多个 Agent 并发改同一个任务状态
- 所有状态推进都要检查当前状态、目标版本和对象版本号
- 计划审核必须绑定明确的 `plan_version`
- 代码审核必须绑定明确的 `pr_head_sha`
- BUS 需要分别维护计划审核和代码审核的轮次计数
- 版本一旦变化，旧审核结论自动失效，必须重新审核
- 状态回退、取消或预算硬熔断时，活动 `CancellationToken` 必须传播到 Agent 与 MCP 层
- 审核意见不一致时，默认按“不通过”打回当前阶段并累积轮次；连续 3 轮仍不一致，再升级到 `pending_arbitration`

这意味着“计划通过”或“代码通过”不是抽象结论，而是对某个明确版本的结论。

### 2.4 所有 Agent 都在受限边界内工作

- 所有涉及仓库、凭据、系统设置的 Agent 都运行在受限沙箱中
- 对仓库、Git 平台、计算集群和 Alice 控制能力的访问统一走 MCP
- 同机通信优先 Unix Domain Socket，跨机通信优先 gRPC
- `Evaluation` 默认网络隔离，只允许访问白名单制品、模型和数据源；进入评测前要经过基础静态扫描与策略校验

因此，这些 Agent 都不是“直接拿外部权限做事”，而是“在受控环境中，经由 MCP 执行动作”。

### 2.5 评测与预算约束

- 只有策略层标记 `requires_evaluation` 的任务，才进入 `evaluation`
- `EvalSpec` 在计划阶段生成，并绑定数据集版本、评测配置、通过阈值、预算上限和资源规格
- `UsageLedger` 负责持续累计 token、模型费用和 GPU/CPU 用量，并作为预算决策和硬熔断依据
- 当预算剩余小于 0 或命中策略硬上限时，BUS 必须停止新的 `coding` / `evaluation` 投递，并尝试终止已运行的评测作业

因此，`evaluation` 不是“代码审核的一部分”，而是可选的客观执行阶段；预算也不是展示字段，而是状态推进约束。

### 2.6 issue / PR / 评论的角色

- issue 是代码任务的外部协作载体
- issue 评论是计划讨论和任务澄清界面
- PR 是代码变更与代码审核的载体
- PR 评论、Review 和状态变更是代码审核阶段的外部事件来源

也就是说，外部平台不是被动展示板，而是工作流的协作表面和事件入口。

## 3. 上下文图

```text
[Human / Auto Process]
          |
          | issue / comment / review / webhook
          v
 [GitHub / GitLab Issue & PR]
          |
          | external event
          v
 [BUS / State Store / Outbox / Notifier]
    +------+------+-------+-----------+-----------+--------+--------+
    |      |      |       |           |           |        |        |
    v      v      v       v           v           v        v        v
[Planner][PlanAudit][Coding][Evaluation][CodeAudit][Store][Notifier]
    |        |       |         |           |
    +--------+-------+---------+-----------+
                      |
                      v
                    [MCP]
        +-----------+--------+--------+
        |                    |        |
        v                    v        v
   [GitHub MCP]        [GitLab MCP] [Cluster MCP]
        |                    |        |
        +----------+---------+--------+
                   v
            [GitHub / GitLab / Cluster]
```

这张图表达四个核心点：

- 所有工作流推进都经过 BUS，而不是 Agent 之间直接串联。
- Planner、审核、编码、评测、代码审核是并列消费 BUS 事件的角色，不是彼此私下调用的黑盒链条。
- 外部 Git 平台同时承担“展示协作界面”和“提供事件回流”的双重作用。
- `Store`、`outbox` 和 `Notifier` 也是工作流的一部分，而不是实现细节附件。

## 4. 角色边界

### 4.1 PlannerAgent

`PlannerAgent` 负责：

- 读取 issue、issue 回复和必要代码上下文
- 形成带 `plan_artifact_id` 和 `plan_version` 的可执行计划
- 通过 MCP 把计划回复到 issue
- 对指定 `plan_version` 请求计划审核

它不负责：

- 直接写最终代码
- 自己批准自己的计划
- 直接合并 PR

### 4.2 PlanAuditAgent

`PlanAuditAgent` 负责：

- 对指定 `plan_version` 是否完整、合理、可执行进行独立审核
- 给出“通过”或“打回”的结论
- 在需要时提出修改意见

它不负责：

- 重写整份计划并替代 Planner 的职责
- 直接进入编码阶段
- 在审核冲突时直接覆盖人类仲裁

### 4.3 CodingAgent

`CodingAgent` 负责：

- 根据已通过审核的 `plan_artifact_id` 和 `plan_version` 执行编码
- 在需要时并行完成子任务
- 保证同一 `task_id` 同时只有一个活动 Coding 轮次
- 创建新的 PR
- 以明确的 `pr_head_sha` 请求代码审核

它不负责：

- 绕过计划直接开工
- 自己完成最终代码审核
- 在未获通过前直接合并

### 4.4 EvaluationAgent

`EvaluationAgent` 负责：

- 对指定 `pr_head_sha` 执行与 `EvalSpec` 绑定的评测
- 通过 Cluster MCP 申请、跟踪并在必要时终止作业
- 回写结构化 `EvalResult`、资源消耗和通过/打回结论

它不负责：

- 自己修改代码替代 `CodingAgent`
- 绕过预算、资源和数据集版本约束
- 在代码未变化时擅自扩大评测范围

### 4.5 CodeAuditAgent

`CodeAuditAgent` 负责：

- 独立审核指定 `pr_head_sha` 是否符合计划
- 审核 CI、测试和分支保护是否满足可合并条件
- 提出修改意见，或者给出通过结论

它不负责：

- 直接重写全部代码并替代 CodingAgent
- 在审核冲突时自行决定覆盖其他审核结论

## 5. 最小概念对象

为了让组件契约可评审，这条工作流至少需要以下概念对象。

| 对象 | 含义 | 由谁产生 | 由谁消费 |
| --- | --- | --- | --- |
| `Task` | BUS 中被真实管理的代码任务，承载 `task_id`、对象版本、关联 issue/PR、当前状态，以及计划审核和代码审核的轮次计数 | BUS `State Store` | 全部后续 Agent |
| `IssueEvent` | 来自 GitHub / GitLab issue、评论、Review 或 PR 状态变化的外部事件，带稳定 `event_id`、`origin_actor`、`causation_id`、验签结果和外部对象引用 | Webhook 入口 | BUS、PlannerAgent、审核 Agent |
| `PlanArtifact` | Planner 形成的任务计划，至少包含 `plan_artifact_id` 和 `plan_version` | PlannerAgent | PlanAuditAgent、Human、CodingAgent |
| `AuditRequest` | 请求某一类审核的控制对象，必须绑定目标类型、目标版本、审核轮次、预期审核 Agent 集合、截止时间和心跳租约 | PlannerAgent、CodingAgent | 审核 Agent |
| `AuditVerdict` | 审核结论，至少包含目标版本、审核轮次、通过/打回结论及意见摘要 | PlanAuditAgent、CodeAuditAgent | BUS、上游执行 Agent |
| `CodingRequest` | 经计划审核通过后发出的正式编码请求，绑定 `plan_artifact_id` 和 `plan_version` | BUS 或流程编排层 | CodingAgent |
| `EvalSpec` | 评测规格，绑定数据集/配置版本、通过阈值、预算上限、资源规格和随机种子策略 | PlannerAgent | EvaluationAgent、BUS |
| `EvalResult` | 针对指定 `pr_head_sha` 的结构化评测结果，至少包含指标摘要、基线对比、产物引用和结论 | EvaluationAgent | BUS、CodeAuditAgent、Human |
| `PRArtifact` | 代码修改产物及其关联 PR，至少包含 `pr_head_sha` 和可合并性信息 | CodingAgent | CodeAuditAgent、Human |
| `ArbitrationRequest` | 人工仲裁请求对象，必须绑定仲裁目标类型、目标版本、触发轮次、触发原因和失效规则 | 审核聚合层 / BUS | Human、BUS |
| `Confirmation` | 用于高风险合并、预算追加或恢复动作的一次性确认对象，带 `confirmation_token` 和鉴权快照 | BUS 策略层 | Human、BUS |
| `CancellationToken` | 用于停止活动 LLM 请求、PR 更新或集群作业的取消凭证 | BUS | CodingAgent、EvaluationAgent、MCP |
| `UsageLedger` | 任务级和阶段级的 token、模型费用与资源消耗账本，用于预算展示和硬熔断 | BUS、EvaluationAgent、MCP | BUS、展示区、告警系统 |
| `OpsReadModel` | 面向 Web / 飞书展示区的只读投影，汇总任务状态、预算、队列积压和 MCP 健康 | BUS | Human、展示区、通知链路 |
| `OutboxAction` | 待执行或待恢复的外部副作用，例如评论、建 PR、合并 PR、关闭 issue | BUS `State Store` / `outbox` | MCP 执行器、恢复流程 |
| `StatusUpdate` | 生命周期中的统一状态推进事件 | 全部 Agent | BUS、通知链路、观测系统 |

## 6. 生命周期总览

`draft.md` 已给出总状态机，这里把它翻译成概念语义。为避免实现歧义，本文做三处细化：

- `draft.md` 中的 `NewTask` 统一细化为 `task_created`
- `draft.md` 中“代码审核通过并合并”统一拆成 `merge_approval_pending`、`merging` 和 `merged`，用来区分“等待合并审批”“实际合并执行中”和“实际合并已成功”
- `draft.md` 中的 `WaitingHuman` 在本文中被解释为一类共享挂起状态，可承载高风险确认、信息补充、预算追加和恢复处理；连续 3 轮审核仍不一致后的人工仲裁单独进入 `pending_arbitration`

| 状态 | 含义 | 主要推进者 | 下一步 |
| --- | --- | --- | --- |
| `task_created` | BUS 中新建了一个代码任务 | ReceptionAgent / issue 入口桥接层 | `issue_created` 或 `issue_sync_pending` |
| `issue_sync_pending` | 任务已建立，但当前来源不是现成 issue，因此系统仍在创建或补建外部 issue 镜像 | ReceptionAgent / Git 平台桥接层 | `issue_created`、`failed` |
| `issue_created` | 任务已绑定外部 issue 载体；要么直接绑定已有外部 issue，要么镜像已建立成功 | ReceptionAgent / Git 平台桥接层 | `planning` |
| `planning` | Planner 正在生成或更新执行计划 | PlannerAgent | `plan_audit` |
| `plan_audit` | 计划进入针对特定 `plan_version` 的审核 | PlanAuditAgent | `planning`、`coding` 或 `pending_arbitration` |
| `waiting_human` | 等待人类补充说明、一次性确认令牌、预算决策或恢复指令，并携带 `waiting_reason` | 人类确认链路 / BUS 策略层 | `issue_sync_pending`、`planning`、`coding`、`evaluation`、`merge_approval_pending` 或 `cancelled` |
| `pending_arbitration` | 连续 3 轮审核仍不一致，等待与当前目标版本绑定的人工仲裁 | 人类仲裁链路 / BUS 策略层 | `planning`、`coding`、`code_audit` 或 `cancelled` |
| `coding` | CodingAgent 正在实现已批准计划 | CodingAgent | `evaluation`、`code_audit` 或 `failed` |
| `evaluation` | EvaluationAgent 正在对指定 `pr_head_sha` 执行评测 | EvaluationAgent | `coding`、`code_audit`、`budget_exceeded`、`waiting_human` 或 `failed` |
| `budget_exceeded` | 任务已命中预算或资源硬上限，自动推进暂停 | BUS / UsageLedger / EvaluationAgent | `waiting_human` |
| `code_audit` | PR 正在接受针对特定 `pr_head_sha` 的代码审核 | CodeAuditAgent | `coding`、`pending_arbitration`、`merge_approval_pending`、`merging` 或 `closed` |
| `merge_approval_pending` | 代码审核已通过，等待合并审批 | 人类确认链路 / BUS 策略层 | `merging`、`coding` 或 `cancelled` |
| `merging` | 系统正在执行实际合并与 issue 收口动作 | 系统流程 / Git 平台桥接层 | `merged`、`waiting_human` 或 `failed` |
| `merged` | 代码已成功合并，但 issue 关闭、通知等收尾动作尚未全部完成 | 系统流程 / Git 平台桥接层 | `closed` |
| `closed` | issue 与任务生命周期结束 | 系统流程 | 终态 |
| `failed` | 当前阶段终止，任务生命周期在本轮设计中视为结束 | 当前执行主体 | 终态 |
| `cancelled` | 任务被取消，不再继续推进 | 人类或控制侧 | 终态 |

## 7. 生命周期流转图

```text
   +------------------+
   |   task_created   |
   +-----+------+-----+
         |      |
         |      v
         |  +--------------------+
         |  | issue_sync_pending |
         |  +--------+-----------+
         |           |
         v           v
   +---------------+ +---------------+
   | issue_created | | issue_created |
   +-------+-------+ +-------+-------+
           |
           v
    +-----------+
    | planning  |
    +-----+-----+
          |
          v
    +-----------+
    |plan_audit |
    +--+-----+--+
       |     |
 reject|     |pass
 or    |     |
 round<3     |
       v     v
 +---------+  +--------+
 |planning |  | coding |
 +---------+  +---+----+
                  |
          requires_eval? \
                yes      \ no
                  |        \
                  v         v
           +-------------+ +-----------+
           | evaluation  | |code_audit |
           +--+----+--+--+ +--+-----+--+
              |    |  |       |     |
     fail/head|    |  |pass   |pass |
   change/no  |    |  +------>+     |
    progress  |    |               reject/head
              |    |               change or
              v    v               round<3
          +------+ +---------------+
          |coding| |budget_exceeded|
          +------+ +-------+-------+
                              |
                              v
                        +-------------+
                        |waiting_human|
                        +--+---+---+--+
                           |   |   |
                           |   |   +--> evaluation
                           |   +------> coding / planning
                           +----------> cancelled

          +------------------------+
          | merge_approval_pending |
          +-----------+------------+
                      |
                      v
                 +---------+
                 | merging |
                 +----+----+
                      |
                      v
                 +--------+
                 | merged |
                 +---+----+
                     |
                     v
                  +------+
                  |closed|
                  +------+

    +-----------+                     +--------------------+
    |plan_audit |---- round=3 ----->  |pending_arbitration |
    +-----------+                     +---------+----------+
                                               ^
                                               |
    +-----------+---- round=3 -----------------+
    |code_audit |
    +-----------+
                                               |
                                               v
                            human arbitration / confirmation / resume routing
```

这张图强调四类回路：

- 已有外部 issue 直接进入 `issue_created`；只有缺少外部载体时才经过 `issue_sync_pending`
- 计划审核打回，回到 `planning`
- 代码审核或评测打回，回到 `coding`
- 预算触顶后，先进入 `budget_exceeded -> waiting_human`，再由人类决定恢复到 `evaluation`、`coding` 或 `planning`
- 审核冲突在前 2 轮默认按不通过处理并回到上一步；连续 3 轮仍不一致时，进入 `pending_arbitration`

## 8. 关键阶段说明

### 8.1 issue 触发阶段

触发来源可以有两类：

- 人类新建 issue
- 人类或自动过程对 issue 的回复

对系统来说，这些都先表现为 `IssueEvent`，进入 BUS 后再由对应 Agent 消费。

这一步的核心不是“看到 issue 就执行”，而是：

- 把 issue 和内部 `Task` 建立关联；如果请求本身来自现成 GitHub / GitLab issue，应直接绑定该 issue 并进入 `issue_created`，而不是再走 `issue_sync_pending`
- 判断这是新任务、补充说明、审核反馈，还是状态推进事件
- 对 webhook 做验签、去重，并把 issue、评论、PR、Review 全部关联回同一个 `task_id`
- 识别 `origin_actor` 和 `causation_id`；若事件是 Alice 自己发出的评论、Review 或状态变更回执，则只更新回执与镜像状态，不再次触发 Planner、审核或 Coding

### 8.2 计划阶段

`PlannerAgent` 读取 issue、回复和相关代码上下文，输出一份带 `plan_artifact_id` 与 `plan_version` 的计划并回复到 issue。

计划阶段的目标不是开始编码，而是把“模糊需求”转成“可审核、可执行、可追踪”的计划对象。

计划一旦形成，就应在 BUS 中推进到 `plan_audit`，并把“审核目标版本”固定为当前 `plan_version`。

### 8.3 计划审核阶段

`PlanAuditAgent` 对计划进行独立审核。

在进入“通过 / 打回 / 冲突处理”之前，系统还必须先完成一轮审核聚合。

#### 8.3.1 一轮审核的完成条件

一轮计划审核在概念上由一个 `AuditRequest` 固定以下内容：

- 审核目标：本轮唯一允许的 `plan_version`
- 审核轮次：`plan_audit_round`
- 审核 Agent 集合：由策略层预先决定，本轮内不再动态增减
- 截止时间：可按任务类型、风险级别等配置给出默认值，例如 24 小时，也可由发起审核的 Agent 在策略允许范围内动态指定
- 心跳租约：审核 Agent 接单后必须先回写 `accepted`，随后定期续租

这一轮的聚合规则是：

- 首个 `AuditVerdict` 到达时，只记录，不立即推进主状态机
- 在收齐该轮预期的全部 verdict 之前，任务保持在 `plan_audit`
- 当收齐全部 verdict，或所有未完成席位都因心跳租约失效 / 截止时间到达而被判定为缺席后，审核聚合层再统一判定“通过 / 打回 / 结论冲突”
- 迟到 verdict、旧轮次 verdict、旧 `plan_version` verdict 只保留审计，不再改变主状态

因此，“一轮计划审核”的最终结果只和该轮固定的 verdict 集合有关，不应因到达顺序不同而改变。

根据 `draft.md`，这里允许接入多个审核 Agent，而审核结论不一致时，默认先按“不通过”处理并打回 Planner 重做，同时累积计划审核轮次；只有连续 3 轮仍然不一致时，才升级到 `pending_arbitration` 做人工仲裁。

因此，这一阶段的原则是：

- 审核通过，才能进入 `coding`
- 任一明确打回，返回 `planning`
- 多审核不一致时，前 2 轮按“不通过”处理并返回 `planning`
- 连续 3 轮仍不一致，进入 `pending_arbitration`
- 审核缺席按聚合策略参与本轮结论；第一版默认保守，不把缺席视为自动通过
- 人类仲裁结论优先；必要时可以追加第三审核 Agent，但不能绕过版本绑定和轮次计数

这种设计偏保守，但更稳。

### 8.4 编码阶段

`CodingAgent` 只消费已经通过计划审核的任务。

它的职责是：

- 基于通过计划进行实现
- 在需要时并行拆分编码工作
- 保证同一 `task_id` 同时只有一个活动 Coding 轮次
- 创建或更新唯一活动 PR
- 根据策略决定是直接请求代码审核，还是先进入 `evaluation`

一旦 PR 建立，BUS 必须立刻推进到“下一个明确阶段”，而不是只在 Git 平台上显示“已开 PR”：

- `requires_evaluation = false` 时，直接进入 `code_audit`
- `requires_evaluation = true` 时，直接进入 `evaluation`

如果编码过程中需要自动同步主干、rebase 或补提交，导致 `pr_head_sha` 变化，则旧审核目标自动失效，必须重新请求代码审核或重新评测。

如果更新 PR 时发现远端分支已经出现非快进变化，则系统只允许：

- 自动 rebase 并在无冲突时继续
- 发生冲突时回退到 `coding` 解决

第一版不允许通过 force push 覆盖远端审核历史。

此外，状态回退、人工取消或预算硬熔断触发时，BUS 必须撤销活动 `CancellationToken`，通知编码侧停止仍在运行的 LLM 请求、PR 更新和相关 MCP 调用。

### 8.5 评测阶段

`EvaluationAgent` 只在 `requires_evaluation = true` 的任务上工作。

它消费的是“当前 `pr_head_sha` + 当前 `EvalSpec`”这一对明确目标，而不是模糊的“帮我跑一下实验”。

这一阶段至少应满足：

- `EvalSpec` 在计划阶段已经固定通过阈值、预算上限、资源规格、镜像摘要、数据集版本和随机种子策略
- `EvaluationAgent` 通过 Cluster MCP 提交、轮询和在必要时终止作业
- `EvalResult` 必须和当前 `task_id`、`pr_head_sha`、`eval_spec_id` 绑定回写
- 旧 `pr_head_sha`、旧数据集版本或旧评测配置的结果只保留审计，不得推进当前主状态

评测阶段的结果分为四类：

- 指标通过：进入 `code_audit`
- 指标不通过、结果劣化或配置变化：回到 `coding`
- 预算或资源命中硬上限：进入 `budget_exceeded`
- 长时间无进展、资源不足或方向需要人工裁决：进入 `waiting_human`

如果人类只是追加预算、恢复资源或允许继续当前评测，而代码和 `EvalSpec` 没有变化，任务可以从 `waiting_human` 直接恢复到 `evaluation`，而不必强制回到 `coding`。

### 8.6 代码审核阶段

`CodeAuditAgent` 独立审核 PR。

代码审核沿用同样的一轮聚合语义，只是目标版本从 `plan_version` 换成 `pr_head_sha`，轮次从 `plan_audit_round` 换成 `code_audit_round`。

对代码审核而言，一轮 `AuditRequest` 必须固定：

- 审核目标：本轮唯一允许的 `pr_head_sha`
- 审核轮次：`code_audit_round`
- 审核 Agent 集合：由策略层预先决定
- 截止时间：可按任务类型、风险级别等配置给出默认值，例如 24 小时，也可由发起审核的 Agent 在策略允许范围内动态指定
- 心跳租约：审核 Agent 接单后必须先回写 `accepted`，随后定期续租

在收齐该轮预期 verdict 或发生超时前：

- 首个 approve 或 reject 都只记录，不立即推进状态
- 任务保持在 `code_audit`
- 最终聚合结论由审核聚合层统一给出；缺席席位在租约失效或截止时间到达后按缺席处理

这样才能避免“同一组审核结果，仅因到达顺序不同就走到不同状态”的竞态问题。

根据 `draft.md`，这里也允许多个独立 CodeAuditAgent 审核，并且结论不一致时默认先按“不通过”处理，回到 `coding`；如果连续 3 轮代码审核仍然意见不一致，再升级到 `pending_arbitration`。

因此这一阶段遵循：

- 审核通过，任务进入 `merge_approval_pending` 或 `merging`
- 任一明确打回，返回 `coding`
- 多审核不一致时，前 2 轮按“不通过”处理并返回 `coding`
- 连续 3 轮仍不一致，进入 `pending_arbitration`
- 审核对象始终是明确的 `pr_head_sha`
- 只有当 CI、测试和分支保护都满足时，才允许继续合并
- 审核意见应保留结构化理由摘要、证据引用和模型/策略元数据，便于人类复盘；不要求保留模型内部 chain-of-thought

这里要特别区分两类条件：

- 审核聚合是否已经通过
- 合并门禁，例如 CI、分支保护、可合并性是否已经满足

如果审核 verdict 已经聚合为通过，但 CI / 分支保护尚未满足，任务应继续停留在 `code_audit`，并记录“等待门禁满足”的子原因；当后续 webhook 或轮询发现门禁满足后，再由 BUS 触发推进到 `merge_approval_pending` 或 `merging`。

### 8.7 合并与关闭阶段

当代码审核通过后，系统先进入 `merge_approval_pending` 或 `merging`，再执行：

- 提交合并请求
- 执行合并
- 关闭 issue
- 通知人类

在这个阶段里：

- PR 实际合并成功后，BUS 才能进入 `merged`
- issue 关闭、通知和收尾动作完成后，任务再进入 `closed`
- 如果策略层把合并视为高风险副作用，则应先进入 `merge_approval_pending`，待确认令牌回流后再恢复到 `merging`
- 飞书卡片或 Web 审批动作必须携带 `confirmation_token` 或等价幂等键；重复点击只能返回“已处理/已失效”，不能重复触发合并

如果任务策略明确为“只产出报告、不合入主干”，则代码审核或评测收口后可以直接生成阶段报告并进入 `closed`，不强制进入合并分支。

这里的人工确认身份来源，与入口阶段保持同一套信任基础：飞书用户ID、Web 会话或仓库平台账号映射。

这里要特别注意，外部 issue 关闭和 PR 合并只是外部动作；真正的流程完成，仍应以 BUS 中进入 `merged` 和 `closed` 为准。

### 8.8 连续冲突后的人工仲裁阶段

`pending_arbitration` 在这里承担的是“连续 3 轮审核仍无法达成一致”的人工仲裁挂起态，而不是每次审核冲突都直接进入的中间态。

进入该状态时，BUS 必须先生成一个版本绑定的 `ArbitrationRequest`，至少固定：

- 仲裁目标类型：计划仲裁或代码仲裁
- 目标版本：`plan_version` 或 `pr_head_sha`
- 触发轮次
- 触发原因与差异摘要
- 失效规则：一旦目标版本被 supersede，旧仲裁请求立即失效

因此，人类仲裁结论不是对“这个任务大概怎么办”的抽象意见，而是对某个明确版本的裁决。

这一阶段至少要做到：

- 汇总本阶段前 3 轮的审核目标、版本和差异摘要
- 通过通知链路主动触达人类
- 优先通过飞书卡片加按钮完成批准、驳回或仲裁，Web 管理页作为补充后台
- 只接受绑定当前 `ArbitrationRequest` 的人类仲裁结论，并把结果回写 BUS
- 在需要时允许追加第三审核 Agent，但最终仍由人类结论兜底

如果仲裁期间目标版本发生变化，例如 Planner 重新产出新 `plan_version`，或 Coding 产生新 `pr_head_sha`，则当前 `ArbitrationRequest` 必须标记为 `superseded`；旧卡片、旧按钮和旧结论只保留审计，不再推进主状态。

## 9. 事件与状态推进矩阵

| 外部事件 / 内部事件 | 当前状态 | 处理主体 | 结果 |
| --- | --- | --- | --- |
| 新 issue 创建 | 无任务 | 入口桥接层 / BUS | 创建 `Task`，直接绑定该 issue 并推进到 `issue_created` |
| 非 issue 入口创建的代码任务 | `task_created` | 入口桥接层 / BUS | 因缺少外部 issue 载体，推进到 `issue_sync_pending` 等待镜像创建 |
| issue 镜像创建成功 | `issue_sync_pending` | ReceptionAgent / Git 平台桥接层 | 推进到 `issue_created` |
| issue 镜像创建返回 403/404 等致命错误 | `issue_sync_pending` | Git 平台桥接层 / BUS | 结束补建并推进到 `failed` |
| issue 新回复，属于需求补充 | `issue_created` / `planning` | PlannerAgent | 更新计划上下文，保持或回到 `planning` |
| Alice 自己发出的 issue 评论 / Review / PR 状态变更回流 | 任意 | BUS / Git 平台桥接层 | 只确认回执、同步镜像或更新外部对象引用，不再次触发同类 Agent |
| 计划已发布并请求审核 | `planning` | PlannerAgent | 固定当前 `plan_version`，推进到 `plan_audit` |
| 计划版本变化 | `plan_audit` | PlannerAgent / BUS | 旧审核目标失效，回到 `planning` |
| 单个计划审核 verdict 到达 | `plan_audit` | PlanAuditAgent | 只记录本轮 verdict，不立即推进主状态 |
| 当前轮计划审核 verdict 已收齐且聚合结果为打回 | `plan_audit` | 审核聚合层 | 回到 `planning` |
| 当前轮计划审核 verdict 已收齐且结论冲突，且轮次 < 3 | `plan_audit` | 审核聚合层 | 默认按“不通过”处理，累积 `plan_audit_round`，回到 `planning` |
| 当前轮计划审核 verdict 已收齐且结论冲突，且轮次 = 3 | `plan_audit` | 审核聚合层 / Notifier | 创建绑定当前 `plan_version` 的 `ArbitrationRequest`，推进到 `pending_arbitration`，并通知人类 |
| 计划审核席位租约失效或截止时间到达 | `plan_audit` | 审核聚合层 | 将未完成席位按缺席纳入聚合，并产出本轮最终结论 |
| 人类仲裁计划审核 | `pending_arbitration` | Human / BUS | 仅消费当前 `ArbitrationRequest`；根据仲裁结果回到 `planning` 或进入 `coding` |
| 计划仲裁目标版本在等待期间被 supersede | `pending_arbitration` | BUS | 当前 `ArbitrationRequest` 失效，旧结论只保留审计，任务回到 `planning` |
| 当前轮计划审核 verdict 已收齐且聚合结果为通过 | `plan_audit` | 审核聚合层 | 生成绑定 `plan_artifact_id` 和 `plan_version` 的 `CodingRequest`，推进到 `coding` |
| PR 创建成功且无需评测 | `coding` | CodingAgent | 推进到 `code_audit` |
| PR 创建成功且需要评测 | `coding` | CodingAgent / BUS | 推进到 `evaluation` |
| PR 更新遇到远端非快进变化且自动 rebase 成功 | `coding` | CodingAgent | 继续 `coding` 或以新 `pr_head_sha` 进入后续阶段 |
| PR 更新遇到远端非快进变化且 rebase 冲突 | `coding` | CodingAgent | 保持或回退到 `coding`，等待解决冲突 |
| 当前评测完成且指标通过 | `evaluation` | EvaluationAgent | 推进到 `code_audit` |
| 当前评测完成且指标不通过或配置变化 | `evaluation` | EvaluationAgent | 回到 `coding` |
| 评测命中预算或资源硬上限 | `evaluation` | EvaluationAgent / UsageLedger / BUS | 推进到 `budget_exceeded` |
| 评测长时间无进展或资源不足 | `evaluation` | EvaluationAgent / BUS | 推进到 `waiting_human` |
| 人类在预算决策后允许继续当前评测 | `waiting_human` | Human / BUS | 若代码和 `EvalSpec` 未变，直接恢复到 `evaluation` |
| 人类在预算决策后要求调整方向或改代码 | `waiting_human` | Human / BUS | 回到 `planning` 或 `coding` |
| `budget_exceeded` 收到追加预算或恢复资源确认 | `waiting_human` | Human / BUS | 直接恢复到 `evaluation` 或回到 `coding` |
| `pr_head_sha` 变化 | `code_audit` | CodingAgent / BUS | 当前审核目标失效，回到 `coding` |
| 单个代码审核 verdict 到达 | `code_audit` | CodeAuditAgent | 只记录本轮 verdict，不立即推进主状态 |
| 当前轮代码审核 verdict 已收齐且聚合结果为打回 | `code_audit` | 审核聚合层 | 回到 `coding` |
| 当前轮代码审核 verdict 已收齐且结论冲突，且轮次 < 3 | `code_audit` | 审核聚合层 | 默认按“不通过”处理，累积 `code_audit_round`，回到 `coding` |
| 当前轮代码审核 verdict 已收齐且结论冲突，且轮次 = 3 | `code_audit` | 审核聚合层 / Notifier | 创建绑定当前 `pr_head_sha` 的 `ArbitrationRequest`，推进到 `pending_arbitration`，并通知人类 |
| 代码审核席位租约失效或截止时间到达 | `code_audit` | 审核聚合层 | 将未完成席位按缺席纳入聚合，并产出本轮最终结论 |
| 人类仲裁代码审核 | `pending_arbitration` | Human / BUS | 仅消费当前 `ArbitrationRequest`；根据仲裁结果回到 `coding`、恢复 `code_audit` 或进入 `merge_approval_pending` |
| 代码仲裁目标版本在等待期间被 supersede | `pending_arbitration` | BUS | 当前 `ArbitrationRequest` 失效，旧结论只保留审计，任务回到 `coding` |
| 当前轮代码审核 verdict 已收齐且聚合结果为通过，但 CI/保护未满足 | `code_audit` | 审核聚合层 / BUS | 保持在 `code_audit`，记录“等待门禁满足”状态 |
| 当前轮代码审核 verdict 已收齐且聚合结果为通过，且 CI/保护满足且无需审批 | `code_audit` | 审核聚合层 / BUS | 推进到 `merging` |
| 当前轮代码审核 verdict 已收齐且聚合结果为通过，且 CI/保护满足但需要审批 | `code_audit` | 审核聚合层 / BUS | 推进到 `merge_approval_pending` |
| 当前轮代码审核 verdict 已收齐且聚合结果为通过，且策略要求仅产出报告 | `code_audit` | 审核聚合层 / BUS | 生成阶段报告并推进到 `closed` |
| CI / 分支保护后续变为满足，且当前轮代码审核已通过 | `code_audit` | Git 平台 webhook / BUS | 重新评估合并门禁，推进到 `merge_approval_pending` 或 `merging` |
| 合并审批通过 | `merge_approval_pending` | Human / BUS | 推进到 `merging` |
| 合并审批拒绝 | `merge_approval_pending` | Human / BUS | 回到 `coding` 或 `cancelled` |
| PR 合并成功 | `merging` | 系统流程 / Git 平台桥接层 | 推进到 `merged` |
| issue 关闭与通知完成 | `merged` | 系统流程 / Git 平台桥接层 / Notifier | 推进到 `closed` |

## 10. 异常场景与默认策略

| 异常场景 | 默认系统行为 |
| --- | --- |
| Webhook 重复投递 | 不重复推进状态；依据 Task 和外部事件关联键做幂等处理 |
| Webhook 验签失败或事件ID非法 | 直接拒收，不推进状态，并记录安全事件 |
| issue / PR 外部状态与 BUS 不一致 | 以 BUS 为准，并尝试通过补写评论、补同步外部状态恢复一致 |
| issue 镜像创建长时间未完成 | 保持在 `issue_sync_pending`，记录补建动作并告警；采用指数退避补建，超过恢复阈值后进入 `waiting_human` 或回写 `failed` |
| issue 镜像创建返回 403/404 | 视为致命错误，结束补建并回写 `failed` |
| 已有外部 issue 却仍试图走镜像创建 | 视为绑定语义错误；应直接创建 issue 绑定并进入 `issue_created`，禁止重复建单 |
| Planner 回复失败但 BUS 已进入 `planning` | 保持 BUS 状态不丢失，并记录需要补发外部评论的恢复动作 |
| Alice 自己的评论 / Review / 状态回流再次命中触发器 | 仅更新回执与镜像状态，不再次触发 Planner、审核或 Coding，避免反馈环 |
| 多个计划审核结论不一致 | 仅在当前轮 verdict 收齐后才聚合；默认按“不通过”处理并回到 `planning`，同时累积 `plan_audit_round`；连续 3 轮仍不一致时再进入 `pending_arbitration` |
| 多个代码审核结论不一致 | 仅在当前轮 verdict 收齐后才聚合；默认按“不通过”处理并回到 `coding`，同时累积 `code_audit_round`；连续 3 轮仍不一致时再进入 `pending_arbitration` |
| 审核轮次未收齐 verdict 就先到首个 reject | 只记录当前 verdict，不立即推进；继续等待本轮剩余 verdict、租约失效或截止时间到达 |
| 审核 Agent 接单后失联 | 其席位在心跳租约失效后按缺席处理，不允许无限等待 |
| 计划版本变化后仍试图沿用旧审核结论 | 拒绝推进，旧结论失效，回到 `planning` |
| 仲裁期间目标版本变化 | 当前 `ArbitrationRequest` 立即失效；旧卡片和旧结论只保留审计，不得落到 superseded 版本上 |
| PR 创建失败 | 不进入 `code_audit`；保持或回退到 `coding`，并回写失败原因 |
| `pr_head_sha` 变化后仍试图沿用旧审核结论 | 拒绝推进，旧结论失效，回到 `coding` |
| PR 更新遇到远端非快进变化 | 先尝试自动 rebase；失败则回到 `coding` 解决，不允许 force push 覆盖远端历史 |
| 评测代码存在明显危险调用 | 在进入 `evaluation` 前被静态扫描或策略校验拦截，回到 `coding` 或进入 `waiting_human` |
| 评测结果来自旧 `pr_head_sha` / 旧数据集版本 | 只保留审计，不推进当前主状态 |
| CI 未通过或分支保护不满足 | 不进入实际合并；保持在 `code_audit`、`merge_approval_pending` 或 `merging` 之前，直到条件满足或人工取消 |
| 审核已通过但 CI / 分支保护稍后才满足 | 保持在 `code_audit`；由后续 webhook 或轮询重新评估门禁，再推进到合并相关状态 |
| 合并失败 | 不进入 `merged`；可在 `merging` 内做有限重试，重试耗尽后回写 `failed`；一旦进入 `failed` 即视为终态 |
| 预算命中硬熔断 | 停止新的 `coding` / `evaluation` 投递，并尝试终止已运行的评测作业 |
| BUS 崩溃后遗留长时间 `pending` 的 outbox 或外部作业 | 启动自检巡检并主动对账，修复幽灵任务，而不是被动等待 webhook |
| 外部事件重试耗尽 | 进入死信队列，等待人工处理，同时保持 BUS 真实状态不变 |
| MCP 健康检查失败、API 限流或凭据失效 | 暂停依赖该 MCP 的新请求并告警，不继续消耗危险动作；其他能力域保持独立运行 |
| 主模型不可用 | 按预设优先级降级到备用模型；无法降级时再回写 `failed` 或阻塞报告 |
| 人类在流程中追加新需求 | 视为新的 `IssueEvent`；若改变计划边界，则回到 `planning` |
| 任务被人工取消 | 推进到 `cancelled`，后续 Agent 不再继续消费该任务 |
| 模型网络连接异常 | 视为可短暂容忍的瞬时错误；在当前阶段有限重试，超过阈值后回写失败或阻塞报告 |
| 集群或外部存储故障 | 视为外部基础设施故障；暂停危险动作，保留当前真实状态，并回写需要人工介入的失败信息 |
| 飞书卡片或 Web 审批被重复点击 | 只接受首个有效 `confirmation_token`，其余点击返回“已处理/已失效” |
| 长时间投入后仍无法收敛 | 不要求 Agent 无限求解；优先形成阶段性报告，说明已尝试内容、当前阻塞点和建议下一步 |

这些策略的共同原则是：

- 不让外部平台状态覆盖内部真实状态
- 不让审核分歧被默默忽略
- 不让系统自发回流事件再次触发同类 Agent
- 不让失败阶段跳过必要回滚或回写

## 11. 故障上报与自检原则

结合 `draft.md` 的异常处理要求，这条工作流还应补充三条跨阶段原则：

- 对可恢复的瞬时异常，例如模型网络波动、Webhook 抖动、短时平台不可用，优先有限重试，不立刻改变主状态机方向
- 对不可忽略的基础设施异常，例如集群存储故障、仓库侧持续不可用，应尽快停止继续消耗，并把任务推进到可上报状态
- 当 Agent 已经花费较多时间和资源仍无法解决问题时，优先输出阶段性报告，而不是无限重试或静默超时

这里的“阶段性报告”在概念上至少应包含：

- 已完成的检查或尝试
- 当前明确的阻塞点
- 是否建议人工介入
- 是否建议稍后自动重试

此外，系统还应具备基础自检能力，用于在异常频发时辅助判断是单任务问题，还是系统性故障。

第一版可把自检巡检重点放在两类对象上：

- 超过 30 分钟仍处于 `pending` 的 `OutboxAction`
- 已提交但长时间未回写结果的外部评测作业

对于这些对象，系统应主动与 Git 平台或 Cluster MCP 对账，而不是只被动等待 webhook 或作业回调。

对于高优先级故障或长时间阻塞任务，系统应通过飞书等通知链路主动 @ 人类，而不是只把失败留在 issue 或 BUS 日志中。

## 12. 方案取舍

### 12.1 为什么要把计划和编码拆成两个阶段

- 计划先行可以降低直接编码带来的返工成本。
- 计划审核通过后再编码，能把“理解需求”和“实现需求”分离。
- 这也让多 Agent 协作更清晰，避免 CodingAgent 在需求模糊时盲目开工。

### 12.2 为什么计划审核和代码审核分开

- 计划审核关注方向是否对，代码审核关注实现是否对。
- 两类审核对象不同，合并在一起会让责任边界混乱。
- 分开之后，打回路径也更清晰：计划问题回 `planning`，实现问题回 `coding`。

### 12.3 为什么多个审核意见不一致时先打回，连续 3 轮后再升级人工仲裁

- 第一版应优先让系统按保守规则自我收敛，而不是每次出现冲突都立刻把问题甩给人类。
- 先按“不通过”处理，能够把审核分歧收敛到明确的修改动作，并给 Planner 或 Coding 一到两轮自我修正机会。
- 连续 3 轮仍然不一致，说明系统已经失去自动裁决能力，这时再升级到 `pending_arbitration` 更符合成本和风险平衡。

## 13. 与入口文档的关系

这份文档承接 [entry_processing_detail.md](./entry_processing_detail.md) 中的 `issue_created` 交接点。

两份文档的边界是：

- 入口文档负责“接入、分类、建 Task、建 issue 镜像”
- 本文负责“issue 后续如何驱动计划、审核、编码、PR、合并和关闭”

## 14. 结论

基于最新 `draft.md`，代码任务的异步主流程可以收敛为一句话：

系统先在 BUS 中确立代码任务，再借助 issue 作为协作载体，由 Planner、审核、编码、可选评测、代码审核等 Agent 按状态机推进；审核始终绑定明确版本并按租约聚合，评测始终绑定 `pr_head_sha + EvalSpec`，预算触顶时先熔断再等待人类决策，审核冲突前 2 轮默认按不通过处理并回退，连续 3 轮仍不一致才进入 `pending_arbitration`，最终要么进入合并审批/执行分支，要么按策略产出阶段报告并关闭，所有关键进度最终仍以 BUS 状态为准。
