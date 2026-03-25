# 第四部分：LLM 后端

### R28. `ComposePromptPrefix` 是空操作 [待处理]

`internal/prompting/prefix.go`

函数接收 `personality` 和 `noReplyToken` 参数但完全不使用，只 TrimSpace 返回 prefix。这是死代码还是预留接口？

---

### R29. Provider 功能覆盖严重不对称 [待处理]

| 功能 | Codex | Claude | Gemini | Kimi |
|------|-------|--------|--------|------|
| Usage 统计 | ✅ | ❌ | ❌ | ❌ |
| File change 追踪 | ✅ | ❌ | ❌ | ❌ |
| Tool call 追踪 | ✅ | ✅ | ❌ | ✅ |
| Streaming | ✅ | ✅ | ❌ | ✅ |
| Exec policy | ✅ 两级 | ❌ 固定 bypass | ❌ | ❌ |

Codex 有 57 个测试，其他 provider 7-10 个。功能差距大，导致切换 provider 后行为差异显著（如 /status 看不到 token 用量）。

---

### R30. Gemini 后端完全不做 streaming [待处理]

`internal/llm/gemini/gemini.go`

读取全部 stdout 到内存，大响应可能 OOM。其他三个 provider 都做了流式解析。

---

### R31. Codex synthetic diff guard 使用包级全局状态 [待处理]

`internal/llm/codex/synthetic_diff_guard.go`

`syntheticDiffGuard` 是全局单例，影响测试隔离和并发安全。应注入到 Runner 或使用 per-instance guard。

---

### R32. 环境变量合并方式不一致 [待处理]

Codex sort keys 后合并，Gemini/Kimi 不排序。虽然功能不受影响，但 debug trace 的可重复性不同。

---

# 第五部分：Bootstrap / Config

### R33. Config reload 无回滚机制 [待处理]

`internal/bootstrap/config_reload.go`

如果 reload 中途失败（如 `buildLLMBackend` 成功但后续步骤失败），系统处于部分更新状态。无回滚逻辑。

---

### R34. 默认 Codex Work Scene 使用 `danger-full-access` [待处理] — 安全文档缺失

`internal/config/config.go:113-116`

默认权限为 `sandbox: danger-full-access`, `approval: never`。虽然是设计意图（work scene 需要完全访问），但缺少安全影响文档。

---

### R35. Runtime HTTP 地址唯一性未校验 [待处理]

`internal/config/multibot_runtime.go`

多 bot 场景下若手动配置地址，可能冲突。`incrementHostPort` 失败时静默返回默认值，所有 bot 可能共用同端口。

---

### R36. `AllowedBundledSkills()` 硬编码 skill 名称 [待处理]

`internal/config/multibot.go:220-243`

skill 名称（alice-message, alice-scheduler, alice-code-army）硬编码在权限逻辑中。改名需同步修改。

---

# 第六部分：Runtime API

### R37. 无 request body size limit [待处理]

`internal/runtimeapi/server.go`

无 Gin middleware 限制请求体大小。恶意或异常的大 JSON payload 可导致内存问题。

---

### R38. 无 API rate limiting [待处理]

Bearer token 认证无速率限制，token 较弱时存在暴力猜测风险。

---

### R39. limit 参数解析不验证范围 [待处理]

`internal/runtimeapi/automation_handlers.go`, `campaign_handlers.go`

`fmt.Sscanf` 解析 limit 参数，不检查负数或超大值。

---

### R40. 缺少审计日志 [待处理]

Campaign/Task 的创建、修改、删除没有审计日志。生产环境下无法追溯谁改了什么。

---

# 第七部分：Image Generation

### R41. `UploadFile` 无 size limit（同 R26）[待处理]

`internal/imagegen/openai_output.go`

Base64 解码后也不验证大小，API 返回超大内容可导致 OOM。

---

### R42. Reference image 16 张硬编码限制 [待处理]

`internal/imagegen/openai_http.go`

`compactExistingPaths` 最多取 16 张参考图，限制值 hardcode 且无文档说明。

---

# 第八部分：Status View

### R43. 部分成功 vs 完全成功无法区分 [待处理]

`internal/statusview/service.go`

`Query()` 返回的 `Result` 中，campaign/task/usage 各自可能有 error。调用方无法简单判断是部分成功还是全部成功。

---

# 第九部分：测试与 CI

### R44. 测试覆盖不均匀 [待处理]

- `internal/llm/codex`: 57 个测试 case
- `internal/llm/claude`: 10 个
- `internal/llm/gemini`: 7 个
- `internal/llm/kimi`: 8 个
- `internal/campaignrepo`: 1 个测试文件
- `internal/connector`: processor 测试只覆盖 memory context 和 reply flow

---

### R45. 集成测试缺失 [待处理]

所有测试都是 unit test（fake shell script + mock），无端到端集成测试。Reconcile → dispatch → execute → review 的完整流程未被测试覆盖。

---

# 第十部分：文档

### R46. 架构文档未提及 `internal/statusview` [待处理]

`docs/architecture.md` 的 component map 列出了 connector, llm, prompting, memory, automation, runtimeapi, skills，但缺少 statusview, imagegen, storeutil, messaging 这些包。

---

### R47. Session key 格式未文档化 [待处理]

Session key 格式 `{type}:{id}|scene:{scene}|thread:{thread}|message:{msg}` 是核心路由机制，但只能通过代码推断，无文档说明。

---

# 小问题汇总

