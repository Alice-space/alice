---
name: alice-code-army
description: 以 Orchestrator-native 模式组织长期代码/研究协作。Claude Sonnet 4.6 直接作为编排者，调用 Opus 规划、Codex 审阅计划与执行、Sonnet 审阅代码，不依赖 Alice runtime 的模型调度。适用于多阶段、多子任务、多 repo 的长期并行推进。
---

# Alice Code Army — Orchestrator-Native Edition

## 设计原则

- **Orchestrator 只做统筹，不做具体工作**：维护进展状态表、调度 subagent、和用户对话。自己的上下文专用于高层决策，不沉溺于执行细节。
- **Campaign repo 是所有 subagent 的共享通信总线**：subagent 将产出写入 campaign repo，Orchestrator 读取 repo 了解进展，不直接接受 subagent 大段汇报。
- **编排 Claude 直接说话即可发飞书**：文字输出通过 `onThinking → sendAgentMessage` 自动推送飞书，不需要 alice-message。
- **统一术语**：维护 `glossary.md`，所有角色对相同事物使用相同称呼。

## 角色分工

| 角色 | 模型 | 调用方式 | 职责 |
|------|------|---------|------|
| **Orchestrator** | Claude Sonnet 4.6（我自己） | 长期 session | 统筹调度、用户对话、维护进展状态表 |
| **Planner** | Claude Opus 4.6 | `Agent(subagent_type="Plan", model="opus")` | 将目标细化为 phase / task，输出可执行计划 |
| **Planner_Reviewer** | Codex GPT-5.4 xhigh | `codex:rescue --effort xhigh` | 审阅计划，给出三路判决 |
| **Executor** | Codex GPT-5.4 medium | `codex:rescue --effort medium` | 执行具体 task，写入 progress.md 和 results/ |
| **Reviewer** | Claude Sonnet 4.6 | `Agent(model="sonnet")` | Task 级代码/结果审阅，写入 reviews/Rxxx.md |

## 何时使用

- 用户发 `#work` 消息，需要长期多阶段任务推进。
- 需要 Plan → Review Plan → Execute → Review 完整工作流。
- 任务预计超过单次对话，需要持续运行数小时到数天。
- 用户想让 Orchestrator 自主调度多个 agent，自己只做高层决策。

---

## 完整运行流程

### 阶段 0：充分理解用户需求

用户发布任务后，**Orchestrator 不得立即开始执行**，必须先确保完全理解用户意图。

**追问直到满足所有条件**：
- [ ] 总目标是什么，成功标准是什么
- [ ] 源代码仓库：本地路径、基线分支
- [ ] 有哪些硬约束（不能做什么、安全要求、资源限制）
- [ ] 大致几个阶段，每阶段的产出是什么

**禁止"笼统开始"**：不能以"大方向清楚了就先跑起来"为由进入执行。有任何不清楚的地方必须追问，宁可多问，不要猜。

### 阶段 1：初始化 Campaign Repo

确认需求后，运行初始化脚本创建 campaign repo：

```bash
bash ~/.alice/skills/alice-code-army/scripts/alice-code-army.sh init \
  --id <campaign_id> \
  --title "<标题>" \
  --objective "<总目标>" \
  --repo "<源码仓库路径>"
```

写入内容：
- `campaign.md`：总目标、约束、当前阶段
- `glossary.md`：初始术语表（后续各角色补充）
- `reports/live-report.md`：进展状态表（Orchestrator 持续维护）

### 阶段 2：规划（Planner → Planner_Reviewer 循环）

**调用 Planner：**

```python
Agent(
    subagent_type="Plan",
    model="opus",
    prompt="""
你是 Planner，请为以下 campaign 制定完整执行计划。

Campaign 目标：<objective>
源码仓库：<repo_path>
约束：<constraints>
Campaign repo：<campaign_repo_path>
术语表：<campaign_repo_path>/glossary.md

请：
1. 阅读源码仓库了解现状
2. 将目标分解为 Phase（不能并行的分 phase）和 Task（能并行的放同一 phase）
3. 每个 task 必须包含：目标、要修改的文件/模块、验收标准、产物路径
4. 将计划写入 <campaign_repo_path>/plan.md 和 phases/ 目录
5. 若发现术语歧义，补充 glossary.md
"""
)
```

