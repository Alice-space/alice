# TDR 04: 入口接入、ReceptionAgent 与确认链路

## 1. 目标

本文定义 `internal/ingress`、`ReceptionAgent`、身份校验与确认令牌的实现，覆盖飞书、Web、GitHub / GitLab webhook 等入口。

建议目录：

- `internal/ingress/router.go`
- `internal/ingress/feishu/`
- `internal/ingress/web/`
- `internal/ingress/github/`
- `internal/ingress/gitlab/`
- `internal/workflow/reception.go`
- `internal/platform/auth/`

## 2. 输入标准化

所有入口都必须先被标准化为以下两类之一：

- `UserRequest`
- `ExternalEvent`

区分标准：

- 人类显式发起的自然语言请求进入 `UserRequest`
- issue / PR / review / 按钮回执 / webhook 进入 `ExternalEvent`

入口适配层只做：

- 解析
- 验签
- actor 识别
- 原始负载引用持久化
- 提交 BUS 命令

入口适配层不能直接决定业务状态。

## 3. ReceptionAgent 责任

`ReceptionAgent` 只做三件事：

1. 识别任务类型
2. 风险分级
3. 产出 `RouteDecision`

建议接口：

```go
type ReceptionAgent interface {
    Classify(ctx context.Context, req domain.UserRequest) (domain.RouteDecision, error)
}
```

建议输出：

```go
type RouteDecision struct {
    Route          string
    TaskType       domain.TaskType
    RiskLevel      domain.RiskLevel
    NeedsHuman     bool
    WaitingReason  domain.WaitingReason
    CoalescingKey  string
    ReasonSummary  string
}
```

Route 仅允许：

- `sync_direct`
- `sync_mcp_read`
- `control_write`
- `async_issue`
- `wait_human`

## 4. 路由算法

建议按规则优先级而不是自由生成执行：

1. 若命中高风险动作且无有效确认，输出 `wait_human`
2. 若请求明确要求改代码、改测试、改仓库、开 PR，输出 `async_issue`
3. 若是只读 MCP 查询，例如用量或配置读取，输出 `sync_mcp_read`
4. 若是有副作用的控制调用，例如改设置、改记忆、改定时任务，输出 `control_write`
5. 若是轻量查询或只读分析，输出 `sync_direct`
6. 其余无法稳定分类的情况输出 `wait_human`

禁止：

- 一次入口请求里同时启动多个主动作
- 把“复杂但非代码”的任务自动塞进 issue 主流程

## 5. 入口命令流

### 5.1 人类消息入口

```text
receive message
  -> verify channel auth
  -> normalize UserRequest
  -> classify
  -> CreateTask
  -> branch:
       sync_direct / sync_mcp_read / control_write / async_issue / wait_human
```

### 5.2 GitHub / GitLab webhook

```text
receive webhook
  -> verify signature
  -> dedupe by external_event_id
  -> normalize ExternalEvent
  -> locate existing task or create task from issue
  -> submit workflow command
```

## 6. 身份与权限

所有高风险动作前必须把外部身份映射到内部 `alice_user_id`。

```go
type IdentityBinding struct {
    AliceUserID   string
    Provider      string
    ExternalID    string
    Roles         []string
    MFAValidated  bool
    LastVerifiedAt time.Time
}
```

支持来源：

- 飞书用户 ID
- Web 登录会话
- GitHub / GitLab 账号

最小权限规则：

- 普通消息可创建任务和补充上下文
- 高风险控制动作需要明确角色
- 合并审批默认要求高于提交人权限

## 7. Confirmation 设计

高风险动作必须先签发 `Confirmation`。

```go
type Confirmation struct {
    ConfirmationID    string
    ConfirmationToken string
    TaskID            string
    ActionHash        string
    RequestedBy       string
    ApproverID        string
    Channel           string
    AuthSnapshot      json.RawMessage
    ExpiresAt         time.Time
    Status            string
}
```

状态：

