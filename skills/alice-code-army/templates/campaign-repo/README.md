# Campaign Repo

这个仓库是当前 campaign 的主事实源。

- 长期状态以这里的 markdown/frontmatter 为准。
- planner、planner reviewer、executor、reviewer 都应先读这里，再决定下一步。
- 不要把聊天消息、runtime summary 或外部 issue 当成主存储。

## 新 Agent 先按这个顺序读

1. `campaign.md`
   先确认 objective、current_phase、plan_status、plan_round 和 source_repos。
2. `reports/live-report.md`
   先看当前活跃任务、阻塞项、review queue 和下一步。
3. `plans/merged/master-plan.md`
   如果已经有人类批准的总计划，执行和拆任务时以它为准。
4. `findings.md` 和 `docs/research-contract.md`
   先补齐研究约束、已知发现、禁区和外部依赖。
5. `repos/*.md`
   确认 source repo 的本地路径、默认分支、工作分支和关键 commit。
6. `phases/<phase>/phase.md` 与 `phases/<phase>/tasks/<task-id>/`
   先读当前 phase，再读对应 task package 里的 `task.md`、`context.md`、`plan.md`。
7. `phases/<phase>/tasks/<task-id>/reviews/`
   需要判断 task 为什么被接受、返工或阻塞时，再看 task-local review 文件。

## 目录怎么理解

- `campaign.md`
  总目标、计划阶段状态、当前方向。
- `plans/`
  planner proposal、planner reviewer review、以及最终 merged master plan。
- `phases/`
  按 phase 组织任务；模板只保留 `P01` 作为示例，phase 数量由 planner 决定。
- `phases/<phase>/tasks/<task-id>/`
  一个完整 task package。executor 进入这个文件夹就应该能拿到目标、背景、计划、结果和审阅。
- `reports/`
  live report、阶段报告、最终报告。
- `repos/`
  source repo 的引用信息，不放业务代码本体。
- `paper/`
  论文或最终文档草稿。

## 关键约定

- `campaign repo` 是主事实源，`source repo` 是真实代码改动面。
- planner 负责 proposal 和 draft task 切分。
- planner 必须把每个 task 细化成完整文件夹，不能只留一个 `T001.md` 空壳。
- planner 切 task 的标准是“executor 不需要回聊天追问也能开工”。
- 一个 task 尽量只覆盖一个明确产物，或一组紧耦合且可被同一 write scope 约束的改动。
- 如果一个 task 同时跨多个松耦合 repo / 模块 / 产物，或者验收方式不止一条独立主线，应该继续拆分。
- planner reviewer 只审 proposal / task 切分。
- executor 可以改 source repo 和 task 目录。
- reviewer 只写 task-local `reviews/` 文件，由 Alice judge 应用 verdict。
- phase 数量、task 数量、拆分粒度由 planner 按目标、依赖和 write scope 决定，不要被模板样例限制。

## 开始工作前至少确认

- 当前 `plan_status` 是什么，是否还在 planning。
- 当前任务是否已有 `owner_agent`、`lease_until`、`depends_on`、`write_scope`。
- 目标 source repo 是否已经在 `repos/*.md` 里登记清楚。
- 上一轮 review / findings 是否已经解释了当前阻塞原因。