**调用 Planner_Reviewer：**

```
Skill: codex:rescue
Args: --wait --effort xhigh

提示词：
你是 Planner_Reviewer，请审阅 <campaign_repo_path>/plan.md。

给出三路判决之一，写入 <campaign_repo_path>/plans/reviews/PR001.md：
- approve：计划完整可执行，进入执行阶段
- rework：计划有问题，需 Planner 针对审稿意见修改（写明具体问题）
- needs_human：存在需要人类决策的问题（写明需要人类确认什么）
```

**Orchestrator 根据判决行动**：

| 判决 | 行动 |
|------|------|
| `approve` | 进入执行阶段 |
| `rework` | 新上下文再次调用 Planner，传入审稿意见，让其针对性修改；再次调用 Planner_Reviewer |
| `needs_human` | 向用户输出问题总结，暂停等待指令 |

### 阶段 3：执行循环（Task 级）

计划通过后，按 plan.md 中的 phase 顺序执行。

**每个 Phase：**
1. 拉起当前 phase 下所有 `status=pending` 的 task（并行调用多个 Executor）
2. 等所有 task 完成（`--wait` 阻塞）
3. 扫描是否有 blocked task，有则先处理（见"处理 Blocked Task"）
4. 对每个完成的 task 调用 Reviewer 做 task 级审阅
5. 审阅 approve → 标记 `status=done`；rework → 重新调用 Executor
6. 当前 phase 所有 task 全 done → 更新 `phase.md`，写 checkpoint

**调用 Executor（每个 task 独立调用）：**

```
Skill: codex:rescue
Args: --wait --write --effort medium

提示词：
你是 Executor，请执行以下 task：
- Task 文件：<campaign_repo_path>/phases/P01/tasks/T001/task.md
- Campaign 目标：<objective>
- 术语表：<campaign_repo_path>/glossary.md

执行步骤：
1. 读 task.md 了解目标、范围、验收标准
2. 执行工作
3. 将结果写入：
   - progress.md（严格按格式规范，见 SKILL.md "progress.md 格式规范"）
   - results/（实际产物：代码 patch、测试输出等）
4. 执行完毕将 progress.md 中 status 改为 done / blocked / failed
```

**调用 Reviewer（每个 task 完成后立即调用）：**

```python
Agent(
    model="sonnet",
    prompt="""
你是 Reviewer，请审阅以下 task 的执行结果：
- Task 文件：<campaign_repo_path>/phases/P01/tasks/T001/task.md
- 执行记录：<campaign_repo_path>/phases/P01/tasks/T001/progress.md
- 产物目录：<campaign_repo_path>/phases/P01/tasks/T001/results/
- 术语表：<campaign_repo_path>/glossary.md

对照 task.md 中的验收标准逐条审阅，将审阅记录写入：
<campaign_repo_path>/phases/P01/tasks/T001/reviews/R001.md

判决（写入 reviews/R001.md frontmatter verdict 字段）：
- approve：全部验收标准通过
- rework：有问题需要修改（写明具体问题和修改建议）
"""
)
```

### 阶段 4：验收（最终 Planner 验收）

所有 phase 全部完成后，调用 Planner 做最终目标验收：

```python
Agent(
    subagent_type="Plan",
    model="opus",
    prompt="""
请阅读以下内容，判断 campaign 总目标是否已达成：
- Campaign 目标：<campaign_repo_path>/campaign.md
- 所有 task 完成记录：<campaign_repo_path>/phases/
- 所有 review 记录：各 task 的 reviews/ 目录

如果目标已达成：在 campaign.md 中写入 status=completed，输出总结报告到 reports/final-report.md。
如果目标未达成：写明差距，提出下一轮计划方向，写入 plan.md，Orchestrator 将发起新一轮规划循环。
"""
)
```

