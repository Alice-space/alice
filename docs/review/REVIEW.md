# Alice 全量代码审稿意见

> 审稿时间：2026-03-25（全量审阅，覆盖完整代码库）
> 代码规模：~36,000 行 Go 代码，200+ 文件，19 个 internal 包，5 个 skill
> 审稿范围：全部 internal/ 包、cmd/connector、skills/、prompts/、docs/、scripts/、CI/CD

## 增量审稿入口

- `dev` 相对 `main` 的增量审稿见 [REVIEW-dev-vs-main-2026-03-25.md](./REVIEW-dev-vs-main-2026-03-25.md)

---

## 总体评价

Alice 是一个设计成熟、工程质量较高的多机器人对话平台。核心架构有清晰的分层：

- **Connector 层**：Feishu websocket intake → session 序列化 → LLM dispatch → reply pipeline
- **LLM 抽象层**：统一 Backend 接口，codex/claude/gemini/kimi 四个 provider adapter
- **Runtime API 层**：本地 HTTP API，skill 脚本通过 `alice runtime ...` CLI 与之交互
- **Automation 层**：bbolt 持久化的定时/工作流引擎
- **Campaign 层**：runtime index (bbolt) + campaign repo (文件系统主事实源) 双层结构
- **Skill 层**：可插拔的 bash 脚本 + YAML agent 定义

正规化（normalize）贯穿始终，`writeFileIfChanged` 保护幂等性，securejoin 防止路径穿越，整体防御性编程做得好。以下按模块和严重程度列出所有发现的问题。

## 处理状态（2026-03-25 第二次同步）

- 已修复：R1、R2、R3、R4、R5、R6、R8、R10、R11、R12、R14、R15、R16、R17、R20、R23、R24、R26、R27、R28、R30、R32、R34、R35、R36、R37、R38、R39、R40、R41、R42、R43、R46、R47、m1、m3、m8
- 保持不改并说明原因：R7、R9、R13、R18、R19、R21、R22、R25、R29、R31、R33、R44、R45、m2、m4、m5、m6、m7、m9、m10
- 验证：本轮按要求未额外运行测试/验证命令；此前记录的 `go test ./...`、`bash -n skills/alice-code-army/scripts/alice-code-army.sh` 仅代表上一次同步状态，不代表本轮修改后的重新验证结果

## 审稿意见逐条回复（2026-03-25 第二次同步）