- `issued`
- `confirmed`
- `rejected`
- `expired`
- `consumed`

签发规则：

- 使用 CSPRNG 生成 token
- 默认 10 分钟有效
- 绑定 `task_id + action_hash + approver_id`
- 重复点击只消费一次

## 8. Web 与飞书按钮回流

按钮或表单回流也视为 `ExternalEvent`。

实现要求：

- 请求必须带 `confirmation_token`
- 路径只允许携带 `confirmation_id` 这类非敏感标识
- 真正的 `confirmation_token` 必须放在 POST body 或专用 header
- 先验证 token 状态与过期时间
- 成功后提交 `ConsumeConfirmation` 命令
- BUS 决定恢复到哪个状态，而不是回调 handler 直接改状态
- API、反向代理、APM 和接入日志必须对 token 字段脱敏

## 9. 同步任务执行

### 9.1 `sync_direct`

适用场景：

- 轻量查询
- 单轮总结
- 只读文件解释

处理方式：

- 创建短生命周期 `Task`
- 进入 `sync_running`
- 执行成功后 `closed`
- 超时或错误进入 `failed`

### 9.2 `sync_mcp_read`

适用场景：

- 查询用量
- 查询配置
- 其他只读控制面查询

处理方式：

- 通过 Control MCP 直接同步执行
- 不经过 `outbox`
- BUS 只落审计事件与结果摘要，不生成副作用意图
- 请求成功时返回稳定即时结果

### 9.3 `control_write`

适用场景：

- 修改设置
- 修改记忆
- 创建、暂停或删除定时任务
- 其他白名单内单步控制写操作

处理方式：

- 有副作用的控制调用一律走 `outbox`
- 如果策略要求确认，则先进入 `waiting_human(waiting_confirmation)`
- 确认通过后创建 `OutboxAction(control_call)`
- 状态路径通常为 `task_created -> sync_running -> closed/failed`；若需确认，则先 `task_created -> waiting_human -> sync_running`
- 请求入口只返回 `task_id` 和当前状态，不承诺同步得到最终结果
- HTTP / Web API 推荐返回 `202 Accepted` 而不是伪造同步成功
- 任务完成时再进入 `closed` 或 `failed`

## 10. issue 镜像建立

非 issue 来源的代码任务需要补建 issue。

流程：

1. `CreateTask`
2. 标记 `issue_sync_pending`
3. 创建 `OutboxAction(create_issue)`
4. 由 outbox worker 通过 GitHub / GitLab MCP 建 issue
5. 成功后 `BindIssue`
6. 切换到 `issue_created`

失败策略：

- 瞬时错误：指数退避重试
- 403 / 404 等致命错误：`failed`
- 长时间未完成：`waiting_human(waiting_recovery)`

## 11. 接口建议

### 11.1 Webhook HTTP

- `POST /webhooks/github`
- `POST /webhooks/gitlab`
- `POST /webhooks/feishu`

### 11.2 Admin API

- `POST /api/v1/tasks`
- `POST /api/v1/confirmations/{confirmation_id}/approve`
- `POST /api/v1/confirmations/{confirmation_id}/reject`
- `POST /api/v1/tasks/{task_id}/cancel`

确认接口请求体建议：

```json
{
  "confirmation_token": "opaque-secret-token"
}
```

或通过专用 header 传递：

```text
X-Alice-Confirmation-Token: opaque-secret-token
```

这些 API 只负责把输入转换为命令，不直接持久化业务状态。

## 12. 最小测试矩阵

- webhook 验签失败拒收测试
- 重复 `confirmation_token` 只消费一次测试
- `confirmation_token` 不出现在路径与普通访问日志测试
- 高风险动作无确认进入 `waiting_human` 测试
- 代码任务从消息入口走 `issue_sync_pending` 测试
- `sync_mcp_read` 直接返回结果且不生成 `outbox` 测试
- `control_write` 生成 `outbox` 并返回受理状态测试
