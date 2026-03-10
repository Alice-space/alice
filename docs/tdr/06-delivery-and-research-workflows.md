# TDR 06: 交付与研究两类 workflow

## 1. 目标

本文只落两类业务 workflow：

- `issue-delivery`
- `research-exploration`

控制面 workflow 和 `outbox + MCP` 细节放到 [07-external-effects-control-plane-and-scheduler.md](./07-external-effects-control-plane-and-scheduler.md)。

## 2. `issue-delivery`

### 2.1 绑定条件

以下信号通常绑定 `issue-delivery`：

- 需要仓库写权限
- 主成功条件是交付 patch / PR / merge
- 需要 review 与人工确认
- 可能有测试，但不以实验闭环为主

### 2.2 默认 step 图

| 顺序 | step | 主角色 | 输入 | 输出 |
| --- | --- | --- | --- | --- |
| 1 | `triage` | `leader` | request/context refs | `task_brief` |
| 2 | `plan` | `leader` | `task_brief` | `plan` |
| 3 | `code` | `leader` / `worker` | `plan` | `candidate_patch`, `test_notes`, 可选 `pr_ref` |
| 4 | `review` | `reviewer` | patch/PR/test evidence | `review_result` |
| 5 | 可选 `merge` | `leader` | `review_result`, gate results | merge receipt |
| 6 | 可选 `report` | `leader` | 全部 artifact | `report` |

常见 gate：

- `plan` 后 `approval`
- 高风险外部写前 `confirmation`
- `merge` 前 `approval`

### 2.3 artifact 约束

必需 artifact：

- `task_brief`
- `plan`
- `candidate_patch`
- `review_result`

可选 artifact：

- `test_notes`
- `report`

### 2.4 回退规则

| 触发条件 | 回退点 |
| --- | --- |
| review 不通过 | `code` |
| 需求边界变化 | `plan` |
| merge gate 被拒绝 | `review` 或 `plan` |
| PR / branch 引用被 supersede | `code` |

### 2.5 外部动作

`issue-delivery` 允许的 MCP 域：

- `github`
- `gitlab`
- 可选 `cluster`（仅测试/评测辅助）

允许的典型 action：

- create/update issue comment
- create/update branch
- create/update PR
- request review
- merge PR

这些动作都必须经 `outbox`。

## 3. `research-exploration`

### 3.1 绑定条件

以下信号通常绑定 `research-exploration`：

- `async=true`
- `budget_required=true`
- `recovery_required=true`
- 主成功条件是实验指标或评测结果

### 3.2 默认 step 图

| 顺序 | step | 主角色 | 输入 | 输出 |
| --- | --- | --- | --- | --- |
| 1 | `plan` | `leader` | request/context refs | `plan`, 可选 `validation_report` |
| 2 | `code` | `leader` / `worker` | `plan` | `candidate_patch` |
| 3 | `evaluate` | `evaluator` / `leader` | patch + eval config | `evaluation_result` |
| 4 | 可选 `review` | `reviewer` | evaluation evidence | `review_result` |
| 5 | `report` | `leader` | 全部 artifact | `report` |

常见 gate：

- `plan` 后 `approval`
- 预算耗尽后 `budget`
- `evaluation_result` 产出后的自动 `evaluation` 判定；默认不进入 `WaitingHuman`

### 3.3 artifact 约束

必需 artifact：

- `plan`
- `candidate_patch`
- `evaluation_result`

可选 artifact：

- `review_result`
- `report`

### 3.4 回退规则

| 触发条件 | 回退点 |
| --- | --- |
| 指标未达标但方向不变 | `code` |
| 数据集 / 指标 / 停止条件变化 | `plan` |
| 预算追加但代码与 `EvalSpec` 不变 | `evaluate` |
| 集群作业失败且可恢复 | `evaluate` |

### 3.5 预算与取消

`research-exploration` 必须写 `UsageLedger`：

- token
- 估算模型成本
- CPU/GPU 用量
- 预算剩余

预算硬约束命中后：

1. task 进入 `WaitingHuman`
2. 传播取消到 cluster 作业
3. 等待新的 `budget` gate 回流事件

而 `evaluation` 本身的默认路径是自动化的：

1. `evaluate` step 产出 `evaluation_result`
2. runtime 根据 manifest 阈值判定 `report`、`code` 或 `Succeeded`
3. 只有当 manifest 明确要求人工裁决，才额外打开 gate

`research-exploration` 的 manifest 至少要把下面这些字段写实：

- `result_family=evaluation_result`
- `rules[*].metric`
- `rules[*].op`
- `rules[*].threshold`
- `aggregate`
- `on_pass`
- `on_fail`
- `on_error`

否则 runtime 无法确定性决定是回 `code`、进 `report`，还是进入 `WaitingHuman`。

## 4. 人类介入

两类 workflow 都必须支持这些人类动作：

- 补充输入
- 批准 / 拒绝
- 取消
- 打回到更早 step

但路由方式统一是：

- 新的 `ExternalEvent`
- 固定 route keys
- 校验当前 binding / step / gate 是否仍活跃

## 5. 与 `StepExecution` 的关系

`triage`、`plan`、`code`、`review`、`evaluate`、`merge`、`report` 都只是 `StepExecution.StepID` 的取值，不是 task 顶层状态。

这意味着：

- 一个 workflow 可以省略某些 step
- 控制面 workflow 可以定义完全不同的 step
- 不需要因为新 workflow 出现就改 task 顶层枚举

## 6. 关键接口

```go
type DeliveryPlanner interface {
    BuildPlan(ctx context.Context, pack *domain.ContextPack) (*domain.Artifact, error)
}

type Evaluator interface {
    Evaluate(ctx context.Context, exec *domain.StepExecution) (*domain.Artifact, error)
}
```

## 7. 测试建议

必须覆盖：

- `issue-delivery` review 不通过回 `code`
- 需求边界变化回 `plan`
- `research-exploration` 预算恢复回 `evaluate`
- 数据集变化回 `plan`
- merge 不是默认动作，必须显式存在于 manifest
