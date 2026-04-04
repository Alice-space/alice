# 第十二部分：代码冗余、死代码、外部库替换（2026-03-25 第四次审查）

> 说明：本文件保留原始问题描述；处理状态与逐条回复见 [REVIEW.md](./REVIEW.md) 中“2026-03-25 第五次同步”。

## 死代码和未使用导出

### R50. `mcpbridge` 包含未使用的公开函数 [待处理]

`internal/mcpbridge/proc_context.go`

- `MergeSessionContext()` 和 `SessionContextFromProcessTree()` 在整个代码库中没有调用方（只在自身 test 中出现）
- 包名 `mcpbridge` 本身具有误导性——MCP 工具已废弃，这个包实际只是 session context 环境变量桥接
- 建议删除两个未使用函数，或将包重命名为 `sessionenv`

---

### R51. `appendSessionAlias` 未使用 [待处理]

`internal/connector/session_state_alias.go:36`

函数 `appendSessionAlias(aliases []string, alias string) []string` 从未被调用。代码中直接调用的是 `appendSessionAliasWithLimit`。应直接删除。

---

### R52. `runtimeapi.Server` 导出方法未被调用 [待处理]

`internal/runtimeapi/runtime_config.go`

`Addr()`、`Token()`、`BaseURL()` 三个 receiver method 是导出的但在包外没有任何调用方。如果是为了未来扩展预留，应加注释说明；否则应删除。

---

### R53. `ComposePromptPrefix` 的 `loader` 参数被丢弃 [待处理]

`internal/prompting/prefix.go`

```go
func ComposePromptPrefix(loader *Loader, ...) string {
    _ = loader // 显式丢弃
```

R28 已修复了 personality/noReplyToken 的使用，但 loader 仍然被丢弃。要么通过 loader 渲染 prefix 模板，要么从签名中移除。

---

## 代码重复

### R54. `uniqueNonEmptyStrings` 存在三份拷贝 [待处理] — 应提取到公共包

完全相同的逻辑出现在三个包中：
- `internal/automation/model.go` — `uniqueNonEmptyStrings`
- `internal/campaign/model.go` — `uniqueNonEmptyStrings`
- `internal/campaignrepo/repository.go` — `normalizeStringList`（同逻辑不同名字）

三份代码有微妙差异（变量名、nil-return 条件），但功能完全一致。应提取到 `internal/storeutil` 或新建 `internal/stringutil` 包。

---

### R55. bbolt snapshot 读写模式在 campaign 和 automation 间 95% 重复 [待处理]

`internal/automation/store_snapshot.go` 和 `internal/campaign/store_snapshot.go`

两个文件的 `viewSnapshot`、`updateSnapshot`、`readSnapshotTx`、`writeSnapshotTx` 结构完全一致，唯一差异是类型名（`Task` vs `Campaign`）和 bucket 名。

Go 1.18+ 泛型可以消除这个重复。在 `internal/storeutil` 中提供通用的泛型 snapshot helper：

```go
func ViewSnapshot[T any](db *bolt.DB, bucket string, normalize func(T) T) ([]T, error)
func UpdateSnapshot[T any](db *bolt.DB, bucket string, ...) error
```

这也是 R13（PatchTask 单 key 更新）改完后进一步优化的最佳切入点——泛型化一次，两个 store 同时受益。

---

### R56. Scope 解析逻辑重复 [待处理]

`internal/runtimeapi/automation_scope.go:26-71` 和 `campaign_handlers.go:299-340`

`resolveAutomationScope` 和 `resolveCampaignScope` 前半段完全相同：

```go
// 两个函数都做：
if err := session.Validate(); err != nil { ... }
actorUserID := strings.TrimSpace(session.ActorUserID)
actorOpenID := strings.TrimSpace(session.ActorOpenID)
actorID := actorUserID
if actorID == "" { actorID = actorOpenID }
if actorID == "" { return ..., errors.New("missing actor id") }
chatType := strings.ToLower(strings.TrimSpace(session.ChatType))
isGroup := chatType == "group" || chatType == "topic_group"
```

约 50-60% 代码重复。应提取共享的 actor/scope 解析到一个公共 helper。

---

### R57. Session key 构建/解析逻辑散布在三个位置 [待处理]

`{type}:{id}` 格式的 session key 构建在以下位置各写了一遍：
- `internal/runtimeapi/automation_scope.go:209-221` (`scopeSessionKey`)
- `internal/statusview/service.go:81-82` (inline)
- `internal/automation/engine_runtime.go:174-180` (`engineSessionKey`)

三处逻辑一致但写法不同。应提供一个统一的 `sessionkey.Build(idType, id)` 工具函数。

---

### R58. 过度重复 TrimSpace [待处理] — 30+ 处对已归一化值再次 trim

多处代码对已经经过 normalize 的值再次调用 `strings.TrimSpace()`。例如：

