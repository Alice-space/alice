---
name: alice-message
description: 向当前 session 发送图片/文件附件
---

# Alice Message

发送图片或文件到当前会话。纯文本直接输出即可，不用此 skill。

## 命令

**图片**：
- `scripts/alice-message.sh image --path /path/to/img.png` - 本地文件
- `scripts/alice-message.sh image --image-key img_v3_xxx` - 已有飞书图片

**文件**：
- `scripts/alice-message.sh file --path /path/to/report.pdf` - 本地文件
- `scripts/alice-message.sh file --file-key file_v3_xxx` - 已有飞书文件

## 规则

1. 已有 key 时优先复用 `--image-key` / `--file-key`
2. 不要向用户索要 `receive_id_type`、`receive_id`、`source_message_id`（由 session 自动注入）
3. 文件不存在/不可读时报告错误，不要重试