---

## 处理 Blocked Task

Orchestrator 在每批 Executor 返回后扫描阻塞任务：

```bash
grep -rl "status: blocked" <campaign_repo_path>/phases/*/tasks/*/progress.md
```

对每个 blocked task：
1. 读取 `progress.md` 中的"阻塞信息"
2. 判断是否能自行解决（如缺少配置、路径问题等简单问题）
   - **能解决**：修复后重新调用 Executor（新上下文，附说明）
   - **不能解决**：向用户汇报，暂停等待指令

---

## Campaign Repo 结构

```
<campaign-repo>/
├── README.md               # 入场说明和阅读顺序
├── campaign.md             # 总目标、约束、当前 phase、status
├── glossary.md             # 统一术语表（所有角色共用，发现歧义时补充）
├── plan.md                 # 当前执行计划（Planner 产出）
├── plans/
│   └── reviews/            # Planner_Reviewer 审阅记录 PR001.md, PR002.md...
├── phases/
│   └── P01/
│       ├── phase.md        # 阶段目标、依赖、当前 status
│       └── tasks/
│           └── T001/
│               ├── task.md       # 任务元数据（含 status）
│               ├── progress.md   # Executor 执行记录（见格式规范）
│               ├── results/      # 实际产物（代码 patch、测试输出等）
│               └── reviews/      # Reviewer 审阅记录 R001.md, R002.md...
├── checkpoints/            # 每 phase 完成后写入的灾难恢复检查点
├── repos/                  # 源代码仓库引用
│   └── <repo-id>.md
└── reports/
    ├── live-report.md      # Orchestrator 维护的实时进展状态表
    └── final-report.md     # 最终验收报告
```

### task.md status 字段

`pending` → `executing` → `review_pending` → `done` / `failed` / `blocked`

### 写入时机（保证可恢复）

| 时机 | 写入内容 |
|------|---------|
| 调用 Executor 前 | `task.md status=executing` |
| Executor 完成后 | `progress.md`（含 status）+ `results/` |
| Reviewer 完成后 | `reviews/Rxxx.md` + `task.md status=done/failed` |
| 每 phase 完成后 | `phase.md` + `checkpoints/checkpoint-{timestamp}.md` |

---

## progress.md 格式规范

Executor 必须严格按此格式写入，Orchestrator 和 Reviewer 依赖此格式读取状态：

```markdown
---
task_id: T001
status: done          # executing | done | blocked | failed
executor_model: gpt-5.4-medium
started_at: 2026-04-04T10:00:00Z
completed_at: 2026-04-04T10:32:00Z
---

## 执行摘要
1-3 句话：做了什么、结果是什么。

## 变更清单
- `src/foo.py` — 新增 `bar()` 函数，处理 XYZ 逻辑
- `tests/test_foo.py` — 新增 3 条单元测试

## 验收自检
- [x] 单测全部通过（输出见 results/test-output.txt）
- [ ] 集成测试未覆盖（超出本任务范围）

## 阻塞信息（仅 status=blocked 时填写）
- 原因: 缺少 API Key，无法访问外部服务
- 需要: 用户提供 OPENAI_API_KEY 或确认 mock 方案

## 错误信息（仅 status=failed 时填写）
- <错误描述及堆栈摘要>
```

---

## Orchestrator 进展状态表

Orchestrator 在 `reports/live-report.md` 维护实时状态：

```markdown
# Live Report — <campaign_id>
更新时间：<timestamp>

## 总体进度
- 当前 Phase：P01
- 已完成 Task：T001, T002
- 执行中 Task：T003, T004
- 阻塞 Task：T005（等待用户提供 API Key）
- 待执行 Task：T006, T007

## Phase 状态
| Phase | 状态 | Task 总数 | 完成 | 阻塞 |
|-------|------|----------|------|------|
| P01   | executing | 5 | 2 | 1 |
| P02   | pending | 3 | 0 | 0 |
```