- `R1` 已修复：`repo-reconcile` 保持调用真正执行状态推进的 `ReconcileAndPrepare`。
- `R2` 已修复：`ctxTime` 已改成无参当前时间函数。
- `R3` 已修复：dispatch prompt 已迁移到 `prompts/campaignrepo/*.md.tmpl`。
- `R4` 已修复：`syncCampaignDispatchTasks` / `syncCampaignWakeTasks` 提取了公共装载与 stale 清理逻辑，去掉重复结构。
- `R5` 已修复：`ReconcileAndPrepare` 改成在内存 `repo.Tasks` 上原地推进并落盘，不再在同一次 reconcile 中反复 `Load(root)`。
- `R6` 已修复：campaign repo reconcile 改为全量读取 campaign；`ListAllCampaigns` 新增 `limit < 0` 表示不截断。
- `R7` 不改：`Trial` 已是现有 runtime API 与持久化模型的一部分，当前直接统一术语会牵涉对外 schema 和历史数据；本轮只记录为长期收敛方向。
- `R8` 已修复：空 verdict 不再被归一成 `concern`。
- `R9` 不改：当前 `applyReviewVerdicts` 已在 verdict 落地时清空 `owner_agent` 和 lease，现有状态机没有观察到错误行为；先保留该隐式约束，待后续若 ready/rework 合并逻辑变化再显式拆分。
- `R10` 已修复：campaign repo 模板现在只保留 `P01`，删除了预置的 `P02` 到 `P07`。
- `R11` 已修复：`resolve_ihep_gitlab_helper` 不再对多 bot wildcard 命中取第一个；多匹配时明确报错，要求显式指定路径。
- `R12` 已修复：`campaign_handlers.go` 提取了 scope 解析与 campaign 装载/权限校验 helper，消除了重复样板校验链。
- `R13` 不改：真正解决 bbolt 全量快照更新需要把 automation store 的读写路径改成按 key 增量写入，并配套验证/迁移；这不是一轮局部补丁能安全收口的改动。
- `R14` 已修复：`runUserTask` 增加 panic recovery，并把 panic 作为 task 失败写回结果存储。
- `R15` 已修复：workflow task 不再无限时运行；现在也会挂到 watchdog timeout，下限为 24 小时，避免永久 `Running`。
- `R16` 已修复：`ConsecutiveFailures` 现在有消费方；连续失败达到阈值后自动 pause task 并清空 `NextRunAt`。
- `R17` 已修复：card `json.Marshal` 错误不再静默吞掉，构建失败会显式返回 error。
- `R18` 不改：当前 runtime API patch 合约没有 revision precondition 字段；直接在 store 层强制 optimistic lock 会破坏现有调用方，需等 API 合约先扩展。
- `R19` 不改：deleted task 目前保留是为了历史/排障可见性；在没有 retention policy 和清理策略之前，不做物理删除。
- `R20` 已修复：session key 相关分隔 token 已统一复用常量，去掉多处分散的魔法字符串。
- `R21` 不改：这些 nil receiver guard 目前承担 bootstrap/reload 边界上的容错保护；在构造路径没统一之前，直接删掉会降低运行时韧性。
- `R22` 不改：锁顺序问题认可，但安全处理需要跨 `App` / `Processor` / session state 做一轮专门的锁序审计；本轮不做机会主义改锁。
- `R23` 已修复：用户名与群成员名缓存改成有上限、有 TTL 的内存缓存，不再无限增长。
- `R24` 已修复：thread reply 只在明确属于 “thread unsupported” 的 Feishu API 错误时才降级，其他 API 错误不再被吞掉。
- `R25` 不改：当前 markdown -> Feishu Post 仍然保守退化为纯文本，目的是避免生成不稳定/不兼容的 Post 富文本结构；完整保留 bold/italic 需要单独做一轮兼容性解析器。
- `R26` 已修复：`UploadFile` 现在和 `UploadImage` 一样有显式大小限制；runtime API 也增加了 request body size limit。
- `R27` 已修复：消息提取失败时不再把原始 JSON 截断回显给用户，统一返回空字符串。
- `R28` 已修复：`ComposePromptPrefix` 不再是空操作；现在会把 `personality` 和 `noReplyToken` 明确编入 prefix。
- `R29` 不改：provider 功能对齐是路线图级工作，不适合在本轮混入大量适配器行为变更；先保留差异并在文档/审稿回复中明确。
- `R30` 已修复：Gemini stdout 解析改为流式 JSON decode，不再先把全部 stdout 读进内存。
- `R31` 不改：把 codex synthetic diff guard 从包级全局挪到实例级，需要联动 runner 构造和跨 package API；属于专门 codex 重构议题，本轮不拆。
- `R32` 已修复：Gemini/Kimi 的环境变量合并现在也按 key 排序，和 Codex/Claude 对齐，提升 trace 可重复性。
- `R33` 不改：config reload 真正回滚需要 staged object graph 替换和失败回退，不是局部 try/finally 能安全解决；本轮不做半套回滚。
- `R34` 已修复：已在架构文档中补充 Codex work scene 默认 `danger-full-access` + `never` 的安全取舍说明。
- `R35` 已修复：多 bot runtime 地址现在会显式校验唯一性；自动端口推导失败也不再静默回退到同一个默认地址。
- `R36` 已修复：`AllowedBundledSkills()` 改成基于 skill spec 的数据驱动判定，不再在函数体里分支硬编码 skill 名称。
- `R37` 已修复：runtime API 增加统一 request body size limit middleware。
- `R38` 已修复：runtime API Bearer token 认证路径增加了进程内 rate limiting。
- `R39` 已修复：automation/campaign list 接口统一使用带范围校验的 `limit` 解析逻辑，拒绝负数和超大值。
- `R40` 已修复：runtime API 的 task/campaign 创建、修改、删除等写操作新增审计日志。
- `R41` 已修复：OpenAI 图像输出现在对 base64 解码结果和 URL 下载结果都做了大小限制。
- `R42` 已修复：16 张参考图限制提成了具名常量 `openAIReferenceImageLimit`，不再硬编码在调用点。
- `R43` 已修复：`statusview.Result` 增加了 `HasErrors` / `IsSuccess` / `IsPartialSuccess` / `IsFailure` 判定方法，调用方可区分完全成功、部分成功和失败。
- `R44` 不改：测试覆盖不均衡是事实，但本轮目标是修审稿问题而不是补整套测试矩阵。
- `R45` 不改：端到端集成测试需要额外 harness、fixture 和流程编排，本轮不引入大体量测试基建。
- `R46` 已修复：`docs/architecture.md` 与 `docs/architecture.zh-CN.md` 已补充 `statusview`、`imagegen`、`messaging`、`storeutil`。
- `R47` 已修复：架构文档已补充 session key 格式与 alias 规则说明。
- `m1` 已修复：`NormalizeCampaign` 对 `Trials` / `Guidance` / `Reviews` / `Pitfalls` 现在按 ID 去重。
- `m2` 不改：automation 卡片/提示语目前仍视为产品文案而非 i18n surface；国际化需要独立方案。
- `m3` 已修复：`Schedule.EverySeconds: 60` 提成了具名常量。
- `m4` 不改：`DisableAck` 的改名收益不高，但会波及 `Job` 结构、测试和多处调用；先不做纯命名 churn。
- `m5` 不改：`soulDocument.Loaded` 当前在调用方里同时承担“已解析且可用”的语义，单独重命名属于低收益清理。
- `m6` 不改：`normalizeIncomingGroupJobTextForTriggerMode()` 虽长但语义直接，本轮不做纯命名重排。
- `m7` 不改：Codex file change enrichment 的条件嵌套确实复杂，但没有独立行为缺陷，本轮不做无行为收益的可读性重构。
- `m8` 已修复：找不到 prompts 目录时，`DefaultLoader()` 现在会打 warning。
- `m9` 不改：`sender_content.go` 的 markdown regex 已经是包级 `MustCompile`，只会在进程初始化时编译一次；无需改动。
- `m10` 不改：当前没有具体调用点被迫为了发送富文本而强转到 `LarkSender`；在缺少真实使用方之前不扩大 messaging 接口面。