| 编号 | 位置 | 描述 |
|------|------|------|
| m1 | `internal/campaign/model.go` | NormalizeCampaign 对 Trials/Guidance/Reviews/Pitfalls 不去重 |
| m2 | `internal/automation/engine_render.go` | 硬编码中文 UI 字符串，不可国际化 |
| m3 | `internal/bootstrap/campaign_repo_runtime.go` | `Schedule.EverySeconds: 60` hardcode，应提为常量 |
| m4 | `internal/connector/processor.go` | `DisableAck` 负向布尔命名，阅读困难 |
| m5 | `internal/connector/types.go` | `Loaded` in soulDocument 含义模糊，应为 `Parsed` |
| m6 | `internal/connector/group_scenes.go` | 函数名过长如 `normalizeIncomingGroupJobTextForTriggerMode()` |
| m7 | `internal/llm/codex/codex.go` | File change message enrichment 有复杂嵌套条件 |
| m8 | `internal/prompting/default_loader.go` | 找不到 prompts 目录时静默返回空字符串，无 warning |
| m9 | `internal/connector/sender_content.go` | Markdown regex 在包级编译，应确认只编译一次 |
| m10 | `internal/messaging/ports.go` | 无 `SendRichText` / `SendRichTextMarkdown` 端口，需强制转型到 LarkSender |

---

# 总结评分

| 维度 | 评分 | 说明 |
|------|------|------|
| 架构设计 | ⭐⭐⭐⭐ | 分层清晰，扩展面合理，multi-bot 模型成熟 |
| 代码质量 | ⭐⭐⭐⭐ | normalize 贯穿始终，防御性好，命名整体规范 |
| Campaign 重构 | ⭐⭐⭐⭐ | repo-first 方向正确，reconcile 逻辑完整 |
| LLM 抽象 | ⭐⭐⭐ | 接口简洁，但 provider 间功能差距过大 |
| 存储层 | ⭐⭐ | bbolt 全量快照更新是性能瓶颈，无索引 |
| 安全性 | ⭐⭐⭐ | securejoin 好，但缺 rate limit / body size limit |
| 测试覆盖 | ⭐⭐⭐ | codex 测试优秀，其他模块偏薄 |
| 文档 | ⭐⭐⭐ | 架构文档好，但缺 session key 格式等细节 |

## 修复优先级建议

**P0（影响正确性/稳定性）：**
- R14 用户 task goroutine panic recovery

**P1（影响性能/安全）：**
- R13 bbolt 全量快照更新
- R23 用户名缓存无界无 TTL
- R26/R37 upload/request body size limit
- R33 config reload 回滚机制

**P2（改善可维护性）：**
- R4 sync 函数去重
- R7 Trial vs Task 术语统一
- R20 session key 魔法字符串
- R28 ComposePromptPrefix 清理
- R47 session key 格式文档

**P3（长期改进）：**
- R29 provider 功能对齐
- R44/R45 测试覆盖均衡化和集成测试
- R40 审计日志
- R46 架构文档补全

---

# 第十一部分：Code Army 架构补充审查（2026-03-25 第三次同步）

### R48. Reconcile 轮询间隔过短 [已修复]

`internal/bootstrap/campaign_repo_runtime.go:17`

`campaignRepoReconcileInterval = time.Minute`，每分钟全量扫描一次 campaign repo。但 executor/reviewer 一次任务通常运行数分钟到数十分钟，绝大多数轮询周期内状态无变化，产生无谓的目录遍历和 live-report 重写 IO 开销。

**建议**：改为事件驱动 + 兜底轮询模式。executor/reviewer 完成时通过 runtime API 主动触发一次 reconcile，兜底轮询间隔拉长到 5–10 分钟。

---

### R49. Plan 阶段只有脚手架，无执行逻辑 [已提交实现计划，待开发] — 架构缺口

当前 Code Army 只实现了 executor → reviewer 两阶段流程。SKILL.md 描述的计划阶段完全没有运行时代码支撑。

**详细设计已完成**，见 `docs/R49-plan-phase-design.md`。核心要点：

**三阶段流程**：

```
planned → planning → plan_review_pending → planning (concern 循环)
                                         → plan_approved (approve)
                                         → plan_approved → running (人类 /alice approve-plan)
```

**设计原则**：
- 角色与模型解耦：planner / planner_reviewer 只是角色标签，绑定哪个 provider/model 由 campaign 级配置决定
- 最小侵入：复用现有 reconcile → dispatch → automation 管线，不新建引擎
- Human gate 是硬门槛：planner_reviewer approve 后必须等人类 `/alice approve-plan` 才进入执行

**Plan 状态机**（campaign repo frontmatter `plan_status` 字段）：
- `idle` → `planning` → `plan_review_pending` → `plan_reviewing` → `plan_approved` → `human_approved`
- concern 回到 `planning`（plan_round++），approve 到 `plan_approved`

**新增内容**：
- `DispatchKind`: `planner`, `planner_reviewer`
- `CampaignFrontmatter` 新增: `default_planner`, `default_planner_reviewer`, `plan_round`, `plan_status`
- `Repository` 新增: `PlanProposals`, `PlanReviews` 集合（扫描 `plans/proposals/` 和 `plans/reviews/`）
- 新文件 `internal/campaignrepo/reconcile_plan.go`: `reconcilePlanPhase()`, `applyPlanVerdict()`, `promoteDraftTasksToReady()`
- 新模板: `prompts/campaignrepo/planner_dispatch.md.tmpl`, `planner_reviewer_dispatch.md.tmpl`
- Skill script: 新增 `approve-plan`, `plan-status` 命令及 `/alice approve-plan` 指令

**Task 展开方式**：Planner 直接生成 `phases/Pxx/tasks/Txxx.md`（status=draft），reviewer 可直接审查 write_scope 和依赖关系。Human approve 后 reconcile 自动将 draft → ready，执行阶段无缝接管。

**Bootstrap 集成**：planner dispatch 复用 `campaign_dispatch:` StateKey 前缀和 `syncCampaignDispatchTasks()` 逻辑，无需改动 sync 代码。
