# 自定义 SOUL.md 人格

每个 bot 可以拥有一个名为 `SOUL.md` 的人格文档，用于定义其行为、语气和回复偏好。

## 什么是 SOUL.md？

`SOUL.md` 是一个带 YAML frontmatter 的 Markdown 文件。它有两个用途：

1. **人格设定**：Markdown 正文被注入到 `chat` 场景的 LLM prompt 中，塑造 bot 的语气和行为
2. **元数据**：YAML frontmatter 控制机器可读的回复行为

## 文件位置

默认情况下，Alice 在 bot 的 `alice_home` 下查找 `SOUL.md`：

```
~/.alice/bots/<bot_id>/SOUL.md
```

你可以通过 `soul_path` 自定义路径：

```yaml
bots:
  my_bot:
    soul_path: "SOUL.md"            # 相对于 alice_home（默认）
    # soul_path: "/path/to/custom/SOUL.md"  # 绝对路径
```

如果启动时文件不存在，Alice 会从 `prompts/SOUL.md.example` 写入内嵌模板。

## Frontmatter 字段

```yaml
---
image_refs:
  - refs/avatar.png
  - refs/signature.jpg
output_contract:
  hidden_tags:
    - reply_will
    - motion
  reply_will_tag: reply_will
  reply_will_field: reply_will
  motion_tag: motion
  suppress_token: "[[NO_REPLY]]"
---
```

| 字段 | 说明 |
|-----|-------------|
| `image_refs` | bot 可以引用的本地图片路径列表。路径相对于 `SOUL.md` 所在目录 |
| `output_contract.hidden_tags` | bot 回复中 Alice 在发送给飞书之前会剥离的标签 |
| `output_contract.reply_will_tag` | 标记 bot 回复意图的标签 |
| `output_contract.reply_will_field` | 标签内的字段名 |
| `output_contract.motion_tag` | 动作/动画提示的标签 |
| `output_contract.suppress_token` | bot 输出此 token 时，Alice 完全抑制回复 |

## 完整示例

```markdown
---
image_refs:
  - refs/avatar.png
output_contract:
  hidden_tags:
    - reply_will
    - motion
  reply_will_tag: reply_will
  reply_will_field: reply_will
  motion_tag: motion
  suppress_token: "[[NO_REPLY]]"
---

# 人格设定

你是 Alice，一个乐于助人的工程助手。你用简洁的中文夹杂英文技术术语进行交流。除非明确要求，否则不使用 emoji。

## 规则

- 代码片段保持在 30 行以内
- 优先解释思路再展示代码
- 永远不要道歉 — 直接解决问题
```

## SOUL.md 何时生效？

- **Chat 场景**：完整正文被添加到 prompt 前，Alice 解析 frontmatter 用于回复控制
- **Work 场景**：SOUL.md 被**刻意跳过**。Work 模式用于任务执行，不需要人格角色扮演

## 测试你的人格

1. 编辑 `SOUL.md`
2. 重启 Alice（多 bot 模式需要重启；单 bot 模式支持热重载）
3. 在 chat 场景中发消息 — bot 应体现更新后的人格
4. 如需重置对话，使用 `/clear`
