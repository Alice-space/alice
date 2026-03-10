# 控制面工作流概念设计

本文讨论两类高风险控制面 workflow：

- `schedule-management`
- `workflow-management`

它们和代码交付 workflow 的共同点，是都运行在 `DurableTask` 上；不同点是，它们修改的不是业务仓库产物，而是系统未来行为本身。

## 1. 范围

本文回答三类问题：

- schedule / workflow 定义变更为什么不能停留在 direct-answer path
- 控制面 workflow 的最小 step 图、产物和 gate 应该是什么
- 控制面对象怎样路由、确认、发布和避免影响已运行 task

## 2. 共享前提

控制面 workflow 必须继承以下不变量：

- 入口阶段已经形成 `PromotionDecision`，并明确命中 promote 硬规则
- `DurableTask` 已经创建，并绑定到不可变 workflow revision
- 控制面对象必须有专属路由键，不能只靠对话线程
- 高风险副作用必须经过 gate，再走 `outbox + MCP`
- 已运行 task 不会因为控制面发布新配置而自动漂移

## 3. 控制面对象与路由键

最小对象集合：

| 对象 | 作用 |
| --- | --- |
| `schedule_request` | 定时任务的结构化变更请求 |
| `workflow_change_request` | workflow 定义的结构化变更请求 |
| `validation_report` | schema / 策略 / 兼容性校验结果 |
| `ScheduledTask` | 持久化调度对象 |
| `WorkflowBinding` | 当前 task 所使用的 workflow 绑定 |

最小路由键：

- `scheduled_task_id`
- `control_object_ref`
- `workflow_object_ref`

优先级原则：

- 显式 `task_id` / `request_id` 和 `reply_to_event_id` 仍然高于控制面对象键
- 修改、暂停、删除定时任务优先按 `scheduled_task_id`
- 创建新定时任务至少要带一个“调度注册表类”的 `control_object_ref`
- workflow 定义变更优先按 `workflow_object_ref`
- 只命中活跃对象；终态对象默认不复用
- 若 `reply_to_event_id` 指向普通代码/研究 task，但同一事件显式携带 `scheduled_task_id` 或 `workflow_object_ref`，BUS 不得把该控制面动作吞进原 task；应拆出新的控制面 request/task，或先澄清

这里冻结的是“路由键类别”，不是某个实现字面值。比如 `control_object_ref` 可以指向某个 schedule registry 对象，但 `schedule_registry` 只是例子，不应被理解为平台固定常量。

## 4. `schedule-management` Workflow

### 4.1 适用场景

`schedule-management` 用于：

- 创建新的定时任务
- 修改既有定时任务
- 暂停 / 恢复 / 删除定时任务

### 4.2 为什么必须是 `DurableTask`

从第一性原理看，定时任务的关键不是“这句话像不像控制命令”，而是它会改变系统未来行为。

因此只要请求涉及：

- 创建持久化调度对象
- 修改调度对象
- 删除调度对象

就必须命中 promote 硬规则，不能留在 `EphemeralRequest` 内偷偷执行。

### 4.3 最小 step 图

推荐的最小 step 图：

1. `parse_request`
2. `validate_request`
3. `apply_change`
4. `report`

这是一张评审用参考图，不是 BUS 平台级固定流水线。第一版真正冻结的是：

- 需要结构化请求产物
- 高风险动作前需要 `confirmation` 或 `approval` 这类既有 gate
- 外部变更必须经 `outbox + MCP`

常见 gate 挂点：

- `validate_request` 与 `apply_change` 之间可挂 `confirmation`
- 对更高风险的批量或跨域变更，也可挂 `approval`

### 4.4 关键产物

`schedule-management` 通常至少产出：

- `schedule_request`
- 可选 `validation_report`
- 最终的 `ScheduledTask` 引用，其中必须固化目标 `workflow_id/source/rev`、输入模板和启用状态

### 4.5 step 语义

`parse_request`

- 把自然语言转成结构化 `schedule_request`
- 至少明确频率、时间、目标 workflow、输入模板和作用范围

`validate_request`

- 检查目标 workflow 是否存在
- 检查所需引用是否齐全
- 检查是否超出允许的控制面权限
- 信息不全时进入 `WaitingHuman`

`confirmation` / `approval` gate

- 若策略认为该变更高风险，则创建确认 gate
- 审批对象必须绑定结构化 `schedule_request`，而不是只绑定用户原话
- gate 是 step 之间的治理对象，不是业务 step 本身

`apply_change`

- BUS 先写 `OutboxRecord`
- Control MCP 创建 / 更新 / 暂停 / 删除 `ScheduledTask`
- BUS 回写调度对象引用和状态

`report`

