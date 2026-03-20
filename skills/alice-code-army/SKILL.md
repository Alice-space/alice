---
name: alice-code-army
description: 用 GitLab issue / branch / MR、Alice 调度、集群执行和多模型 reviewer 组织多 trial 优化闭环。适用于在当前会话中启动、推进、暂停、取消、改向、比较和收敛代码或模型优化实验，并默认要求关键操作在 GitLab 上可见，维护共享实验记录避免重复踩坑。
---

# Alice 代码军队

`alice-code-army` 是一套长期优化任务的编排约定：

优先使用 `scripts/alice-code-army.sh` 管理当前会话下的 campaign。脚本会自动使用 Alice 注入的当前 thread/session 上下文和 runtime HTTP API。

- `GitLab` 做记录面：目标、trial、MR、review、CI、结论都优先落在 issue / MR。
- `Cluster` 做执行面：本地 GPU、IHEP、IHEPAI 等资源负责跑任务、查状态、取日志。
- `Alice` 做编排面：提出假设、开 trial、汇报进度、收集 reviewer 意见、推动收敛。
- `Kimi` / `DeepSeek` / 其他模型做 reviewer：负责讨论、审核、挑错、补建议。

## 何时使用

- 用户要做长期优化、并行多 trial、性能调优、代码试验、实验收敛。
- 用户提到 issue、MR、branch、pipeline、review、merge、reject、共享实验记录、踩坑复用。
- 用户希望能在 Feishu 或 GitLab 里打断、暂停、取消、改方向。
- 用户希望把别的模型拉进来一起讨论、审核和提建议。

## 可见性约束

- 默认要求 `campaign` 绑定一个 GitLab `issue`；如果还没有 `issue_iid`，先创建或绑定 issue，再继续后续操作。
- 默认要求关键状态变更在 GitLab 上可见：
  `campaign` 创建后应在同一轮内创建/绑定 issue，并把当前摘要同步到 issue。
  `trial` 创建后应在 GitLab 上可见，通常至少要在 issue 中出现；进入实现阶段后再创建 branch / MR。
  `guidance`、`review`、`pitfall`、`hold`、`cancel`、`accept`、`steer` 等状态变化，应在同一轮或紧接着同步到 issue / MR。
- 不允许把“只存在本地 runtime、GitLab 不可见”的状态当作默认完成态。
- 只有当用户明确允许“暂时不落 GitLab / 先本地草稿”时，才可以保留本地不可见状态；否则应把缺失的 issue / MR 视为阻塞项，而不是静默继续。

## 核心流程

1. 找到或创建目标 `issue`，写清 baseline、目标、硬门槛、当前计划；若 issue 不存在，先创建并绑定到 campaign。
2. 把每个 `trial` 映射到一个 `branch`，通常再配一个 `MR`。
3. 用本地或集群资源执行 trial，并把结果回写到 `MR` / `issue`；不要只停留在本地 campaign JSON。
4. 把同一批上下文交给 reviewer 模型，让它们审代码、审实验设计、审结果解释。
5. 汇总结果，给出 `merge` / `reject` / `hold` / `needs-more-evidence` 结论。
6. 追加共享实验记录，避免后续重复踩同一个坑。

## 常用命令

- 列出当前 thread 下的 campaign：
  `scripts/alice-code-army.sh list`
- 新建 campaign：
  `scripts/alice-code-army.sh create <<'JSON'`
  `{ "title": "Optimize Model-X", "objective": "速度提升且质量上升", "repo": "group/model-x", "issue_iid": "218", "max_parallel_trials": 3 }`
  `JSON`
  若没有 `issue_iid`，应先用 GitLab 创建 issue，再回填到 campaign；不要让 campaign 长时间处于“不可见”状态。
- 查看单个 campaign：
  `scripts/alice-code-army.sh get camp_xxx`
- Patch campaign：
  `scripts/alice-code-army.sh patch camp_xxx '{"status":"hold","summary":"waiting for user guidance"}'`
- 新增或更新 trial：
  `scripts/alice-code-army.sh upsert-trial camp_xxx '{"trial":{"id":"trial-1","hypothesis":"蒸馏小模型","status":"running"}}'`
- 追加 guidance：
  `scripts/alice-code-army.sh add-guidance camp_xxx '{"guidance":{"source":"feishu","command":"/alice hold"}}'`
- 追加 review：
  `scripts/alice-code-army.sh add-review camp_xxx '{"review":{"reviewer_id":"experiment-reviewer","verdict":"concern","summary":"需要复验"}}'`
- 追加 pitfall：
  `scripts/alice-code-army.sh add-pitfall camp_xxx '{"pitfall":{"summary":"spec decoding 长上下文退化","related_trial_id":"trial-2"}}'`
- 应用一条 `/alice ...` 指令到当前 campaign：
  `scripts/alice-code-army.sh apply-command camp_xxx '/alice cancel trial-2' issue`
- 渲染发往 issue 的 markdown 摘要：
  `scripts/alice-code-army.sh render-issue-note camp_xxx`
- 渲染单个 trial 发往 MR 的 markdown 摘要：
  `scripts/alice-code-army.sh render-trial-note camp_xxx trial-1`
- 同步一条 issue 摘要到 GitLab：
  `scripts/alice-code-army.sh sync-issue camp_xxx`