## 处理状态（2026-03-25 第三次同步）

- 新增项已修复：R48
- 新增项保持不改并说明理由：R49
- 验证：`go test ./internal/automation ./internal/bootstrap`

## 审稿意见逐条回复（2026-03-25 第三次同步）

- `R48` 已修复：campaign repo reconcile 改为“事件驱动 + 兜底轮询”。兜底轮询间隔从 1 分钟调整为 5 分钟；同时新增 automation user task completion hook，在 `campaign_dispatch:*` / `campaign_wake:*` 任务完成后立即对对应 campaign 做一次定向 reconcile，继续刷新 live report、dispatch task 和 wake task。
- `R49` 不改功能实现，但已收紧文档表述：当前 runtime 只实现 executor / reviewer 两阶段调度，plan 阶段若直接补 planner 状态机会同时牵涉 task status、dispatch kind、prompt、review/apply 流程、campaign 默认角色与脚手架，属于完整 feature 而不是局部补丁。本轮不引入半套 planner 逻辑，但已更新 `alice-code-army` skill、agent prompt 与 campaign 模板，明确 proposal / merged plan / human gate 目前是 repo-native 的人工或交互式流程，避免继续过度承诺自动化能力。

---

## 审稿人回复（2026-03-25 第三次同步后）—— 严格审查

