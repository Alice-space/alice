# Prompt 拼装

Alice 如何构建发送给 LLM 后端的 prompt 文本。

## 模板系统

Alice 使用 Go `text/template` 并结合 [Sprig](https://masterminds.github.io/sprig/) 函数进行 prompt 模板化。模板文件以 `.md.tmpl` 为后缀。

### 模板加载

```
1. 检查磁盘：<prompt_dir>/<template>.tmpl
2. 如果未找到，使用内嵌模板（编译进二进制文件）
```

磁盘文件会覆盖内嵌模板，支持按 bot 自定义。

### 模板文件

所有模板位于 `prompts/` 下：

| 模板 | 用途 |
|----------|---------|
| `connector/bot_soul.md.tmpl` | 将 SOUL.md 正文注入 prompt |
| `connector/current_user_input.md.tmpl` | 格式化当前用户消息 |
| `connector/reply_context.md.tmpl` | 添加来自被回复消息的上下文 |
| `connector/runtime_skill_hint.md.tmpl` | 描述可用的 bundled skill |
| `connector/synthetic_mention.md.tmpl` | 格式化合成 @mention |
| `connector/help.md.tmpl` | `/help` 命令响应 |
| `llm/initial_prompt.md.tmpl` | 首轮系统指令 |
| `goals/goal_start.tmpl` | Goal 初始化 prompt |
| `goals/goal_continue.tmpl` | Goal 继续 prompt |
| `goals/goal_timeout.tmpl` | Goal 超时通知 |

## 模板变量

模板可以访问完整的 `Job` 上下文和 session 元数据。关键变量包括：

| 变量 | 说明 |
|----------|-------------|
| `.UserText` | 用户的消息文本 |
| `.BotName` | 回复 bot 的显示名称 |
| `.SenderName` | 发送消息的用户名称 |
| `.MentionedUsers` | 消息中 @mention 的用户列表 |
| `.ReplyContext` | 被回复消息的文本 |
| `.Attachments` | 收到的附件元数据 |
| `.Scene` | `"chat"` 或 `"work"` |
| `.SessionKey` | 规范 session 标识符 |
| `.SoulBody` | SOUL.md 正文内容（仅 chat） |
| `.SkillDescriptions` | 已启用 bundled skill 的描述 |

## 首轮 vs 恢复

Prompt 拼装的关键区别：

### 首轮（无已有 Thread）

- 拼装完整的初始 prompt
- 包含系统指令（`initial_prompt.md.tmpl`）
- Chat 场景：前置 SOUL.md 正文
- 身份提示（`Name`说：、@mention 规则），除非 `disable_identity_hints: true`

### 恢复（存在 Provider Thread）

- 仅发送当前用户的消息文本
- Alice 依赖 **provider 端的 thread/session** 保持之前的上下文
- 无系统 prompt，无 SOUL.md，无身份提示
- 这样更高效 — 后端模型已拥有完整的对话历史

## SOUL.md 注入

SOUL.md 根据场景有两个用途：

### Chat 场景

1. Alice 读取文件，解析 YAML frontmatter
2. Frontmatter 字段（`image_refs`、`output_contract`）被 Alice 消耗用于回复控制
3. 剩余 Markdown 正文通过 `bot_soul.md.tmpl` 前置到首轮 prompt

### Work 场景

SOUL.md 被**刻意跳过**。Work 模式用于任务执行 — 注入人格会干扰工具使用和代码生成。

## 身份提示

当 `disable_identity_hints: false`（默认）时，Alice 为消息添加上下文：

```
张三说：fix the login timeout
```

当 `disable_identity_hints: true`，原始消息直接传递：

```
fix the login timeout
```

## Prompt 前缀

每个 LLM profile 可以设置 `prompt_prefix`：

```yaml
llm_profiles:
  work:
    prompt_prefix: "You are a senior Go engineer. Be concise, use idiomatic patterns."
```

此文本会被前置到该 profile 的每次 prompt 中，包括恢复的 session。

## Prompt 与调试

当 `log_level: debug` 时，Alice 会记录发送给每个后端的完整渲染 prompt。Debug 跟踪包括：

- Provider 名称
- 模型和 profile
- Thread/session ID
- 完整的渲染输入文本
- 观察到的工具调用活动和最终输出

> 警告：渲染后的 prompt 可能包含 SOUL.md 内容和对话历史。避免公开发布 debug 日志。
