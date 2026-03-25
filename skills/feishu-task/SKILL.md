---
name: feishu-task
description: 通过飞书 OpenAPI 的 Task v2 管理官方任务与任务清单（CRUD、分派、截止时间、成员协作）。仅当用户明确提到 `飞书任务`、`Feishu Task`、`Task v2` 时触发；不要把它和 Alice 自动化任务混用。
---

# 飞书任务

## 概览

维护约束：当前会话里 `.agents/skills/...` 的已安装 skill 副本来自 Alice 安装/更新流程，不应直接修改；需要变更 skill 时，应修改 Alice 仓库里的 `alice/skills/...` 源文件，再通过安装流程同步进去。

本 skill 只使用飞书官方 `task/v2` OpenAPI 管理任务与任务清单。
必须与 Alice 自动化任务严格隔离。

## 边界规则

1. `Alice任务` 与 `飞书任务` 是两套系统。
- `Alice任务`：连接器本地自动化任务（例如 `automation_task_*`）。
- `飞书任务`：官方 OpenAPI Task 资源（`/open-apis/task/v2/...`）。

2. 只有用户明确表达“飞书任务意图”才触发本 skill。
- 正向信号：`飞书任务`、`Feishu Task`、`task/v2`、带 OpenAPI 语境的“任务清单”。
- 负向或模糊信号：只说“任务”但未限定系统。先追问再执行。

3. 本 skill 的全部操作仅使用 Task v2。
- 只调用 `task/v2` 端点，不混入其他版本。

## 工作流

1. 先确认系统边界。
- 用户说“飞书任务”才继续。
- 用户说“Alice任务”则切换到 Alice 自动化路径。

2. 确认 token 模式和身份类型。
- “当前人的任务”使用 `user_access_token`（Task v2 列表接口要求用户 token）。
- 创建/更新/删除通常支持用户 token 或 tenant token。
- 默认 `user_id_type=open_id`，除非用户明确要求其他类型。

3. 先用脚本执行。
- 优先使用 `scripts/feishu-task-v2.sh` 做可预测 CRUD。
- 若脚本参数覆盖不到，再直接调用同一 Task v2 接口。

4. 返回简洁结果。
- 包含任务/任务清单 ID、关键变更字段、建议下一步。
- 若 API 返回非零 code，必须带上 `code` 与 `msg`，并停止盲目重试。

## 必备操作覆盖

1. 查看我当前管理的任务。
- `list-managed-tasklists` 后 `list-tasklist-tasks <tasklist_guid>`。
- 对应端点：`GET /open-apis/task/v2/tasklists`、`GET /open-apis/task/v2/tasklists/:tasklist_guid/tasks`。

2. 查看当前人的任务。
- `list-my-tasks`。
- 对应端点：`GET /open-apis/task/v2/tasks`（`type=my_tasks`）。

3. 发布新任务。
- `create-task`。
- 对应端点：`POST /open-apis/task/v2/tasks`。

4. 为任务分配成员。
- `assign-task`。
- 对应端点：`POST /open-apis/task/v2/tasks/:task_guid/add_members`。

5. 设置任务截止时间。
- `set-deadline`。
- 对应端点：`PATCH /open-apis/task/v2/tasks/:task_guid`，`update_fields=["due"]`。

6. 创建与管理任务清单。
- `create-tasklist`、`list-tasklists`、`update-tasklist-name`、`delete-tasklist`、`add-tasklist-member`、`remove-tasklist-member`。
- 对应端点均在 `/open-apis/task/v2/tasklists` 下。

7. 必备补充操作。
- `get-task`、`update-task-summary`、`delete-task`、`remove-task-member`。
- 可幂等写操作尽量携带 `client_token`。

## 快速命令

```bash
# 查看帮助
$HOME/.agents/skills/feishu-task/scripts/feishu-task-v2.sh help

# 查看我的任务（需要 FEISHU_USER_ACCESS_TOKEN）
$HOME/.agents/skills/feishu-task/scripts/feishu-task-v2.sh list-my-tasks

# 创建任务
$HOME/.agents/skills/feishu-task/scripts/feishu-task-v2.sh create-task "准备周会材料" "周三前完成"

# 设置截止时间（毫秒时间戳）
$HOME/.agents/skills/feishu-task/scripts/feishu-task-v2.sh set-deadline <task_guid> 1767225600000 false
```

## 参考资料

- 详见 `references/task-v2-api-map.md`。