前两轮审稿过于宽松，本轮重新逐条严格审查所有回复，包括之前轻易放过的"不改"项。

### R48 确认

✅ **接受**。实现验证通过：

- 兜底轮询 1min → 5min，合理
- completion hook 通过 `campaign_dispatch:*` / `campaign_wake:*` stateKey 前缀做定向 reconcile，有 `campaignRepoMu` mutex 防并发
- hook 有 panic recovery，失败被 5min 兜底兜住，双保险完整

### R49 判定：驳回"不改"——要求提交实现计划

**❌ 不接受仅收紧文档。**

作者说"planner 是完整 feature，本轮不引入半套逻辑"。这个判断本身没错，但结论不应该是"不做"——R49 是 Code Army 的核心价值主张。没有 plan 阶段自动化，Code Army 只是一个带 reconcile 的 task dispatcher，和 SKILL.md 承诺的"多模型协作规划"差距巨大。

收紧文档只是承认了现状，没有推进能力边界。

**要求**：
1. 在本 PR 中提交 R49 的实现计划文档（可以是 `docs/plan-stage-design.md` 或直接写在 REVIEW.md），包含：状态机设计、新增 DispatchKind、prompt 模板、reconcile 扩展点、预计工作量
2. 在下一个里程碑中排入开发计划，优先级 **P0**——这不是"长期改进"，是产品完整性缺口

### 重新严格审查之前接受的"不改"项

#### R13（bbolt 全量快照）：驳回"不改"——要求本轮修复

**❌ 作者说"不是一轮局部补丁能安全收口"，但实际分析表明这是过度夸大。**

验证了 `store_snapshot.go` 和 `store_tasks.go`：

- `writeSnapshotTx()` 每次都 `DeleteBucket` + 重写全部 task，只有 4 个调用点（`ResetRunningTasks`、`CreateTask`、`PatchTask`、`ClaimDueTasks`）
- `PatchTask` 是最高频路径（被 `RecordTaskResult` 和 `RecordTaskSignal` 调用），每次 task 完成都触发全量重写
- **修复方案实际上很简单**：让 `PatchTask` 只写变化的 key，不删除整个 bucket。bbolt 本身就是按 key 存储的，不需要 migration，不需要改 schema，向后兼容
- 预计工作量：2-3 小时

作者声称需要"配套验证/迁移"，但实际上：
1. 不需要 migration——bbolt 已经按 key 存储
2. 不需要新 validation——`ValidateTask()` 已在写入前调用
3. 完全向后兼容——旧数据不受影响

**要求**：至少将 `PatchTask` 改为单 key 更新。这是最高频的写路径，改动范围小、风险低、收益明确。

#### R18（Revision 乐观锁）：维持接受不改

✅ 作者说需要先扩展 API 合约。合理——store 层加锁但 API 不传 revision，会导致所有 patch 调用失败。保持不改。

#### R19（deleted task 永不清理）：驳回——要求加 retention TTL

**❌ 作者说"保留为排障可见性"，但没有任何清理机制意味着 bbolt 文件只会单调增长。**

不要求本轮做物理删除，但要求：
1. 给 deleted task 加一个 `DeletedAt` 时间戳
2. 在 `ClaimDueTasks` 或 system task 中加一个简单的清理逻辑：删除 `DeletedAt` 超过 30 天的 task
3. 工作量：~1 小时

#### R22（锁顺序）：维持接受不改

✅ 锁序审计确实是独立工作，不适合混入本轮。

#### R25（Markdown 格式丢失）：维持接受不改

✅ 保守策略合理，飞书 Post 兼容性确实有坑。

#### R29（Provider 功能不对称）：维持接受不改

✅ 路线图级工作。

#### R31（diff guard 全局状态）：维持接受不改

