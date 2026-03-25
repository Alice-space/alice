---
name: alice-code-army
description: 以 campaign 仓库为主事实源，结合 Alice 调度、多模型执行与审阅，组织长期代码/研究协作。适用于多阶段、多子任务、多 repo 的并行推进，以及 repo-native 的计划、进度、评审与报告收敛。
---

# Alice 代码军队

`alice-code-army` 现在采用 repo-first 约定：

- `campaign repo` 做主事实源：计划、阶段、任务、评审、报告都以仓库里的 markdown/frontmatter 为主。
- `Alice runtime` 做轻量索引层：记录当前会话绑定哪个 campaign、当前 scheduler task、当前运行态。
- `source repos` 做真实代码变更面：task 改的代码仍落在原始代码仓库，而不是复制到 campaign repo。
- `GitLab issue / MR` 做可选镜像面：需要对人可见时再同步，不再是默认主存储。
- Alice backend 会周期扫描 `campaign repo`：自动刷新 `reports/live-report.md`，并把 task 的 `wake_at` / `wake_prompt` 同步成真正的 automation wake task。
- 当前 runtime 自动化只覆盖 executor/reviewer dispatch、review verdict 应用、wake task 和 reconcile；plan 阶段的 proposal / merge / human gate 仍以 repo 文件和人工/交互式流程为主，尚未有独立 planner runtime 调度。

优先使用 `scripts/alice-code-army.sh` 管理当前会话下的 campaign。脚本会自动使用 Alice 注入的当前 thread/session 上下文和 runtime HTTP API。

维护约束：当前会话里 `.agents/skills/...` 的已安装 skill 副本来自 Alice 安装/更新流程，不应直接修改；需要变更 skill 时，应修改 Alice 仓库里的 `alice/skills/...` 源文件，再通过安装流程同步进去。

## 何时使用

- 用户要做长期优化、研究协作、并行多 task、多 repo 任务推进。
- 用户要把计划、任务、评审、报告沉淀在一个 campaign repo 里。
- 用户希望让强模型做规划/审阅，让小模型做子任务执行。
- 用户希望长任务能自动唤醒、周期汇报、持续收敛。

## 主数据面

默认约定：

- `campaign.md`：总目标、gate、方向、阶段、当前结论。
- `plans/`：多模型 proposal、人类意见、merged plan。
- `phases/Pxx/phase.md`：阶段定义。
- `phases/Pxx/tasks/Txxx.md`：任务定义、依赖、状态、write scope、唤醒信息。
- `reviews/`：审阅记录。
- `reports/`：live report、phase report、final report。
- `paper/`：论文或最终文档。
- `repos/*.md`：source repo 引用信息，包括本地路径、远端、默认分支、工作分支。
- `docs/research-contract.md` / `findings.md` / `EXPERIMENT_LOG.md`：研究约束、关键发现、实验日志。

## Task 约定

一个 task 不只是单个 markdown，而是一个小工作包。常见内容包括：

- `task.md`：任务元数据与目标
- `plan.md`：执行方案
- `progress.md`：执行日志
- `review/*.md`：审阅记录
- `results/*.md`：结果摘要、指标、产物路径
- `scripts/`：task 专属小脚本

真实业务代码继续放在 source repo。大数据和大产物只记录路径、checksum、摘要，不默认进 git。

task frontmatter 现在默认带两类角色：

- `executor`：默认 `executor.codex`
- `reviewer`：默认 `reviewer.claude`

并带这类执行态字段：

- `dispatch_state`
- `review_status`
- `execution_round`
- `review_round`
- `base_commit`
- `head_commit`
- `last_run_path`
- `last_review_path`

## 并行与防冲突

并行的基本规则：

- 同一个 task 必须有独立 `owner_agent`、`lease_until`、`working_branch`、`write_scope`。
- 同一个 source repo 可以并行多个 task，但只有在 `write_scope` 不重叠时才允许。
- 同文件或同模块的重叠改动，不应并行；要么改成依赖链，要么合并成一个 task。
- 开发分支可以并行，回主线集成应串行。

长任务约定：

- task 可以写 `wake_at` / `wake_prompt`
- reconcile 读到后，应通过 `alice-scheduler` 创建或更新唤醒任务
- 到时间后自动恢复该 task，而不是靠人工记忆

## 核心流程