- 同步单个 trial 摘要到 GitLab MR：
  `scripts/alice-code-army.sh sync-trial camp_xxx trial-1`
- 同步 issue 和所有带 MR 的 trial：
  `scripts/alice-code-army.sh sync-all camp_xxx`

## 控制通道

- `Feishu`：即时控制，优先用于“马上停掉”“先别继续开新 trial”这类动作。
- `issue` / `MR` 评论：持久控制，适合留痕与协作。
- 初版优先使用明确命令，减少歧义，例如：
  `/alice hold`
  `/alice cancel trial-2`
  `/alice steer primary_metric=decode_latency accuracy_budget=0.1%`
  `/alice accept trial-1`
- 用户在 Feishu 给出关键指令后，如果 repo / issue 已知，应把决定同步回 GitLab 评论留痕。

建议语义：

- `hold`：不再启动新任务，已在跑的允许先跑完。
- `cancel trial-x`：终止指定 trial，对应 job 取消，MR 标记为 `aborted`。
- `steer ...`：修改目标、预算或方向；根据策略决定是否立即停掉正在跑的任务。
- `accept trial-x`：把某个 trial 提升为当前候选赢家，进入复验或合并判断。

当前脚本已经内置一层轻量落地：

- `apply-command` 会把 `/alice hold`、`/alice cancel trial-x`、`/alice accept trial-x`、`/alice steer ...` 写入 guidance，并同步 patch campaign / trial 状态。
- `render-issue-note` / `render-trial-note` 会把 campaign JSON 渲染成适合发到 GitLab 的 markdown。
- `sync-issue` / `sync-trial` / `sync-all` 优先复用环境里的 `ihep-gitlab` helper；若 helper 启用了 note 去重，可避免短时间内重复发送相同摘要。
- 当前 GitLab sync 只发纯文本 markdown，不上传附件文件。
- 因此默认动作应是：
  创建/更新 campaign -> 绑定或创建 issue -> `sync-issue`
  创建/更新 trial -> issue 可见 -> 若有 MR 则 `sync-trial`

## 评价规则

- 先设硬门槛，再比较谁更好。
- 硬门槛通常包括：
  速度或主目标必须提升。
  质量下降不能超过预算。
  显存、资源或复杂度不能越界。
  结果要可复现。
  reviewer 不能有未解决的阻塞意见。
- 只有通过硬门槛的 trial 才进入比较。
- 比较顺序通常是：主目标 -> 副目标 -> 工程风险 / 维护成本。

## 结论规则

- `merge`
  通过全部硬门槛；在同批 trial 中最优或最稳；至少有一次确认复验；没有未解决的 blocking review。
- `reject`
  未过硬门槛；明显差于 sibling trial；重复已知坑且没有新前提；或用户明确取消。
- `needs-more-evidence`
  有潜力但证据不足，保留 MR，不立即合并或拒绝。
- `hold`
  方向暂不清晰、资源受限、等待人工决策或外部依赖。

## 共享实验记录

- 不要只依赖聊天记忆或零散评论。
- 保留一层轻量结构化记录，既可放在 GitLab issue 总结表，也可放在仓库文件，例如：
  `experiments/index.yaml`
  `experiments/pitfalls.md`
- 每个 trial 至少记录：
  issue、branch、MR、假设、资源、结果、结论、原因、标签。
- 新开 trial 前，先读最近相关的失败记录；若只是重复旧坑且没有新证据，应先提醒用户，而不是直接重跑。

## 工具优先级

- 调度：`../alice-scheduler/scripts/alice-scheduler.sh`
- GitLab：若环境里有 `ihep-gitlab`，优先用它操作 issue / MR / CI。
- 集群：若环境里有 `ihep-main-cluster` / `ihep-ai-cluster`，优先用它们提交、查询、取消和取日志。
- 附件：只有发图片或文件时才使用 `alice-message`；纯文本继续走主回复链路。
- reviewer：主执行模型和 reviewer 模型不必相同。
- 若没有 GitLab issue / MR 可见面，优先补齐 GitLab 可见性，再继续 trial 编排。

## 简例

同一个优化 issue 下，可以并行开 3 个 trial，并在 issue 中维护总表：

```text
trial     branch                 MR      resource        status
trial-1   opt/128-fuse-kernel    !201    IHEPAI-5090-1   running
trial-2   opt/128-kv-cache       !202    IHEPAI-5090-2   running
trial-3   opt/128-int8           !203    local/V100      queued
```

如果用户评论 `/alice cancel trial-3`，应：

- 取消该 trial 的 job；
- 在 issue 和 MR 里记录原因；
- 把 `trial-3` 标记为 `aborted`；
- 继续比较 `trial-1` 和 `trial-2`。

## 回复模式

- 始终说明：目标 issue、活跃 trial、当前 winner / no-winner、阻塞项、下一步。
- 当状态变化时，明确写出是 `hold`、`cancel`、`steer`、`merge`、`reject` 还是 `needs-more-evidence`。
- 在 `merge` / `reject` / `aborted` 时，追加一条可复用的共享记录摘要。
- 若当前还没有 GitLab issue / MR，可直接说明这是阻塞项，并优先补齐；不要把本地不可见 campaign 伪装成已完成落地。