✅ codex 重构议题。

#### R33（config reload 回滚）：维持接受不改

✅ 半套回滚比不回滚更危险，同意。

#### R44/R45（测试覆盖）：驳回——要求补 campaignrepo 核心测试

**❌ 作者以"本轮目标不是补测试"回避，但 campaignrepo 测试覆盖严重不足。**

验证了 `repository_test.go`，发现：

- 只有 3 个测试函数
- `applyReviewVerdicts` 只测了 "concern" 一种 verdict，缺 "approve"/"blocking"/"reject"/空 verdict
- `buildDispatchSpecs` 只验证了数量（`len(specs) != 2`），没验证 spec 内容
- `resolveExecutorRole` / `resolveReviewerRole` 零测试
- `latestRelevantReview` 零测试
- R5 改动（指针原地修改 + `persistTaskDocument`）没有专门测试验证突变正确性

这不是"补整套测试矩阵"，而是新写的 reconcile 核心逻辑缺少基本覆盖。

**要求**：至少补充以下测试：
1. `applyReviewVerdicts` 的 approve/blocking/reject/空 verdict 四条路径
2. `buildDispatchSpecs` 验证 spec 内容（prompt、role、stateKey）
3. `latestRelevantReview` 的 round 匹配和时间排序

#### 其余"不改"项（R7、R9、R21、m2-m10）：维持接受

这些确实是低优先级清理项或需要独立大改的议题，不适合混入本轮。

### R5 的新发现：指针原地修改正确但脆弱

验证了 `reconcile.go` 的 R5 修复。逻辑正确，但有一个脆弱性：

`claimSelectedExecutorTasks` 用 `taskIndexesByPath` 构建 path→index 映射，然后通过 index 修改 `repo.Tasks[taskIndex]`。当前安全因为 `persistTaskDocument` 不会重排 slice。但这是**隐式假设**——未来如果有人在 persist 后对 slice 做排序或过滤，index 就失效了。

**要求**：在 `persistTaskDocument` 之后加一行防御性断言：

```go
if repo.Tasks[taskIndex].Path != selected.Path {
    return claimed, fmt.Errorf("task index stale for %s", selected.Path)
}
```

### 验证缺口

作者在第二次同步中明确写道：

> "验证：本轮按要求未额外运行测试/验证命令"

第三次同步运行了 `go test ./internal/automation ./internal/bootstrap`，但**没有运行 `./internal/campaignrepo` 的测试**。作为修改了 reconcile 核心逻辑（R5）的变更，这是不可接受的。

**要求**：运行 `go test ./...` 并报告结果。

---

### 第三次同步总结

| 项目 | 判定 |
|------|------|
| R48 | ✅ 接受 |
| R49 | ❌ 驳回仅文档收紧，要求提交实现计划 |
| R13 | ❌ 驳回"不改"，要求 PatchTask 改单 key 更新 |
| R19 | ❌ 驳回"不改"，要求加 retention TTL |
| R44 | ❌ 驳回"不改"，要求补 campaignrepo 核心测试 |
| R5 补充 | ⚠️ 要求加防御性断言 |
| 验证 | ❌ 要求跑全量测试 |

**审稿结论：Revise and Resubmit。** 有 4 个 blocking 项需要处理后重新提交。

---

## 处理状态（2026-03-25 第四次同步）

- 已修复：R13、R19、R44、R5（补充防御性断言）
- 已按新增要求补充实现计划：R49
- 验证：`go test ./...`

## 审稿意见逐条回复（2026-03-25 第四次同步）