---

## 编排循环伪代码

```
LOOP:
  1. 检查收件箱（见"用户控制命令"），有命令先执行
  2. 扫描当前 phase 的 blocked tasks，处理或上报用户
  3. 找出 status=pending 且依赖满足的 tasks
  4. 并行对每个 ready task：
     a. 写 task.md status=executing
     b. 调用 Executor（--wait 阻塞）
     c. 读 progress.md 确认 status
     d. 调用 Reviewer（--wait 阻塞）
     e. 读 reviews/Rxxx.md verdict：
        approve → task.md status=done
        rework  → 新上下文重调 Executor（附审阅意见）
  5. 当前 phase 所有 task done → 写 checkpoint，更新 live-report.md
  6. 输出进度（自动推送飞书）
  7. 还有下一 phase → 继续 LOOP
  8. 所有 phase 完成 → 调用 Planner 做目标验收
     验收通过 → 结束
     未通过   → 根据 Planner 建议发起新一轮 LOOP
```

---

## 用户控制命令

每次 `codex:rescue` 或 `Agent` 返回后，检查收件箱：

```bash
cat ~/.alice/codearmy/{campaign_id}/control.md 2>/dev/null && \
  rm -f ~/.alice/codearmy/{campaign_id}/control.md
```

收件箱格式：

```markdown
---
command: pause|resume|abort|replan
message: "附加说明（可选）"
---
```

用户写入方式：

```bash
mkdir -p ~/.alice/codearmy/<campaign_id>
cat > ~/.alice/codearmy/<campaign_id>/control.md << 'EOF'
---
command: pause
message: 等我审阅 P01 的结果
---
EOF
```

---

## 进度通知飞书

**直接输出文字即可**，自动经 `onThinking → sendAgentMessage` 推送飞书。

```
✅ Phase P01 完成：T001 T002 T003 全部 done
⏭️ 开始 Phase P02：T004 T005 T006 已就绪，调度 Executor 中...
🚧 T005 blocked：缺少 OPENAI_API_KEY，已暂停等待用户指令
⚠️ T003 Reviewer 返工：接口签名不符合验收标准，重新调度 Executor
```

发送图片/文件时才用 `alice-message` skill。

---

## 灾难恢复

Alice 重启或编排进程中断：

1. `claude --resume <session_id>`：恢复完整对话历史
2. 读 `reports/live-report.md` 和各 `task.md` 确认状态：
   - `executing` → 视为中断，重新执行
   - `review_pending` → 重新审阅
   - `blocked` → 按阻塞信息处理
   - `done` → 跳过
3. 读最新 `checkpoints/` 文件，从断点续跑

### Checkpoint 格式

```markdown
# Checkpoint {timestamp}

## 当前状态
- phase: P01
- completed_tasks: [T001, T002]
- blocked_tasks: [T003]
- next_tasks: [T004, T005]

## 最后完成动作
- task: T002, reviewer: approve

## 恢复指令
检查 T003 阻塞原因；如已解除则重跑，然后继续 T004/T005。
```

---

## 查询进度（Fork Session）

不打断编排进程，fork 一个只读 session：

```bash
SESSION_FILE="$HOME/.claude/projects/<project_hash>/<session_id>.jsonl"
FORK_ID=$(python3 -c "import uuid; print(uuid.uuid4())")
cp "$SESSION_FILE" "$HOME/.claude/projects/<project_hash>/${FORK_ID}.jsonl"
claude --resume "$FORK_ID"
```

---

## 维护约束

当前会话里 `.agents/skills/...` 的已安装 skill 副本来自 Alice 安装/更新流程，不应直接修改；需要变更 skill 时，应修改 Alice 仓库里的 `alice/skills/...` 源文件，再通过安装流程同步进去。
