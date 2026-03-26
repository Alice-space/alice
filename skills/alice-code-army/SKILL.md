---
name: alice-code-army
description: 以 campaign 仓库为主事实源，结合 Alice 调度、多模型执行与审阅，组织长期代码/研究协作。适用于多阶段、多子任务、多 repo 的并行推进，以及 repo-native 的计划、进度、评审与报告收敛。
---

# Alice 代码军队

`alice-code-army` 现在采用 repo-first 约定：

- `campaign repo` 做主事实源：计划、阶段、任务、评审、报告都以仓库里的 markdown/frontmatter 为主。
- `Alice runtime` 做轻量索引层：记录当前会话绑定哪个 campaign、当前 scheduler task、当前运行态。
- `source repos` 做真实代码变更面：task 改的代码仍落在原始代码仓库，而不是复制到 campaign repo。
- Alice backend 会周期扫描 `campaign repo`：自动刷新 `reports/live-report.md`，并把 task 的 `wake_at` / `wake_prompt` 同步成真正的 automation wake task。
- Alice runtime 会按角色调度 planner / planner reviewer / executor / reviewer workflow，并处理 review verdict、wake task 和 reconcile；具体 provider / model / profile 默认由 runtime 配置决定，也可以在 campaign/task frontmatter 里覆盖。proposal merge 和 human approval 仍需要 repo 文件与人工 gate 配合。

运行时优先直接使用 `$HOME/.agents/skills/alice-code-army/scripts/alice-code-army.sh` 管理当前会话下的 campaign。脚本会自动使用 Alice 注入的当前 thread/session 上下文和 runtime HTTP API。

只有在你已经进入当前 skill 目录时，才把它简写成相对路径 `scripts/alice-code-army.sh`。不要把它写成 `alice/scripts/alice-code-army.sh`，也不要假设 release 二进制运行时存在 Alice 源码仓。

维护约束：当前会话里 `.agents/skills/...` 的已安装 skill 副本来自 Alice 安装/更新流程，不应直接修改；需要变更 skill 时，应修改 Alice 仓库里的 `alice/skills/...` 源文件，再通过安装流程同步进去。

## 何时使用

- 用户要做长期优化、研究协作、并行多 task、多 repo 任务推进。
- 用户要把计划、任务、评审、报告沉淀在一个 campaign repo 里。
- 用户希望按角色把 planner / reviewer / executor 交给 Alice runtime 调度，而不是把模型写死在模板里。
- 用户希望长任务能自动唤醒、周期汇报、持续收敛。

## 主数据面

默认约定：

- `README.md`：给新 agent 的入场说明和建议阅读顺序。
- `campaign.md`：总目标、gate、方向、阶段、当前结论。
- `plans/`：多模型 proposal、人类意见、merged plan。
- `phases/Pxx/phase.md`：阶段定义。
- `phases/Pxx/tasks/Txxx/`：完整 task package，里面有 `task.md`、`context.md`、`plan.md`、`progress.md`、`results/`、`reviews/`。
- `reports/`：live report、phase report、final report。
- `paper/`：论文或最终文档。
- `repos/*.md`：source repo 引用信息，包括本地路径、远端、默认分支、工作分支。
- `docs/research-contract.md` / `findings.md` / `EXPERIMENT_LOG.md`：研究约束、关键发现、实验日志。

## Task 约定

一个 task 不只是单个 markdown，而是一个小工作包。常见内容包括：

- `task.md`：任务元数据与目标
- `context.md`：背景、repo、关键文件、依赖
- `plan.md`：执行方案
- `progress.md`：执行日志
- `reviews/*.md`：task-local 审阅记录
- `results/*.md`：结果摘要、指标、产物路径
- `scripts/`：task 专属小脚本

真实业务代码继续放在 source repo。大数据和大产物只记录路径、checksum、摘要，不默认进 git。

task frontmatter 现在默认带两类角色：

- `executor`：默认 `executor`
- `reviewer`：默认 `reviewer`

这些 role 是角色名，不是模型名。若 `provider` / `model` / `profile` 留空，Alice runtime 会按当前 runtime 配置选择实际执行后端。

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
- 到时间后 runtime 会先把 task 明确恢复到 `executing`，再继续该 task 的 workflow

## 核心流程

