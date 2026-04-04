# Alice `dev` 相对 `main` 增量审稿意见

> 审稿时间：2026-03-25
> 比较范围：`main..dev`
> 审稿基线：`dev` 头部提交 `7984f30`
> 审稿方式：代码阅读 + 定向测试

## 结论

当前建议是 **request changes**。

这轮 `dev` 相对 `main` 的超前变更很大，方向整体合理，但我认为至少有 2 个阻断合入的问题，另有 1 个明确的 API 行为回归需要修正。

## Findings

### 1. 高优先级：`llm_profiles` 被声明为“自包含”，但运行时没有按所选 profile 生效 `command` / `timeout_secs` / `prompt_prefix`

**位置**

- `config.example.yaml:51-63`
- `internal/bootstrap/connector_runtime.go:36-88`
- `internal/connector/group_scenes.go:166-179`
- `internal/runtimeapi/automation_scope.go:113-142`

**Facts**

- 配置示例明确写了 “Each profile is self-contained”，并把 `command`、`timeout_secs`、`prompt_prefix` 都定义成 profile 字段。
- `buildFactoryConfig()` 的实现不是“按 profile 构建 backend”，而是“对每个 provider 只拿按名字排序后的第一个 profile 作为该 provider 的默认 `command/timeout/model/reasoning/prompt_prefix`”。
- group scene 和 automation 侧真正透传到运行请求里的只有 `provider`、`model`、`profile`、`reasoning_effort`、`personality`，没有把 profile 自己的 `prompt_prefix`、`timeout_secs`、`command` 一起切换。

**Inference**

- 同一个 provider 下如果配置多个 profile，后续选中的 profile 只能稳定切换 `model/profile`，但不能稳定切换该 profile 对应的 CLI 命令、超时时间、prompt 前缀。
- 这和当前配置文档以及这轮重构“把模型配置收敛到 profile”这件事的语义不一致，属于配置承诺与运行时行为不一致。

**Decision**

- 合入前应修正运行时 profile 解析语义。
- 最低要求是把 `command` / `timeout_secs` / `prompt_prefix` 也做成按实际 profile 生效，而不是 provider 级“取第一个”默认值。

### 2. 高优先级：campaign repo signal 处理直接改 repo 文件，但没有复用 `campaignRepoMu`，存在并发写 repo 的竞态窗口

**位置**

- `internal/bootstrap/campaign_repo_runtime.go:54-60`
- `internal/bootstrap/campaign_repo_signals.go:16-76`
- `internal/campaignrepo/signal_ops.go:12-90`

**Facts**

- automation completion hook 在 `handleCampaignRepoAutomationTaskCompletion()` 里先调用 `handleCampaignRepoTaskSignals()`，再调用 `runCampaignRepoReconcileCampaign()`。
- `runCampaignRepoReconcileCampaign()` 内部会持有 `campaignRepoMu`。
- `handleCampaignRepoTaskSignals()` 本身没有持锁，但它会进一步调用 `appendToFile()`、`campaignrepo.MarkTaskBlocked()`、`campaignrepo.ResetPlanForReplan()`，这些函数都会直接读写 campaign repo 文件。

**Inference**

- 多个 workflow task 同时完成时，signal handler 之间会并发修改同一个 campaign repo。
- 它们也可能与 5 分钟兜底 reconcile 或别的定向 reconcile 交错，形成 “load old state -> write back” 的 last-writer-wins 覆盖。
- 这类问题测试不一定稳定复现，但一旦出现就是 repo-native 状态丢失，排查成本很高。

**Decision**

- signal 对 repo 的写操作应纳入与 reconcile 相同的串行化保护范围。
- 最直接的修法是让 completion hook 在同一个 `campaignRepoMu` 临界区里完成 signal apply + reconcile，或者把 signal 变成 reconcile 输入，由 reconcile 统一落盘。

### 3. 中优先级：automation task delete 把权限/作用域错误映射成 `502`，API 语义错误

**位置**

- `internal/runtimeapi/automation_handlers.go:159-194`

**Facts**

- `handleAutomationTaskDelete()` 直接调用 `PatchTask()`。
- 如果 task 不在当前 scope，closure 返回 `"task not found in current scope"`；如果 actor 无权限，返回 `"permission denied for task delete"`。
- handler 只对 `automation.ErrTaskNotFound` 映射 `404`，其余错误统一走 `502 Bad Gateway`。

**Inference**

- 当前删除接口会把调用方自己的权限错误/作用域错误伪装成服务端故障。
- 这会误导客户端重试逻辑，也让排障方向偏掉。

**Decision**

- 这里应显式区分 `404` / `403` / `502`。
- 这不是架构级问题，但属于明确的 API 回归，建议在合入前一起修掉。

## 非阻断说明

- plan phase 的文档和状态机比之前清晰很多，但我这轮没有把它列成新的阻断项；我主要卡的是“当前实现与当前承诺不一致”的问题，而不是路线图缺口。
- `go test ./internal/campaignrepo ./internal/runtimeapi ./internal/bootstrap` 已通过，这说明现有测试没有覆盖住上面这些问题。

## 验证

本次审稿额外执行了：

```bash
go test ./internal/campaignrepo ./internal/runtimeapi ./internal/bootstrap
```

结果：通过。
