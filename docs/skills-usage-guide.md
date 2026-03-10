# Skills 使用指南

本文说明如何在本仓库中使用 `skills/` 目录下的能力包，并给出 4 个可直接复用的示例。

## 1. 基本用法

在对话中直接提到 skill 名称（推荐使用 `$skill-name`）即可触发对应能力。例如：

- `$public-info-query`
- `$cluster-query`
- `$issue-delivery`
- `$schedule-management-skill`

你也可以一次组合多个 skill，例如：

- `$reception-router + $issue-delivery + $code-implementation + $code-review`

常见组合建议：

- 入口分流：`$reception-router`
- 只读查询：`$public-info-query` 或 `$cluster-query`
- 代码交付：`$issue-delivery + $change-planning + $code-implementation + $code-review + $result-reporting`
- 研究探索：`$research-exploration + $research-planning + $experiment-analysis + $result-reporting`
- 控制面：`$schedule-management-skill` 或 `$workflow-management-skill`

## 2. 示例 1：天气/公开信息查询（直答路径）

示例输入：

```text
$public-info-query 查询上海明天白天气温和降雨概率，用 3 条要点返回。
```

推荐行为：

1. 抽取地点、时间、输出格式
2. 执行只读信息查询
3. 直接返回结果，不走持久化 workflow

适合搭配：

- `$reception-router`（先做风险与 promotion 判断）

## 3. 示例 2：集群排队查询（内部只读）

示例输入：

```text
$cluster-query 查询 GPU A100 队列当前排队长度、我名下作业状态，并按队列给建议。
```

推荐行为：

1. 解析查询范围（队列、用户、时间窗）
2. 调用只读 cluster 查询
3. 返回结构化摘要与风险提示

适合搭配：

- `$result-reporting`（统一整理输出）

## 4. 示例 3：代码需求交付（issue-delivery）

示例输入：

```text
$issue-delivery 修复用户登录接口 500，按“计划->实现->审查->报告”执行，并给出候选 patch 与测试说明。
```

推荐组合：

- `$reception-router`
- `$change-planning`
- `$repo-understanding`
- `$code-implementation`
- `$code-review`
- `$result-reporting`

期望产物：

- `task_brief`
- `plan`
- `candidate_patch`
- `test_notes`
- `review_result`
- `report`

## 5. 示例 4：定时任务配置（控制面）

示例输入：

```text
$schedule-management-skill 把 daily-issue-digest 改为工作日 09:00 触发，时区 Asia/Shanghai，保留 target workflow 不变，并输出结构化变更请求。
```

推荐行为：

1. 解析操作类型（创建/修改/暂停/删除）
2. 规范化 cron、时区、目标 workflow 引用
3. 输出可审计的 `schedule_request`
4. 进入审批/确认后再执行外部变更

适合搭配：

- `$workflow-management-skill`（如果需要同步改 workflow）
- `$result-reporting`（生成最终变更摘要）

## 6. 排错建议

- 如果你发现没有触发预期 skill，请在输入里显式加 `$skill-name`。
- 如果请求涉及外部写操作、预算或审批，先加 `$reception-router`，避免误走只读路径。
- 如果希望输出稳定结构，末尾附加“请输出结构化字段 + 简短结论”。

