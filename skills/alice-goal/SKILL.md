---
name: alice-goal
description: 为当前会话设定并持续执行长期目标
---

# Alice Goal

在当前 work session 中设定并自动迭代执行的长期目标。

## 命令

- `scripts/alice-goal.sh get` - 查看当前目标
- `scripts/alice-goal.sh create '{"objective":"为项目补充单元测试","deadline_in":"48h"}'` - 创建目标（deadline_in 可选，默认 48h）
- `scripts/alice-goal.sh pause` - 暂停
- `scripts/alice-goal.sh resume` - 恢复
- `scripts/alice-goal.sh complete` - 仅限 `audit_completion` subagent 调用，Main Agent 在任何情况下都不得直接调用此命令
- `scripts/alice-goal.sh clear` - 删除目标

## 核心机制

### 自动循环原理

每次 Main Agent (即本次设定`alice-goal`的Agent)执行完毕并退出后，系统会立即自动重新注入相同 prompt 唤醒 Main Agent。因此 Main Agent 应假设会被无限次唤醒，每轮执行"审计 → 决策 → 行动 → 退出"的完整回合。若目标未完成且未超时，每回合应当先进行严格目标审计流程，再根据对话历史判断下一步，然后选择一项具体的任务立即执行。

### 严格审计流程

Main Agent 严禁自行执行审计步骤或直接调用 `alice-goal complete`。所有审计和完成操作必须通过 `audit_completion` subagent 完成。

1. 必须调用 `audit_completion` subagent 完成审计 — Main Agent 严禁自行执行任何审计步骤（列清单、核证据、判断缺口等），也严禁以任何形式调用 `alice-goal complete`。
2. `audit_completion` 必须执行的审计步骤：
   - 明确目标为具体交付物/成功标准
   - 列出检查清单（每个要求对应具体证据）
   - 逐项核对真实证据（文件/输出/PR等，不凭记忆或猜测）
   - 警惕假信号（测试通过/代码量大≠完成，必须覆盖每一项要求）
   - 找出缺口（任何缺失、薄弱或未覆盖即为未完成）
   - 不依赖意图、半成品、耗时等
   - 仅当所有要求满足且无遗留工作时，方可进入下一步
3. 标记完成（仅限 `audit_completion`）：
   - 只有 `audit_completion` 有权调用 `alice-goal complete`。成功后向用户报告总耗时。
   - 绝对禁止因超时或 Main Agent 打算停止而标记完成。
   - 绝对禁止在当前执行回合标记完成；必须在下一独立回合中由 `audit_completion` 重新完成审计后才可调用。
4. 违规后果：Main Agent 若自行调用 `complete`、自行审计或绕过 `audit_completion`，视为严重违规，必须立即停止输出并进入下一次循环。

### 实用规则

- 等待不阻塞：需轮询外部状态或等资源就绪时，必须用 `sleep` 主动让出，严禁忙等或无限循环。
- 单回合单步：Main Agent 每回合只推进一项具体任务（通常是调用一个 subagent 或一个原子操作），完成后立即退出。
- 极限并行与结果排序：
   - 任何可拆分的独立子任务必须分给独立 subagent 并行执行。
   - 汇总所有 subagent 结果时，严格按照结果对完成目标的重要性降序输出：最重要结果放在最前面详细描述，次要结果可缩减篇幅或后置。不要因为每个 subagent 都完成了任务就平均用力。
- 僵局重新规划：出现以下情况之一时，Main Agent 必须立即停止当前行动并执行重新规划：依赖缺失、任务无法拆分、连续多回合无实质推进、审计缺口不明确、外部接口不可用等。重新规划步骤：分析目标缺口→调整策略→重新拆解任务序列→在本回合或下一回合启动新的并行 subagent 群。严禁原地重试、空转或重复失败动作。
