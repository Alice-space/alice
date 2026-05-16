---
name: alice-goal
description: 为当前会话设定并持续执行长期目标
---

# Alice Goal

在当前 work session 中设定并自动迭代执行的长期目标。

## 命令

- `scripts/alice-goal.sh get` - 查看当前目标
- `scripts/alice-goal.sh create '{"objective":"为项目补充单元测试","deadline_in":"48h"}'` - 创建目标（deadline_in 可选，默认 48h）
- `scripts/alice-goal.sh delay <duration> <reason>` - **每一轮对话必须调用**，设置下一轮迭代的延迟唤醒时间
- `scripts/alice-goal.sh pause` - 暂停
- `scripts/alice-goal.sh resume` - 恢复
- `scripts/alice-goal.sh complete` - 仅限 `audit_completion` subagent 调用，Main Agent 在任何情况下都不得直接调用此命令
- `scripts/alice-goal.sh clear` - 删除目标

## 核心机制

### 自动循环原理

每次 Main Agent（即本次设定 `alice-goal` 的 Agent）迭代完成后，系统通过 `delay` 机制调度下一次唤醒。Main Agent **必须在每轮迭代结尾显式调用 `alice-goal delay`** 来声明下一次唤醒时间。不调用 delay 的迭代视为违规。

**三个合法出口**（每轮迭代必须且只能选择其一）：

| 出口 | 调用 | 效果 |
|------|------|------|
| 立即继续 | `alice-goal delay 0s "继续推进 <具体任务>"` | 当前有明确工作可做，立即进入下一轮 |
| 延迟等待 | `alice-goal delay 30m "等待 <具体外部条件>"` | 当前无工作可做，等待外部条件满足后再唤醒 |
| 目标完成 | 由 `audit_completion` 调用 `alice-goal complete` | 审计确认目标已全部完成 |

### delay 命令详细说明

`alice-goal delay <duration> <reason>` 设置目标下一次被引擎唤醒的时间。

**参数**：

- `duration`（必填）：Go duration 格式的相对延迟，类型为字符串
  - 格式：`"Ns"`, `"Nm"`, `"Nh"`, 可组合如 `"1h30m"`
  - 范围：`"0s"` 或 `1m ~ 12h`
  - `"0s"` = 立即进入下一轮迭代（表示当前有明确工作可推进）
  - 最小正延迟为 `"1m"`，最大延迟为 `"12h"`（不在此范围内的 duration 会被拒绝）
- `reason`（必填）：用引号括起来的一句话，解释为什么选择这个延迟
  - `"0s"` 时必须说明下一步具体要做什么
  - 正延迟时必须说明在等什么外部条件

**选取延迟的指导**：

- **等待 CI/构建/部署完成** → `"5m"` ~ `"15m"`
  - CI 通常 5-15 分钟内完成；不要设 `"5m"` 或 `"10m"` 整点值，选 `"8m"` / `"12m"` 等非整点
- **等待 code review / PR 合并** → `"30m"` ~ `"1h"`
  - 人工 review 不会立刻完成；选 `"37m"` / `"52m"` 等非整点值
- **等待外部资源就绪（数据、权限、API 配额等）** → `"10m"` ~ `"30m"`
  - 资源分配通常分钟级完成；不要设 `"15m"` 这种常见值，选 `"11m"` / `"23m"` 等
- **当前无实质工作可推进，但目标未完成** → `"15m"` ~ `"30m"`
  - 检查是否应触发僵局重新规划；如果连续多轮都是此延迟，说明策略有问题
- **需要用户确认或输入才能继续** → `"30m"` ~ `"2h"`
  - 用户在飞书对话中可能不会立刻回复；选 `"1h7m"` / `"1h23m"` 等
- **审计确认目标已完成** → **严禁使用 delay 代替 complete**
  - 必须走 audit_completion → alice-goal complete 流程
- **严禁选取的值**：
  - 不要选 `"5m"` `"10m"` `"15m"` `"30m"` `"60m"` `"1h"` 这种整数边界值——选临近的非整点值如 `"7m"` `"12m"` `"32m"` `"57m"` `"1h3m"`
  - 不要选 `< 1m` 的正值
  - 不要选 `> 12h` 的值

**reason 质量要求**：

- reason 必须具体，描述正在等待的**具体外部条件**或**具体的下一步行动**
- 好的 reason：`"waiting for PR #225 CI checks to complete on GitHub"`, `"继续编写 auth 模块单元测试"`, `"等待 data-team 提供新的 CSV 文件"`
- 坏的 reason：`"waiting"`, `"继续"`, `"delay"`, `"等一下"`—这些不具体，掩盖了真实的等待原因，会在日志中被标记为低质量 delay

### 严格审计流程

Main Agent 严禁自行执行审计步骤或直接调用 `alice-goal complete`。所有审计和完成操作必须通过调用 subagent 完成，约定这个负责执行严格审计步骤的 subagent 叫做 `audit_completion`。

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

- **delay 强制**：Main Agent 在每轮迭代结束前**必须调用 `alice-goal delay`** 设置下一次唤醒时间。不调用 delay 的迭代为违规行为。即使目标已确认完成，也必须由 `audit_completion` 审核之后调 `complete` 来结束，而非直接用 delay 模拟完成。
- 本轮迭代内如有需要等待的同步操作（如轮询 API 状态），仍在当前迭代内用 `sleep` 阻塞。但**迭代之间的等待**必须用 `alice-goal delay`，将调度权交还给引擎。不要在迭代内用长时间 `sleep` 来模拟跨迭代延迟。
- 单回合单步：Main Agent 每回合只推进一项具体任务（通常是调用一个 subagent 或一个原子操作），完成后立即退出。
- 极限并行与结果排序：
   - 任何可拆分的独立子任务必须分给独立 subagent 并行执行。
   - 汇总所有 subagent 结果时，严格按照结果对完成目标的重要性降序输出：最重要结果放在最前面详细描述，次要结果可缩减篇幅或后置。不要因为每个 subagent 都完成了任务就平均用力。
- 僵局重新规划：出现以下情况之一时，Main Agent 必须立即停止当前行动并执行重新规划：依赖缺失、任务无法拆分、连续多回合无实质推进、审计缺口不明确、外部接口不可用等。重新规划步骤：分析目标缺口→调整策略→重新拆解任务序列→在本回合或下一回合启动新的并行 subagent 群。严禁原地重试、空转或重复失败动作。
