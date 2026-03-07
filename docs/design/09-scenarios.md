# 09. 端到端场景

## 1. 简单天气问答

路径：

1. 用户用自然语言发起天气查询。
2. `IntentCompiler` 将其解释为 `task_request`。
3. `TaskClassifier` 判定为 `fast path`。
4. 创建轻量 `TaskSpec` 和 `RunRecord(run_mode=fast)`。
5. `FastSingleAgentRuntime` 调用天气工具或联网查询。
6. 返回结果并写轻量运行记录。

关键约束：

- 不开分支。
- 不占仓库写锁。
- 默认只允许只读和轻量执行。

## 2. 已有代码的模型优化任务

路径：

1. 用户用自然语言发起代码优化任务。
2. `IntentCompiler` 将其解释为 `task_request`。
3. 进入 `task path`。
4. `Planner` 角色读取项目记忆和评测基线。
5. `Developer` 角色修改代码并开 PR。
6. PR 更新触发自动评测。
7. `Reviewer` 角色解释评测结果。
8. 通过则报告，不通过则回写下一轮开发任务。

关键约束：

- 同一分支只能有一个写入型任务。
- PR 评测以 `pr_id + commit_sha + eval template` 为粒度幂等。

## 3. 评测过程中 PR 被新 commit 覆盖

路径：

1. PR 的 commit A 正在跑评测矩阵。
2. 用户 push 了 commit B。
3. Git webhook 触发新的 `EvalRequested`。
4. 控制平面生成新的 `EvalTask(commit_sha=B)`。
5. commit A 对应运行被标记为 `Superseded`。
6. 聚合器忽略 commit A 的门禁结论，只保留审计和产物。
7. commit B 的评测完成后再更新总 check。

关键约束：

- `Superseded` 不是失败。
- 老评测结果不能污染最新 PR 状态。

## 4. 长期科研探索

路径：

1. 用户用自然语言定义长期目标。
2. `IntentCompiler` 将其解释为长期 `task_request`。
3. 工作流长期运行，拆成多轮子任务。
4. 执行型 runtime 处理代码与实验。
5. 评审型 runtime 处理路线分析和阶段总结。
6. 每轮后生成报告、记忆和下一步建议。

关键约束：

- 必须有阶段终止条件。
- 必须定期请示或总结。
- 做不了及时上报优先于盲目继续。

## 5. 每天 9 点邮件摘要

路径：

1. 用户先说“以后每天早上 9 点帮我拉取新邮件并发总结到飞书”。
2. `IntentCompiler` 将该请求解释为 `setting_change`。
3. `SettingsValidator` 判断这是可变运行时设置。
4. 系统写入 `RuntimeSetting` 并创建 `ScheduleEntry`。
5. `Scheduler` 根据 cron 触发任务实例。
6. 邮件集成按游标拉取新增邮件。
7. `FastSingleAgentRuntime` 生成摘要。
8. 飞书发送结果。
9. 更新游标与记忆。

关键约束：

- 去重必须正确。
- 邮件空结果也要有明确策略。
- 高优先级邮件要支持升级告警。

## 6. 自然语言请求修改静态配置

路径：

1. 用户说“把 Git MCP 地址换成新的内网地址”。
2. `IntentCompiler` 输出 `setting_change`。
3. `SettingsValidator` 判定该请求属于静态基线配置。
4. 系统不直接改配置文件，而是创建 `ConfigChangeProposal`。
5. 生成风险摘要并请求人工批准。
6. 批准后由受控运维流程落地并写审计事件。

关键约束：

- 自然语言不能直接改密钥、根目录和 MCP 地址。
- 提案和真实落地动作必须分离。

## 7. 主控重启后接管远端运行

路径：

1. 一个远端实验已经提交到 SLURM，主控随后重启。
2. 启动恢复流程扫描到该 `RunRecord` 仍是非终态。
3. 系统恢复锁和 `lease_token`，查询对应 job 状态。
4. 若作业仍在跑，则补挂监控并继续。
5. 若作业已完成但结果未回写，则补收集产物并补写事件。
6. 若无法判断真实状态，则转 `Blocked` 并通知人工。

关键约束：

- 恢复必须先对账外部真实状态。
- 旧执行器的陈旧回写不能覆盖新租约持有者。

## 8. 人工中途改变方向

路径：

1. 长任务运行到 `Implementing`。
2. 用户通过飞书说“不要再追求速度了，改成优先稳定性，并先出一版总结”。
3. `IntentCompiler` 将其解释为 `manual_signal`。
4. `SignalResolver` 生成 `change_direction + force_report`。
5. 控制平面写入 `HumanSignalReceived`，更新 `workflow_phase`。
6. 当前开发动作收敛，转入 `Reporting` 或新的 `Planning`。

关键约束：

- 人工信号高于自动计划。
- 必须留下方向变更前后的审计链路。
