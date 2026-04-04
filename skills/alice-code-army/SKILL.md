---
name: alice-code-army
description: 以 Orchestrator-native 模式组织长期代码/研究协作。Claude Sonnet 4.6 直接作为编排者，调用 Opus 规划、Codex 执行、Sonnet 审阅，不依赖 Alice runtime 的模型调度。适用于多阶段、多子任务、多 repo 的长期并行推进。
---

# Alice Code Army — Orchestrator-Native Edition

## 设计原则

- **Claude Sonnet 4.6 是 Orchestrator**：直接在长生命周期 session（timeout 10 天）中运行，负责规划、调度、审阅与用户沟通。
- **模型调度由 Orchestrator 决定**：不依赖 Alice runtime 的 `config.yaml` profiles。
- **Campaign repo 是进度日志和灾难恢复检查点**：真正的上下文和知识在 Orchestrator 的 session 里，repo 是辅助记录层。
- **编排 Claude 直接说话即可发飞书**：其文字输出通过 `onThinking → sendAgentMessage` 自动推送飞书，不需要 alice-message。

## 角色分工

| 角色 | 模型 | 调用方式 |
|------|------|---------|
| **Orchestrator** | Claude Sonnet 4.6（我自己） | 长期 session，直接执行 |
| **Planner** | Claude Opus | `Agent` tool，`subagent_type=Plan, model=opus` |
| **Executor** | Codex GPT-5.4 medium | `codex:rescue` skill，`--model gpt-5.4-medium` |
| **Reviewer** | Claude Sonnet | Orchestrator 自身审阅，或 `Agent` tool |

## 何时使用

- 用户发 `#work` 消息，需要长期多阶段任务推进。
- 需要 Plan → Execute → Review 完整工作流。
- 任务预计超过单次对话，需要持续运行数小时到数天。
- 用户想让 Orchestrator 自主调度多个 agent，自己只做高层决策。

## 启动编排 Session

用户用 `#work` 触发后，和 Orchestrator 充分沟通，确认以下内容后再开始编排：

1. **目标（objective）**：要完成什么，成功标准是什么
2. **源代码仓库**：本地路径、远端、基线分支
3. **阶段划分**：大致几个阶段，每阶段的产出
4. **约束**：不能做什么、安全要求、资源限制

确认后，创建 campaign repo 并开始编排循环。

## 规划阶段的沟通守则（必须遵守）

**在进入执行之前，Orchestrator 必须对每一个 task 都做到心中有数。**

规划阶段（调用 Opus 出计划、与用户讨论）是自由沟通时间，**任何不清楚的地方都必须问用户**，直到满足以下所有条件才能结束规划、进入执行：

- [ ] 每个 task 的目标是什么，清楚
- [ ] 每个 task 具体要改哪个文件/模块/接口，清楚
- [ ] 每个 task 的成功标准是什么（怎么验证做完了），清楚
- [ ] task 之间的依赖顺序，清楚
- [ ] 有哪些技术风险或未知项，已和用户确认处理方式

**禁止"笼统开始"**：不能以"大方向清楚了就先跑起来"为由进入执行。只要有任何 task 还是模糊的，就继续问，直到具体。

**宁可多问，不要猜**：对用户意图有任何歧义，优先问清楚，不要自行假设。假设错了的代价远高于多问一句的成本。

## Campaign Repo 结构

```
<campaign-repo>/
├── README.md               # 入场说明和阅读顺序
├── campaign.md             # 总目标、gate、约束、当前结论
├── plan.md                 # 当前执行计划（Opus 产出，Orchestrator 维护）
├── phases/
│   └── P01/
│       ├── phase.md        # 阶段目标和当前状态
│       └── tasks/
│           └── T001/
│               ├── task.md       # 任务元数据
│               ├── progress.md   # 执行日志
│               ├── results/      # 结果文件
│               └── reviews/      # 审阅记录
├── checkpoints/            # 灾难恢复检查点
├── repos/                  # 源代码仓库引用
│   └── <repo-id>.md
└── reports/
    └── live-report.md      # 实时进度报告
```

task.md frontmatter 的 `status` 字段只需：`pending` / `executing` / `review_pending` / `done` / `failed`

### 写入时机（保证可恢复）

- **执行前**：写 `task.md status=executing`（意图写入）
- **执行后**：写 `task.md status=review_pending` + `progress.md` + `results/`
- **审阅后**：写 `reviews/Rxxx.md` + 更新 `task.md status=done/failed`
- **每阶段完成**：更新 `phase.md` + 写 `checkpoints/checkpoint-{timestamp}.md`

## 编排循环

每轮循环的标准步骤：