1. 新建 campaign，并直接 scaffold 一个 campaign repo。
2. 通过人工或交互式会话让 `Claude Code` / `GPT` / `Gemini Code` 分别产出 proposal，同时保留人工输入文件。
3. 人工合并 proposal，生成 merged plan。
4. 按 merged plan 展开阶段和 task 文件树。
5. runtime reconcile 只派发依赖满足、write scope 不冲突的 ready tasks。
6. Alice judge 先读取 review 文件并把 verdict 回写成 `accepted / rework / blocked / rejected`。
7. executor 在 source repo 分支/worktree 上执行，并把 task 状态推进到 `review_pending / waiting_external / blocked`。
8. reviewer 只写 `reviews/Txxx/Rxxx.md`，不直接改 source repo。
9. curator / reporter 汇总为 live report、phase report、paper。
10. Alice 后台 system task 会持续做 repo reconcile、executor/reviewer dispatch 和 wake task 同步，不需要每次都靠人工重跑一遍脚本。

## 角色边界

- `Alice`：orchestrator / judge / integrator
- `Codex executor`：执行 task，可写 source repo 和 task 目录
- `Claude reviewer`：外部审阅者，只写 review 文件和审阅结论

硬规则：

- reviewer 不直接改 source repo
- executor 不直接裁决自己是否通过
- task 进入 `review_pending` 后，review round 由 Alice 派发 reviewer
- review verdict 由 Alice judge 应用到 task frontmatter

## 常用命令

- 列出当前 thread 下的 campaign：
  `scripts/alice-code-army.sh list`
- 新建 campaign，并默认直接 scaffold campaign repo：
  `scripts/alice-code-army.sh create <<'JSON'`
  `{ "title": "Detector Scan", "objective": "完成 repo-native 的多阶段研究协作", "repo": "group/source-repo", "campaign_repo_path": "./campaigns/detector-scan", "max_parallel_trials": 6 }`
  `JSON`
- 为已有 campaign 手动初始化或补建 campaign repo：
  `scripts/alice-code-army.sh init-repo camp_xxx ./campaigns/detector-scan`
- 扫描 campaign repo，查看当前 ready / blocked / wake 状态：
  `scripts/alice-code-army.sh repo-scan camp_xxx`
- 手动触发一次 repo reconcile，并刷新 live report / runtime summary：
  `scripts/alice-code-army.sh repo-reconcile camp_xxx`
- 查看单个 campaign：
  `scripts/alice-code-army.sh get camp_xxx`
- Patch campaign：
  `scripts/alice-code-army.sh patch camp_xxx '{"summary":"direction updated"}'`
- 新增或更新 trial：
  `scripts/alice-code-army.sh upsert-trial camp_xxx '{"trial":{"id":"trial-1","title":"repo-a baseline","status":"running"}}'`
- 追加 guidance：
  `scripts/alice-code-army.sh add-guidance camp_xxx '{"guidance":{"source":"feishu","command":"/alice hold"}}'`
- 追加 review：
  `scripts/alice-code-army.sh add-review camp_xxx '{"review":{"reviewer_id":"repo-reviewer","verdict":"concern","summary":"write scope overlaps with T012"}}'`
- 追加 pitfall：
  `scripts/alice-code-army.sh add-pitfall camp_xxx '{"pitfall":{"summary":"two tasks touched same module","related_trial_id":"trial-2"}}'`
- 应用一条 `/alice ...` 指令：
  `scripts/alice-code-army.sh apply-command camp_xxx '/alice hold' feishu`

## GitLab 镜像

GitLab 现在是可选镜像，不是主事实源。

- 若用户需要 issue / MR 对人可见，可手动使用：
  `render-issue-note`
  `render-trial-note`
  `sync-issue`
  `sync-trial`
  `sync-all`
- 若没有 issue / MR，不再视为默认阻塞项。
- 若 issue 已存在，仍可把 campaign repo 摘要镜像到 issue / MR，但不要把它当唯一状态源。

## 自动化与回复模式

- reconcile / heartbeat 默认应该推进任务，而不是只汇报。
- 对做不到或继续做不安全的动作，仍应发 `/alice needs-human ...`。
- 回复中优先说明：
  `campaign repo`
  `当前阶段`
  `活跃 tasks`
  `阻塞项`
  `下一步`
- 当用户要求看报告时，优先更新 `reports/live-report.md` 或相应阶段报告，而不是只口头描述。
