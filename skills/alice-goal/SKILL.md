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