```
1. 检查收件箱（cat ~/.alice/codearmy/{campaign_id}/control.md）
2. 如有控制命令，先执行
3. 确认下一批 ready tasks（status=pending，依赖满足）
4. 对每个 ready task：
   a. 写 task.md status=executing
   b. 调 codex:rescue 执行（--wait 阻塞等待）
   c. 写执行结果到 progress.md / results/
   d. Orchestrator 自审或调 Agent 做 review
   e. 写 reviews/Rxxx.md，更新 task status
5. 写 checkpoints/checkpoint-{timestamp}.md
6. 更新 live-report.md
7. 直接输出阶段性进度（自动推送飞书）
8. 回到步骤 1
```

### 检查收件箱

在每次 `codex:rescue` 或 `Agent` 工具**返回后**，主动执行：

```bash
cat ~/.alice/codearmy/{campaign_id}/control.md 2>/dev/null && \
  rm -f ~/.alice/codearmy/{campaign_id}/control.md
```

如果文件存在，解析并执行其中的命令，然后继续。

### 收件箱格式

```markdown
---
command: pause|resume|abort|replan
message: "附加说明（可选）"
---
```

用户通过交互 session 向编排 Claude 发命令：

```bash
mkdir -p ~/.alice/codearmy/<campaign_id>
cat > ~/.alice/codearmy/<campaign_id>/control.md << 'EOF'
---
command: pause
message: 等我审阅 P01 的结果
---
EOF
```

## 调用 Codex 执行任务

使用 `codex:rescue` skill，指定 `gpt-5.4-medium`：

```
Skill: codex:rescue
Args: --wait --write --model gpt-5.4-medium --effort medium <任务描述>
```

- `--wait`：阻塞直到 Codex 完成（编排 Claude 挂起，不消耗 token）
- `--write`：允许 Codex 写文件
- `--model gpt-5.4-medium`：廉价执行模型
- 高质量 review 时改用 `gpt-5.4` + `--effort high`

## 调用 Opus 规划

```python
Agent(subagent_type="Plan", model="opus", prompt="...")
```

提供完整的背景、目标、约束、现有代码结构，让 Opus 产出 `plan.md` 的内容。

## 进度通知飞书

**直接输出文字即可**。编排 Claude 的 assistant text 自动经 `onThinking → sendAgentMessage` 推送飞书。

需要发送图片/文件时，才使用 `alice-message` skill。

示例输出：
```
✅ Phase P01 完成：3 个 task 全部 done，详见 reports/live-report.md
⏭️ 开始 Phase P02：T004 T005 T006 已就绪，调度 Codex 执行中...
⚠️ T003 执行失败，原因：<xxx>，需要人工介入，已暂停。
```

## 用户交互

### 查询进度（Fork Session）

用户想了解编排细节（不打断编排进程）：

```bash
# Orchestrator 启动时输出的 session_id
SESSION_FILE="$HOME/.claude/projects/<project_hash>/<session_id>.jsonl"
FORK_ID=$(python3 -c "import uuid; print(uuid.uuid4())")
cp "$SESSION_FILE" "$HOME/.claude/projects/<project_hash>/${FORK_ID}.jsonl"
claude --resume "$FORK_ID"
```

Fork 继承完整上下文，可自由问答，不影响编排进程。

### 发控制命令

写入收件箱文件（见"检查收件箱"节）。

### 普通消息

用户发普通消息（非 `#work`），Alice 起新的交互 Claude 处理，不打断编排 session。

## 灾难恢复

Alice 重启或编排进程中断时：

1. `claude --resume <session_id>`：恢复完整对话历史
2. 读 campaign repo，确认各 task 状态：
   - `executing` → 视为中断，重新执行
   - `review_pending` → 重新审阅
   - `done` → 跳过
3. 读最新 `checkpoints/` 文件，确认断点，从那里续跑

### Checkpoint 格式

```markdown
# Checkpoint {timestamp}

## 当前状态
- phase: P01
- completed_tasks: [T001, T002]
- interrupted_tasks: [T003]
- next_tasks: [T004, T005]

## 最后完成动作
- task: T002, commit: abc123

## 恢复指令
确认 T003 状态；如未完成则重跑，然后继续 T004/T005。
```

## 定时唤醒（跨天 Campaign）

短任务（< 10 天）无需定时，单 session 完成。

跨天长任务，每个 Phase 结束时用 alice-scheduler 创建下一 Phase 的唤醒任务：

```
alice-scheduler create: {
  "title": "codearmy-wakeup-P02",
  "schedule": {"type": "one_shot", "run_at": "<时间>"},
  "action": {
    "type": "run_llm",
    "profile": "work",
    "prompt": "继续 campaign <id> 的 Phase P02，先读 campaign repo 状态，再开始执行。"
  }
}
```

## 维护约束

当前会话里 `.agents/skills/...` 的已安装 skill 副本来自 Alice 安装/更新流程，不应直接修改；需要变更 skill 时，应修改 Alice 仓库里的 `alice/skills/...` 源文件，再通过安装流程同步进去。
