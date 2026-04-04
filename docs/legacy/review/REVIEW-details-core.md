# 第一部分：Campaign / Code Army 重构（当前变更）

## 严重问题

### R1. `repo-reconcile` 命令实际上不做 reconcile [已修复]

`cmd/connector/runtime_campaign_repo_cmd.go`

`ScanFromPath` 只读不写，但命令名叫 `repo-reconcile`。后台 system task 用的 `ReconcileAndPrepare` 才是真正做状态推进的。

**处理结果**：已改为调用 `ReconcileAndPrepare`，命令输出携带 `dispatch_tasks`。

---

### R2. `ctxTime` 函数签名欺骗性 [已修复]

`cmd/connector/runtime_campaign_repo_cmd.go`

函数接收 `ctx` 但完全忽略。

**处理结果**：已改为无参 `currentTime()`。

---

## 重要问题

### R3. 硬编码 prompt 字符串与架构文档矛盾 [已修复]

`internal/campaignrepo/reconcile.go`

`buildExecutorDispatchPrompt` / `buildReviewerDispatchPrompt` 内嵌 prompt 字符串，违反架构文档 "prompts live in template files only"。

**处理结果**：已抽到 `prompts/campaignrepo/executor_dispatch.md.tmpl` 和 `reviewer_dispatch.md.tmpl`。

---

### R4. `syncCampaignDispatchTasks` 和 `syncCampaignWakeTasks` 结构重复 [待处理]

`internal/bootstrap/campaign_repo_runtime.go:88-201`

两个函数结构几乎相同（查询 → 构建索引 → upsert/create → 清理 stale），唯一差异是 `DispatchTaskSpec` vs `WakeTaskSpec`。重复约 100 行。

---

### R5. reconcile 多次重载磁盘 [待处理]

`internal/campaignrepo/reconcile.go:50-108`

`ReconcileAndPrepare` 每步写完后都 `Load(root)` 重载仓库，大型 repo 每次 reconcile 触发最多 4 次完整目录遍历。当前作为正确性保障可接受，但 reconcile 间隔缩短后需关注。

---

### R6. `ListAllCampaigns("", 200)` 固定上限 [待处理]

`internal/bootstrap/campaign_repo_runtime.go:33`

活跃 campaign 超过 200 条时，后续永远不被 reconcile。

---

## 设计观察

### R7. 术语双轨：Trial vs Task [待处理]

`internal/campaign` 用 `Trial`（A/B 实验语义），`internal/campaignrepo` 用 `Task`（工程调度语义）。两者是不同层次的概念但容易混淆。建议长期弃用 `Trial`，以 repo 层 Task 为唯一状态源。

---

### R8. `normalizeReviewVerdict` 对空 verdict 的处理 [已修复]

空 verdict + `blocking == false` 原本返回 `"concern"`，导致意外 rework。

**处理结果**：空 verdict 现在返回空字符串，`applyReviewVerdicts` 跳过空 verdict review。

---

### R9. `leaseBlockReason` 不区分 ready 和 rework [待处理]

`internal/campaignrepo/repository.go`

逻辑本身安全（`applyReviewVerdicts` 已清空 owner_agent），但属于隐式假设，边界条件值得留意。

---

### R10. 模板预设 7 个 phase [待处理]

`skills/alice-code-army/templates/campaign-repo/phases/`

预设 P01-P07 可能误导用户。建议 scaffold 只创建 P01。

---

### R11. `resolve_ihep_gitlab_helper` glob 通配符 [待处理]

多 bot 场景下 glob 匹配取第一个结果，可能不稳定。

---

### R12. Campaign handler 重复样板代码 [待处理]

`internal/runtimeapi/campaign_handlers.go`

每个 handler 重复 `!s.allowRuntimeCampaigns()` + `s.campaigns == nil` + `resolveCampaignScope()` + `GetCampaign()` + `campaignVisibleToSession()` + `canManageCampaign()` 检查链。6 个 handler 共约 200 行纯样板。可用 Gin middleware + 共享 context 提取。

---

# 第二部分：Automation 引擎

### R13. BBolt 全量快照更新 [待处理] — 性能隐患