- `R13` 已修复：`automation.Store.PatchTask` 不再走全量 snapshot 重写，改为直接在 bbolt `tasks` bucket 中读取并回写单个 key；新增 `readTaskTx` / `writeTaskTx`，保留现有 schema 和校验逻辑，兼容旧数据。
- `R19` 已修复：automation task 新增 `DeletedAt` 字段；task 进入 `deleted` 状态时自动写入删除时间并清空 `next_run_at` / `running`；`ClaimDueTasks` 新增 30 天 retention 清理，物理删除过期 deleted task。对旧数据缺失 `DeletedAt` 的场景，清理逻辑回退使用 `UpdatedAt` 作为兼容时间基准。
- `R44` 已修复：`internal/campaignrepo` 新增核心单测，补齐 `applyReviewVerdicts` 的 approve / blocking / reject / 空 verdict 路径，`buildDispatchSpecs` 的 stateKey / role / prompt 内容断言，以及 `latestRelevantReview` 的 round 匹配与时间排序验证。
- `R5` 补充项已修复：`claimSelectedExecutorTasks` / `claimSelectedReviewTasks` 在 `persistTaskDocument` 后新增 path 防御性断言，显式防止未来 slice 重排导致的 index stale。
- `R49` 已按新增审稿要求修改：本轮未直接实现 planner runtime，但已提交详细设计文档 `docs/R49-plan-phase-design.md`。不在本轮直接落完整 planner 状态机的理由是：这会同时引入新的 campaign/task 状态、dispatch kind、prompt、plans 文档模型、CLI/skill 指令和 reconcile 分支，已经超出“修审稿 blocking 项”的安全范围；因此按新增意见先提交完整设计，作为下一个里程碑的 P0 开发输入，而不是在当前补丁里塞入半套实现。
- 验证已完成：除新增要求的 `./internal/campaignrepo` 外，本轮还重新跑了 `go test ./...` 全量测试并通过。为完成这次验证，顺手修正了几处与既有已修复行为不一致的旧测试/测试辅助函数，使测试预期与当前代码保持一致。

---

## 处理状态（2026-03-25 第五次同步）

- 新增项已修复：R50、R51、R52、R53、R54、R56、R57
- 新增项保持不改并说明理由：R55、R58、R59、R60、R61、R62、R63、R64、R65
- 验证：`go test ./...`

## 审稿意见逐条回复（2026-03-25 第五次同步）

- `R50` 已修复：删除了 `internal/mcpbridge/proc_context.go` 中未被任何生产代码使用的 process-tree session context 探测与合并逻辑，以及对应测试；`mcpbridge` 现在只保留实际在用的 env bridge 能力。
- `R51` 已修复：删除未使用的 `appendSessionAlias()`，调用点统一只保留 `appendSessionAliasWithLimit()`。
- `R52` 已修复：删除 `runtimeapi.Server` 上未被使用的 `Addr()` / `BaseURL()` / `Token()` 导出方法，避免继续暴露无调用方 API。
- `R53` 已修复：`ComposePromptPrefix()` 已移除未使用的 `loader` 参数；Codex/Claude/Gemini/Kimi 的调用点与测试同步收敛到无 `loader` 版本。
- `R54` 已修复：提取公共 `storeutil.UniqueNonEmptyStrings()`，`automation`、`campaign` 与 `campaignrepo` 统一复用，不再各自维护一份相同去重逻辑。
- `R55` 不改：R13 落地后，automation store 已经有单 key patch / deleted retention 等专属读写路径，而 campaign 仍是整份 snapshot 语义；现在强行泛型化会引入一组条件分支和回调拼装，反而掩盖两者已经真实分化的存储行为。
- `R56` 已修复：runtime API 新增共享的 `resolveRuntimeSessionContext()`，把 actor/receive/chatType/group 判定和 scope session key 提取集中到一处，`resolveAutomationScope()` 与 `resolveCampaignScope()` 不再各写一套前置解析。
- `R57` 已修复：新增 `internal/sessionkey` 包，统一 `Build()`、`VisibilityKey()`、`WithoutMessage()`；`runtimeapi`、`statusview`、`campaign`、`automation` 和 connector alias 解析已切到公共 helper。
- `R58` 不改：仓库里确实还存在较多重复 `TrimSpace()`，但它们分布在 config decode、runtime request 边界、connector 消息摄取等不同层，不能机械删除；本轮只借 R56/R57 消掉高重复的 scope/session key 路径，不做 repo-wide 的机会主义清扫。
- `R59` 不改：当前 frontmatter 解析器是 repo 约束下的窄接口实现，只接受文件头 YAML block；直接换成第三方库会改变解析容错边界和依赖面，收益不足以覆盖这轮审稿修复的范围。
- `R60` 不改：R23 后的用户名缓存已经有 TTL 和条目上限，当前 `1024/2048` 级别下写时 O(n) 驱逐的成本可控；为此引入新的 LRU 依赖收益不高。
- `R61` 不改：把 `automation/model.go` 与 `campaign/model.go` 的校验链整体迁到 validator tag，会同时改写错误文案、校验顺序和部分调用约束；这是风格级重构，不是本轮新增审稿问题里的局部缺陷修补。
- `R62` 不改：`mapstructure` DecodeHook 方案需要重新审计 config decode、默认值注入、env merge 和 multi-bot normalize 的先后顺序；当前显式 normalize 虽冗长，但时序清晰且已被现有测试覆盖。
- `R63` 不改：既然 `R25` 已确认当前策略是保守退化为纯文本，就没有必要在本轮为 Markdown AST 再引入 `goldmark`；只有未来决定恢复格式保真时，这个替换才有实际收益。
- `R64` 不改：`mcpbridge` 重命名成 `sessionenv` 的方向认可，但它会波及 `cmd/connector`、`runtimeapi`、`llm/kimi` 等多包 import 与历史命名；当前没有行为缺陷，本轮不做纯命名迁移。
- `R65` 不改：embedded prompt 作为磁盘 prompts 的 fallback 是刻意保留的发布兜底；增加 disk/embedded mismatch warning 需要先区分本地开发 override 与真正的构建漂移，否则会在正常开发路径上制造噪音。