- `campaign_repo_runtime.go` 中 `shouldAutoReconcileCampaign` 对 `NormalizeCampaign` 输出再次 trim
- `campaign_handlers.go` 中 `strings.TrimSpace(c.Param("campaignID"))` 后在 helper 中再次 trim
- `automation_scope.go` 对 session 字段多次 trim

原则：在系统边界 normalize 一次，内部代码信任已归一化的值。

---

## 外部库替换机会

### R59. 手写 frontmatter 解析器可用 `adrg/frontmatter` 替代 [待处理]

`internal/campaignrepo/repository.go:745-769`

`parseMarkdownFrontmatter` 手写了约 25 行 frontmatter 解析。`github.com/adrg/frontmatter`（MIT 协议）处理了更多边界情况（如 `---` 出现在内容正文中）。

当前实现在 frontmatter 内容中包含 `---` 行时会误判截断位置。

---

### R60. 手写 LRU 缓存应使用 `hashicorp/golang-lru` [待处理]

`internal/connector/sender_user_name.go`

R23 修复后的自定义缓存 eviction 是 O(n)（遍历所有 item 找最旧的）。`hashicorp/golang-lru/v2`（MPL-2.0）提供 O(1) LRU 操作和内置 TTL 支持（`expirable.NewLRU`）。

---

### R61. `go-playground/validator` 已引入但严重未充分使用 [待处理]

`go.mod` 中已有 `github.com/go-playground/validator/v10`，但只在 `config/config_validate.go` 中用了一个 3 字段的 struct。

`automation/model.go` 和 `campaign/model.go` 中有大量手写 if/else 校验链（各 100+ 行），完全可以用 struct tag 声明式替代：

```go
// 当前：手写 60 行 ValidateTask
func ValidateTask(t Task) error {
    if t.ID == "" { return errors.New("task id is empty") }
    if t.Title == "" { return errors.New("task title is empty") }
    // ... 50 more lines
}

// 改进：struct tag + validator
type Task struct {
    ID    string `json:"id" validate:"required"`
    Title string `json:"title" validate:"required,max=255"`
}
```

已付费引入依赖但没充分使用。

---

### R62. config normalize 的 100+ 行 TrimSpace/ToLower 可用 mapstructure DecodeHook 替代 [待处理]

`internal/config/config_normalize.go`

`normalizeLoadedConfig` 函数有 100+ 行逐字段 `strings.TrimSpace()` / `strings.ToLower()`。`go.mod` 中已有 `github.com/go-viper/mapstructure/v2`，其 DecodeHook 可以在 decode 阶段自动完成 string 字段的 trim 和 lowercase。

---

### R63. 手写 Markdown→飞书 Post 转换的 regex 解析可考虑 goldmark [待处理] — 低优先级

`internal/connector/sender_content.go`

当前用 `regexp.MustCompile` 解析 markdown。`github.com/yuin/goldmark`（MIT）提供完整的 CommonMark AST，可以精确遍历 markdown 结构。

但考虑到 R25 决定保守退化为纯文本，这个替换优先级低——只有决定保留格式信息时才值得引入。

---

## 过期兼容性代码

### R64. `mcpbridge` 包名应重命名为 `sessionenv` [待处理]

架构文档已明确"MCP naming is limited to session-context env keys"。包名 `mcpbridge` 暗示与 MCP 协议有关，但实际只是 env var 序列化。重命名为 `sessionenv` 或 `sessioncontext` 消除误导。

`ALICE_MCP_*` 环境变量名本身可以保留（向后兼容 skill 脚本），但包的 Go 命名不应继续使用已废弃的术语。

---

### R65. `embedded_prompts.go` 与磁盘 prompts 的双重维护风险 [待处理] — 观察项

`embedded_prompts.go` 嵌入了 `prompts/` 目录作为 fallback FS。`internal/prompting/loader.go` 优先从磁盘读取，找不到时 fallback 到 embedded。

当前不是 bug（disk-first + embedded-fallback 是正确模式），但存在一个隐患：开发者修改了磁盘 prompt 模板但忘记重新编译 binary 时，embedded 版本和磁盘版本不一致。建议在 debug 模式下，loader 对比 embedded 和 disk 版本，如果不同打 warning。

---

## 优先级

**Should-Fix（强烈建议本轮处理）：**
- R54 — `uniqueNonEmptyStrings` 三份拷贝合并
- R55 — bbolt snapshot 泛型化（与 R13 一起做）
- R56 — scope 解析逻辑去重
- R50/R51 — 删除未使用的 mcpbridge 函数和 appendSessionAlias

**建议后续处理：**
- R57/R58 — session key 和 TrimSpace 去重
- R59-R62 — 外部库替换（frontmatter、LRU、validator tags、mapstructure hooks）
- R64 — mcpbridge 包重命名
- R52/R53/R63/R65 — 低优先级清理
