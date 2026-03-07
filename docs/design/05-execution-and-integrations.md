# 05. 执行与集成边界设计

## 1. 设计目标

执行和外部系统集成必须高度解耦。控制平面只依赖接口，不依赖平台实现细节。

## 2. 统一执行对象

为了让本地、SSH 和 SLURM 路径不会各长一套协议，执行层必须统一输入输出对象。

### 2.1 ExecutionRequest

最小字段：

- `run_id`
- `task_id`
- `executor_id`
- `command_spec`
- `workspace_ref`
- `env_policy`
- `timeout`
- `artifact_expectations`
- `idempotency_key`
- `lease_token`

### 2.2 ExecutionResult

最小字段：

- `run_id`
- `status`
- `exit_code`
- `stdout_ref`
- `stderr_ref`
- `artifact_manifest`
- `resource_usage`
- `failure_info`
- `executor_fingerprint`

规则：

- `ExecutionResult.status` 必须可映射到统一 `RunStatus`。
- 所有执行器都必须返回标准化产物清单，而不是各自拼字段。
- 所有写回都必须带 `lease_token`，防止陈旧执行者覆盖新状态。

## 3. Executor 抽象

统一接口：

- `Prepare`
- `Run`
- `Query`
- `Cancel`
- `CollectArtifacts`

### 3.1 LocalExecutor

适合：

- 本机只读查询。
- 小规模实验。
- 文档生成。
- 调试。

### 3.2 RemoteExecutor

`RemoteExecutor` 不绑定特定协议，而是通过外部能力层执行：

- `SSH MCP`
- `SLURM MCP`
- 未来其他远端执行 MCP

职责：

- 选择远端执行目标。
- 标准化输入输出。
- 收集日志、退出码、产物和远端路径。

## 4. 工作区治理

### 4.1 工作区原则

- 每个写入型 `run` 必须拥有独立工作目录或独立 worktree。
- 工作区根路径必须来自 `workspace_policy`，不得散落到未授权目录。
- 临时文件默认写到 run 级 `tmp`，不得把临时状态长期留在 `$HOME`。
- 产物目录必须按 `project_id/run_id` 分层，禁止不同运行混写。

### 4.2 清理与保留

- 运行结束后，工作区可清理，但失败现场需要先摘要化并保留关键产物。
- 仓库持久状态应通过 Git 或产物存储保留，不依赖脏工作树延续上下文。
- 共享缓存目录必须只读或带命名空间，不能作为隐式共享工作区。

## 5. MCP 边界

### 5.1 MCP 适用范围

优先 MCP 化的外部能力：

- Git
- SSH
- SLURM / HPC
- 邮件
- 飞书
- 领域专用工具

### 5.2 控制平面与 MCP 的边界

控制平面负责：

- 调用策略。
- 重试和超时。
- 幂等性。
- 预算和审批。
- 恢复对账。

MCP 负责：

- 平台协议。
- 认证。
- 请求到平台动作的映射。
- 平台响应解析。

## 6. GitProvider

接口：

- `CreateIssue`
- `CreateBranch`
- `CommitAndPush`
- `OpenPR`
- `QueryPR`
- `CommentPR`
- `SetCheckStatus`
- `MergePR`

### 6.1 语义要求

- `CommitAndPush` 必须返回 commit SHA。
- `OpenPR` 必须返回 PR 编号和 URL。
- `SetCheckStatus` 必须支持 `pending` / `success` / `failure` / `neutral`。
- `CommentPR` 用于评测摘要和建议，不可替代正式状态。
- 所有 PR、check 和评论写回都必须带幂等键。
- 默认不允许直接向受保护分支写入。

## 7. Notifier

接口：

- `SendProgress`
- `SendAlert`
- `RequestApproval`
- `ReceiveCommand`

### 7.1 通知策略

- 正常进度用普通消息。
- 高优先级阻塞或风险用 `@` 提醒。
- 审批用卡片。
- 人工指令要回写成系统信号。
- 发送动作若无法确认成功，必须进入恢复对账，而不是直接假定已送达。

## 8. Mail Integration

邮件读取适合做成独立 MCP 或适配器。

### 8.1 最小能力

- 读取邮件头与摘要。
- 基于游标拉取新增邮件。
- 拉取正文和附件元数据。
- 标记已处理或保存处理游标。

### 8.2 边界要求

- 邮件系统不负责摘要，只负责数据读取。
- 摘要和优先级判断由 AI 运行时完成。
- 游标推进必须是幂等副作用，不能因重试导致漏信或重复处理。

## 9. SSH MCP

最小能力：

- 准备远端工作目录。
- 执行命令。
- 查询进程或命令状态。
- 拉取日志和产物。
- 取消运行。

边界要求：

- SSH 认证细节不进入控制平面。
- 远端命令超时、退出码和 stderr 必须结构化返回。
- 远端工作目录必须和 `workspace_ref` 对齐，避免共享脏目录污染。

## 10. SLURM MCP

最小能力：

- 提交作业。
- 查询作业状态。
- 取消作业。
- 获取作业日志和产物路径。

边界要求：

- 将 `PENDING` / `RUNNING` / `COMPLETED` / `FAILED` 等 SLURM 状态映射到统一运行状态。
- 资源申请模板由控制平面或项目策略提供，不由 MCP 猜测。
- 对 job ID 的查询和取消必须支持恢复对账。

## 11. 安全边界

- 凭据不进入 agent 上下文。
- 执行器只允许进入授权工作目录。
- 高风险命令必须经过审批或策略允许。
- 不允许 agent 直接掌握平台管理员权限。
- 任何对外部系统的不可逆动作都必须走可审计接口。

## 12. 标准产物

执行和集成层必须返回标准产物：

- 退出码或状态码。
- stdout / stderr 摘要与全文引用。
- 产物清单。
- 时长与资源消耗。
- 错误类别。
- 相关外部对象引用，例如 PR URL、SLURM job ID、消息回执 ID。
