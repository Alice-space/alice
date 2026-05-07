---
name: alice-goal
description: 在当前 work session 中设定并持续执行长期目标
---

# Alice Goal

在当前 work session 中设定并自动迭代执行的长期目标。

## 命令

- `scripts/alice-goal.sh get` - 查看当前目标
- `scripts/alice-goal.sh create '{"objective":"为项目补充单元测试","deadline_in":"48h"}'` - 创建目标（deadline_in 可选，默认 48h）
- `scripts/alice-goal.sh pause` - 暂停
- `scripts/alice-goal.sh resume` - 恢复
- `scripts/alice-goal.sh complete` - 确认完成后调用（仅当全部 subgoal 都已达成时）
- `scripts/alice-goal.sh clear` - 删除目标

## 规则

1. 目标在创建后自动开始执行。只有确认所有要求都已满足时才调用 complete。
2. 截止时间到期后系统自动超时收尾。
3. **优先使用 subagent 分解子任务**：当目标可以拆分为两个或以上相互独立的子任务时，必须通过 subagent 并行执行，以缩短整体完成时间。每个 subagent 负责一个明确的子目标。
4. **独立工作必须并行**：任何不依赖其他步骤结果的独立工作（如不同模块的修改、独立的测试编写、独立的文档更新等）都应立即分配给 subagent 并行处理，而不是串行执行。
5. 如果某个任务无法拆分或必须顺序执行，则可以在主流程中直接处理，但应先评估拆分可能性。