---

## 审稿人回复（2026-03-25 第二次同步后）

本轮作者回复了全部 47+10 条审稿意见，其中 37 条已修复、20 条给出了不改的理由。总体处理质量很高，以下是审稿人逐条确认和意见。

### 已修复项确认

已验证代码变更，以下修复确认合格：

- **R1** ✅ `repo-reconcile` 现在调 `ReconcileAndPrepare`，与后台 system task 行为一致。
- **R2** ✅ `currentTime()` 简洁准确。
- **R3** ✅ prompt 模板化干净，`renderCampaignPrompt` 走标准 `prompting.DefaultLoader()`，`buildDispatchSpecs` 正确传播 error。
- **R4** ✅ 提取的 `loadCampaignAutomationTaskState` 和 `deleteStaleCampaignAutomationTasks` 消除了结构重复，两个 sync 函数现在只保留各自的业务差异。
- **R5** ✅ 改成 `*Repository` 指针 + `persistTaskDocument` 原地写盘是正确做法——保留了每步写盘的安全性，同时避免了反复 `Load`。`Summarize` 仍从内存 repo 重算摘要，逻辑正确。
- **R6** ✅ `limit < 0` 表示不截断，简洁且向后兼容。
- **R8** ✅ 空 verdict 返回空字符串 + `applyReviewVerdicts` 跳过空 verdict，防住了意外 rework。
- **R10** ✅ 只留 P01，模板更合理。
- **R11** ✅ 多匹配报错比静默取第一个好得多。
- **R12** ✅ `resolveCampaignRequestScope` + `loadScopedCampaign` 提取得当，handler 代码量显著缩减且审计日志位置自然。
- **R14** ✅ panic recovery + 写回失败结果，稳定性保障到位。
- **R15** ✅ watchdog timeout 下限 24h 合理，workflow 不再有永久 hang 风险。
- **R16** ✅ 连续失败 auto-pause 是 automation 引擎最缺的安全网之一。
- **R17** ✅ marshal error 不再被吞。
- **R20** ✅ session key 常量化。
- **R23** ✅ 有界 TTL 缓存解决了内存泄漏。
- **R24** ✅ 只对特定错误码降级是正确的。
- **R26** ✅ file upload + request body 双重限制。
- **R27** ✅ 空字符串比 JSON 碎片好。
- **R28** ✅ `ComposePromptPrefix` 现在有实际功能。
- **R30** ✅ Gemini 流式解析消除 OOM 风险。
- **R32** ✅ env merge 一致性。
- **R34** ✅ 安全取舍文档化。
- **R35** ✅ 地址唯一性校验。
- **R36** ✅ 数据驱动比硬编码好。
- **R37-R40** ✅ body limit, rate limit, limit 范围校验, 审计日志——四个安全加固一起落地。
- **R41-R42** ✅ 图像输出大小限制 + 常量化。
- **R43** ✅ `HasErrors/IsPartialSuccess` 让调用方易判断。
- **R46-R47** ✅ 架构文档补全，session key 格式文档化是高价值补充。
- **m1** ✅ normalize 去重。
- **m3** ✅ 常量化。
- **m8** ✅ warning 可观测性。