`internal/automation/store_snapshot.go:100-119`

每次更新（即使只 patch 一个 task）都 `DeleteBucket` 然后重写所有 task。线性 O(n) 开销。当 task 数量增长时严重影响性能。

**建议**：改为按 key 单条更新，只重写变化的 task。

---

### R14. 用户 task goroutine 没有 panic recovery [待处理] — 稳定性风险

`internal/automation/engine_user.go`

System task 有 `defer recover()`（`engine_system.go:80-84`），但 `go e.runUserTask()` 没有。未处理的 panic 会导致 goroutine 静默死亡。

**建议**：给 `runUserTask` 加 defer recovery wrapper。

---

### R15. Workflow task 没有超时 [待处理]

`internal/automation/engine_user.go:60-66`

Workflow 类型 task 跳过 timeout，注释说"workflows may run long"。但没有任何 watchdog，hung workflow 会永久阻塞该 task（`Running` flag 不清除）。

---

### R16. `ConsecutiveFailures` 被追踪但未使用 [待处理]

`internal/automation/store_tasks.go`

字段被 `RecordTaskResult` 递增，但没有消费方。没有 circuit breaker、指数退避或自动暂停。

---

### R17. `json.Marshal` 错误被静默忽略 [待处理]

`internal/automation/engine_render.go:44,78`

```go
raw, _ := json.Marshal(card) // 错误被丢弃
```

marshal 失败时返回空 card，破坏 UI 而不报错。

---

### R18. `Revision` 字段未用于乐观锁 [待处理]

`internal/automation/store_tasks.go`

Task 有 `Revision` 字段但 PatchTask 不检查。并发 patch 可能静默覆盖。

---

### R19. Deleted task 永不清理 [待处理]

软删除 task 永远留在 bbolt 里，数据只增不减。

---

# 第三部分：Connector / 消息处理

### R20. Session key 使用魔法字符串构造 [待处理]

`internal/connector/group_scenes.go`, `session_state_alias.go`

分隔符 `"|scene:"`, `"|message:"`, `"|thread:"`, `"|reset:"` 分散在多个文件中，没有集中常量。部分有常量、部分没有，不一致。

---

### R21. 过度防御性 nil receiver 检查 [待处理]

几乎所有 Processor/App 方法都以 `if p == nil { return }` 开头。大量此类检查掩盖了初始化设计问题——如果 receiver 可能为 nil，说明构造流程有隐患。

---

### R22. 多层互斥锁交叉 [待处理]

`internal/connector/app.go`, `processor.go`

App 持有 `cfgMu`, `automationMu`, `state.mu`, per-session mutexes；Processor 持有 `runtimeMu`, `mu`。缺少锁顺序文档，存在潜在死锁风险。

---

### R23. 用户名缓存无界且无 TTL [待处理] — 内存泄漏

`internal/connector/sender_user_name.go`

`userNameCache` 和 `chatMemberNameCache` 是全局 `sync.Map`，无驱逐策略。用户数增长时内存单调递增，且缓存不会因用户改名而失效。

**建议**：替换为有界 LRU cache + TTL。

---

### R24. Thread-aware reply 把所有 API error 当作"不支持 thread" [待处理]

`internal/connector/sender.go`

`replyMessagePreferThread()` 在任何 `feishuAPIError` 时都 fallback 到 direct reply。应只对特定错误码降级。

---

### R25. Markdown → 飞书 Post 转换丢失语义格式 [待处理]

`internal/connector/sender_content.go`

`**bold**`, `*italic*` 等格式标记被直接剥离，只保留纯文本。飞书 Post 格式支持 bold/italic element，语义信息丢失不必要。

---

### R26. 文件上传缺少统一大小限制 [待处理]

`internal/connector/sender_media.go`

`UploadImage` 有 10MB 限制，但 `UploadFile` 没有。`internal/runtimeapi/message_handlers.go` 也没有 request body size limit middleware。

---

### R27. 消息提取 fallback 泄漏 JSON [待处理]

`internal/connector/sender_message_lookup.go`

结构化解析失败时，fallback 把原始 JSON 内容截断后返回给用户。应返回空字符串或通用提示。

---