1. 新建 campaign，并直接 scaffold 一个 campaign repo。
2. 先补齐 objective、source repos、研究约束等 baseline facts，再由 Alice runtime 按角色派发 planner / planner reviewer workflow。
3. 人工合并 proposal，生成 merged plan。
4. 按 merged plan 展开阶段和 refined task package 文件树。
5. runtime reconcile 只派发依赖满足、write scope 不冲突的 ready tasks。
6. Alice judge 先读取 review 文件并把 verdict 回写成 `accepted / rework / blocked / rejected`。
7. executor 在 source repo 分支/worktree 上执行，并把 task 状态推进到 `review_pending / waiting_external / blocked`。
8. reviewer 只写 task-local `phases/Pxx/tasks/Txxx/reviews/Rxxx.md`，不直接改 source repo。
9. curator / reporter 汇总为 live report、phase report、paper。
10. Alice 后台 system task 会持续做 repo reconcile、planner/planner reviewer/executor/reviewer dispatch 和 wake task 同步，不需要每次都靠人工重跑一遍脚本。

## 角色边界

- `Alice`：orchestrator / judge / integrator
- `Planner`：产出 proposal 和 draft tasks；具体 provider / model 由 Alice runtime 调度
- `Planner Reviewer`：审阅 proposal 和 draft task 切分；具体 provider / model 由 Alice runtime 调度
- `Executor`：执行 task，可写 source repo 和 task 目录；具体 provider / model 由 Alice runtime 调度
- `Reviewer`：外部审阅者，只写 review 文件和审阅结论；具体 provider / model 由 Alice runtime 调度

硬规则：

- reviewer 不直接改 source repo
- executor 不直接裁决自己是否通过
- task 进入 `review_pending` 后，review round 由 Alice 派发 reviewer
- review verdict 由 Alice judge 应用到 task frontmatter

## 常用命令

- 建议先定义脚本入口：
  `CODE_ARMY_SH="$HOME/.agents/skills/alice-code-army/scripts/alice-code-army.sh"`
- 列出当前 thread 下的 campaign：
  `"$CODE_ARMY_SH" list`
- 删除一个 campaign：
  `"$CODE_ARMY_SH" delete camp_xxx`
- 删除一个 campaign，并顺手删除本地 campaign repo：
  `"$CODE_ARMY_SH" delete camp_xxx --delete-repo`
- 新建 campaign，并默认直接 scaffold campaign repo：
  `"$CODE_ARMY_SH" create <<'JSON'`
  `{ "title": "Detector Scan", "objective": "完成 repo-native 的多阶段研究协作", "repo": "group/source-repo", "campaign_repo_path": "./campaigns/detector-scan", "max_parallel_trials": 6 }`
  `JSON`
- 安全启动一个 campaign：create + baseline facts + repo-reconcile，一次性拉起正式 planner dispatch：
  `"$CODE_ARMY_SH" bootstrap <<'JSON'`
  `{ "title": "Detector Scan", "objective": "完成 repo-native 的多阶段研究协作", "repo": "group/source-repo", "campaign_repo_path": "./campaigns/detector-scan", "max_parallel_trials": 6, "source_repos": [ { "repo_id": "repo-a", "local_path": "/path/to/repo-a" } ], "research_contract": { "constraints": ["先完成 planning，再进入执行"] } }`
  `JSON`
- 为已有 campaign 手动初始化或补建 campaign repo：
  `"$CODE_ARMY_SH" init-repo camp_xxx ./campaigns/detector-scan`
- 扫描 campaign repo，查看当前 ready / blocked / wake 状态：
  `"$CODE_ARMY_SH" repo-scan camp_xxx`
- 校验 campaign repo 是否满足 repo-first contract：
  `"$CODE_ARMY_SH" repo-lint camp_xxx`
- 在人类批准前执行更严格的 gate 校验：
  `"$CODE_ARMY_SH" repo-lint camp_xxx --for-approval`
- 手动触发一次 repo reconcile，并刷新 live report / runtime summary：
  `"$CODE_ARMY_SH" repo-reconcile camp_xxx`
- 查看单个 campaign：
  `"$CODE_ARMY_SH" get camp_xxx`
- Patch campaign：
  `"$CODE_ARMY_SH" patch camp_xxx '{"summary":"direction updated"}'`
- 人类批准计划，只有在 planner review、merged plan 和 refined task package 都通过校验后才会成功：
  `"$CODE_ARMY_SH" approve-plan camp_xxx`
- 应用一条 `/alice ...` 指令：
  `"$CODE_ARMY_SH" apply-command camp_xxx '/alice hold' feishu`

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