- 向人类返回“创建了什么、何时触发、如何再修改”的结构化摘要

### 4.6 scheduler 触发语义

定时任务触发后的正确语义不是“替某个 agent 想起一件事”，而是：

1. scheduler 读取 `ScheduledTask`
2. scheduler 生成新的 `ScheduleTriggered` `ExternalEvent`，至少带上 `scheduled_task_id`、目标 workflow 引用和来源 schedule revision
3. BUS 仍按统一入口规则处理该事件：先命中控制面来源对象，再形成可审计的 `PromotionDecision`
4. 由于这类系统事件天然命中 promote 硬规则，BUS 可以在同一事务里立即把该 request 包络 promote 成新的 `DurableTask`
5. BUS 为这个新 task 绑定 `ScheduledTask` 指定的目标 workflow revision
6. 这个新 task 再按普通 durable workflow 路径跑完自己的生命周期

也就是说，`ScheduledTask` 是任务来源，不是隐藏记忆。

## 5. `workflow-management` Workflow

### 5.1 适用场景

`workflow-management` 用于：

- 修改 workflow manifest
- 修改 workflow template / prompt
- 发布新的 workflow revision
- 做 schema / 策略 / 兼容性校验

### 5.2 为什么必须是受控 workflow

workflow 定义本身决定系统未来如何运行，因此它比普通代码修改更像“控制面配置发布”。

如果允许用户自然语言直接改线上 workflow，会带来三类不可接受风险：

- 权限边界被文本绕过
- 已运行 task 的语义漂移
- 缺少发布、审批和回滚审计

所以自然语言只能触发 `workflow-management` task，不能直接成为生效脚本。

### 5.3 最小 step 图

推荐的最小 step 图：

1. `parse_change_request`
2. `read_current_workflow`
3. `produce_diff`
4. `validate_change`
5. `publish`
6. `report`

同样，这里给的是最小评审模板，不是平台固定枚举。评审真正需要锁住的是：

- workflow 变更先形成结构化 `workflow_change_request`
- 发布前必须经过既有 gate 类型和结构化校验
- 发布结果必须形成新的 revision 绑定，并明确“不影响哪些旧 task”

常见 gate 挂点：

- `validate_change` 与 `publish` 之间可挂 `approval`
- 对高风险控制面发布，也可叠加 `confirmation`

### 5.4 关键产物

`workflow-management` 通常至少产出：

- `workflow_change_request`
- `validation_report`
- 候选 diff / PR 引用
- 新的 `workflow_source` / `workflow_rev` / `manifest_digest`

### 5.5 step 语义

`parse_change_request`

- 把自然语言变成结构化 `workflow_change_request`
- 明确目标 workflow、想改的规则、不允许越界的边界

`read_current_workflow`

- 读取当前 workflow 定义
- 定位要修改的 step、gate、artifact 契约

`produce_diff`

- 生成 manifest diff、template diff 或 PR
- 必须把修改范围限制在允许的文件和对象内

`validate_change`

- schema 校验
- 机器约束字段校验
- 权限边界校验
- 兼容性或迁移说明校验

`approval` / `confirmation` gate

- 审批对象不应是自然语言原文
- 应至少包含：结构化变更请求、候选 diff、校验结果、影响摘要
- gate 记录审批，不替代 `publish` 这个业务 step

`publish`

- 通过代码托管 MCP 或 workflow registry MCP 发布
- BUS 回写新的 revision 和审批记录

`report`

- 明确告诉人类：发布了哪个 revision，影响哪些新 task，不影响哪些旧 task

## 6. 旧 task 不漂移原则

控制面 workflow 最关键的不变量是：

- 已运行 task 继续绑定自己的旧 revision
- 只有新建 task，或在允许重绑定的审批点显式重规划，才会使用新 revision
- 显式重规划也不能原地改写旧 `WorkflowBinding`；只能追加新的 binding 记录，并保留旧 binding 的完整审计历史

否则系统会出现“同一个 task 在执行中语义变了”的不可审计行为。

## 7. 人类介入点

控制面 workflow 中，人类通常要在这些地方介入：

- 补充缺失字段
- 确认高风险 schedule 变更
- 审批 workflow diff / 发布
- 决定发布后是否回滚

这些介入都必须表现为新的 `ExternalEvent`，并通过控制面对象路由到正确 task。

## 8. 与其他文档的交接

本文假设入口阶段已经完成：

- `PromotionDecision`
- `DurableTask` 创建
- `WorkflowBinding`

如果这些前提还不存在，应回到 [entry_processing_detail.md](./entry_processing_detail.md)。

如果当前任务属于代码交付或研究闭环，则应回到 [issue_workflow_detail.md](./issue_workflow_detail.md)。
