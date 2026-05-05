---
name: alice-goal
description: 在当前会话中设定并持续执行一个长期目标。执行过程中自动续跑，直到目标完成、超时或被暂停。
---

# Alice 目标执行器

使用 `scripts/alice-goal.sh` 管理当前会话的长期目标。

## 常用命令
- 查看当前目标： `scripts/alice-goal.sh get`
- 创建新目标： `scripts/alice-goal.sh create '{"objective":"为项目补充单元测试"}'`
- 创建带截止时间： `scripts/alice-goal.sh create '{"objective":"...","deadline_in":"48h"}'`
- 暂停： `scripts/alice-goal.sh pause`
- 恢复： `scripts/alice-goal.sh resume`
- 标记完成： `scripts/alice-goal.sh complete`
- 清除： `scripts/alice-goal.sh clear`

## 目标字段
| 字段 | 类型 | 必须 | 说明 |
|------|------|------|------|
| `objective` | string | 是 | 目标描述 |
| `deadline_in` | string | 否 | 截止时间，如 "48h" "1h30m"，默认48h |

## 使用建议
1. 目标创建后自动开始执行。
2. 用户在群里 @Alice 不会中断目标，消息排队在下一次续跑时处理。
3. 只有确实完成时才调用 complete。到达截止时间后系统自动超时收尾。