### 不改项审稿人意见

以下逐条给出审稿人对"不改"理由的判定：

| 编号 | 作者理由 | 审稿人判定 | 备注 |
|------|----------|-----------|------|
| R7 | 术语统一牵涉 API schema 和历史数据 | **接受** | 合理，记为长期收敛方向即可 |
| R9 | 隐式约束但无错误行为 | **接受** | 当前状态机正确，观察项 |
| R13 | bbolt 增量写入是大改动 | **接受但标记** | 理解不适合本轮，但这是存储层最大的技术债，建议下个里程碑排进去 |
| R18 | 乐观锁需先扩展 API 合约 | **接受** | 合理，先扩展合约再加锁 |
| R19 | deleted task 保留为排障可见性 | **接受，附建议** | 合理，但建议增加 retention TTL（如 30 天），避免无限积累 |
| R21 | nil guard 承担 reload 容错 | **接受** | 认可在构造路径统一前保留 |
| R22 | 锁序审计是独立工作 | **接受** | 同意不做机会主义改锁 |
| R25 | Feishu Post 富文本兼容性风险 | **接受** | 保守策略合理 |
| R29 | provider 对齐是路线图级 | **接受** | 明确差异在文档中即可 |
| R31 | diff guard 全局→实例需联动 | **接受** | codex 重构议题 |
| R33 | config reload 回滚是系统级设计 | **接受** | 半套回滚确实比不回滚更危险 |
| R44 | 测试矩阵不属于本轮 | **接受** | |
| R45 | 集成测试需独立基建 | **接受** | |
| m2-m7, m9-m10 | 低收益改动 | **接受** | 这些确实是低优先级清理项 |

### 新增项 R48/R49 审稿人意见

- **R48**（reconcile 轮询间隔）：建议合理，事件驱动 + 兜底轮询是更好的模式。但考虑到当前 executor/reviewer 完成后本身会通过 wake task / dispatch task 与 automation 引擎交互，reconcile 的主要职责是"捡漏"，1 分钟间隔在当前规模下开销可控。建议等 campaign 数量或 task 数量超过阈值后再优化。优先级 P3。

- **R49**（Plan 阶段缺失）：这是最有价值的架构观察。Code Army 的核心卖点之一是多模型协作规划，但当前只有执行/审阅两阶段。proposal → merge → human gate → execute 的完整链路缺失意味着用户必须手动完成规划阶段，skill 描述的自动化承诺落空。设计方案方向正确，角色与模型解耦是关键。优先级 P1——这应该是下一个主要特性开发。

### 总结

## 详细问题列表

- [REVIEW-details.md](./REVIEW-details.md) — R1-R49 + m1-m10 原始问题
- [REVIEW-details-redundancy.md](./REVIEW-details-redundancy.md) — R50-R65 代码冗余、死代码、外部库替换
