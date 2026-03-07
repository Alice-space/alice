# 06. PR 驱动评测与回写闭环

## 1. 目标

已有代码优化任务不能停留在“改完代码就算完成”。系统必须把 PR、评测、比较、回写和下一轮迭代连成闭环。

## 2. 关键对象

- `AcceptancePolicy`
- `EvalTask`
- `BaselineSnapshot`
- `EvalReport`
- `EvalRequested` / `EvalCompleted` / `EvalFailed`

规则：

- `EvalTask` 是 `task_type = evaluation` 的特化任务。
- `EvalReport` 是唯一可用于门禁和回写的评测结论对象，不能用自由文本替代。

## 3. 触发源

| 触发源 | 说明 |
| --- | --- |
| PR 新建 | 开启第一轮自动评测 |
| PR 新 commit | 重新评测 |
| PR 标签变化 | 例如打上 `needs-eval` 或 `force-eval` |
| 手动信号 | 人工要求复评 |
| 定时评测 | 夜间大规模 benchmark |

## 4. EvalTask 生成规则

当触发评测时，控制平面执行：

1. 查询 PR 当前 head commit。
2. 读取项目 `evaluation_policy`。
3. 生成 `EvalTask`。
4. 锁定该 commit SHA 对应的评测任务。
5. 设置 PR check 为 `pending`。

### 4.1 EvalTask 最小字段

- `task_id`
- `eval_id`
- `project_id`
- `pr_id`
- `commit_sha`
- `baseline_ref`
- `dataset_ref`
- `benchmark_suite_ref`
- `environment_ref`
- `seed_policy`
- `acceptance_policy`
- `target_executor`
- `retry_policy`
- `aggregation_rule`

### 4.2 可比性要求

若以下任一信息缺失，评测只能记为信息性结果，不能进入自动门禁：

- 数据集或样本快照 ID
- benchmark 脚本或 suite 版本
- 环境指纹，例如镜像、依赖、驱动或执行器 fingerprint
- 指标 schema
- 随机种子与重跑策略

## 5. Baseline 选择

基线来源按优先级：

1. 项目显式配置的黄金基线。
2. 主分支最近一次通过且仍可比较的基线。
3. 项目记忆中最近验证的 `evaluation_baseline`。

### 5.1 BaselineSnapshot 必备字段

- `baseline_id`
- `source_kind`
- `code_ref`
- `dataset_ref`
- `benchmark_suite_ref`
- `environment_ref`
- `metric_schema`
- `seed_policy`
- `sample_size`
- `verified_at`

规则：

- 基线不只是一组指标，还必须绑定代码、数据和环境。
- 没有可用基线时，结果必须明确标记“无可靠对比基线”，默认不能自动宣称通过优化目标。

## 6. 评测矩阵与聚合

若一个 PR 需要 CPU、GPU、多数据集或多规模测试，必须显式定义评测矩阵。

### 6.1 EvalMatrix

每个子评测至少包含：

- `shard_id`
- `required`
- `environment_ref`
- `dataset_ref`
- `max_retries`
- `timeout`
- `weight`

### 6.2 AggregationRule

聚合规则至少定义：

- 哪些 shard 是必需项
- 哪些 shard 只是信息性项
- 是否允许单个 shard `neutral`
- 多次重跑如何聚合
- 总 check 何时输出 `success` / `failure` / `neutral`

规则：

- PR 总 check 只能由聚合器写出，子评测只写子状态。
- 任一必需 shard 失败或不可比，默认不能自动通过。

## 7. 评测流程

```text
PR Update -> EvalRequested -> EvalTask
         -> Execute Benchmarks -> Collect Artifacts
         -> Compare with Baseline -> EvalReport
         -> Aggregate -> Pass | Retry | Human Gate
```

### 7.1 执行阶段

- 由 `Experimenter` 角色驱动。
- 通过 `LocalExecutor` 或 `RemoteExecutor` 运行。
- 产出统一结构化指标，不允许只上传自由文本。

### 7.2 评审阶段

- 由 `Reviewer` 角色或 `LLMRuntime` 解读指标。
- 输出是否通过、是否回退、是否有风险、推荐下一步动作。

### 7.3 EvalReport 最小要求

- `comparable`
- `metric_deltas`
- `risk_findings`
- `artifact_refs`
- `recommended_action`
- `confidence`

## 8. 结果处理

### 8.1 通过

- 写 `EvalCompleted`。
- 更新 PR check 为 `success`。
- 回写简短 PR 评论。
- 工作流进入 `Reporting` 或等待人工 review。

### 8.2 不通过但可重试

- 写 `EvalFailed`。
- 更新 PR check 为 `failure` 或 `neutral`，取决于策略。
- 自动生成新的 `Implementing` 子任务。
- 子任务上下文必须包含：
  - 回退指标
  - 相比基线的差异
  - 失败原因摘要
  - 推荐修改方向

### 8.3 不通过且需人工决策

- 进入 `WaitingHuman`。
- 飞书通知用户。
- PR check 标记为 `failure` 或 `neutral`，由项目策略决定。

## 9. 并发与 supersede

### 9.1 多次 push

若 PR 在评测进行中又有新 commit：

- 新 commit 触发新的 `EvalRequested`。
- 老 commit 对应的评测运行标记为 `Superseded`。
- 老结果只入审计，不影响最新 PR 状态。

### 9.2 多环境评测

- 不同 shard 可以并行执行。
- 聚合器等待所有必需 shard 结束或被 `Superseded` 后再写总 check。
- 某 shard 基础设施失败时，可按 `AggregationRule` 标为 `neutral` 或整体转人工。

## 10. 非确定性结果

对存在随机性的实验：

- 允许定义 `tolerance`。
- 允许定义重跑次数。
- 不允许用单次偶然提升直接宣布优化成功。
- `EvalReport` 必须说明采用的是单次结果、均值、分位数还是置信区间。

## 11. 回写与记忆

- 评测结果写回 PR 评论时，至少包含比较对象、关键指标变化、是否通过、下一轮建议和产物链接。
- 只有“可比较且稳定”的评测结果才能晋升为 `evaluation_baseline` 记忆。
- 无法比较的结果可归档，但不得污染项目基线。
