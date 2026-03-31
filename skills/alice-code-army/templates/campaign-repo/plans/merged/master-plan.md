---
status: draft
human_approved: false
---

# Master Plan

## Merge Summary
- 待补充

## Phases
- phase 数量由 planner 根据目标、依赖和 write scope 决定
- 模板只保留 `P01` 作为目录示例；需要更多 phase 时，由 planner 在 `proposal`、`master plan` 和 `phases/` 下补齐

## Task Expansion
- 目标：按实际计划展开任务，不预设固定 phase 数量或 task 总数
- 要求：每个 task 都展开成 `phases/Pxx/tasks/Txxx/` 文件夹
- task 的粒度标准是“executor 只靠 task 文件夹即可开工，不需要回聊天补上下文”
- 如果一个 task 同时覆盖多个独立产物、多个松耦合 repo 改动或多个不相干 write_scope，继续拆分
- `task.md` 必须写清 Goal / Background / Acceptance / Deliverables，且 Goal / Acceptance 不能停留在泛词
- `context.md` 必须写清 repo、关键文件、依赖和背景信息，Relevant Files 要尽量落到具体路径
- `plan.md` 必须写清执行步骤、验证和 handoff；Execution Steps 要能直接照着做，Validation 要可观察
- executor 应只靠 task 文件夹即可开工，不再回聊天补上下文
