---
name: alice-message
description: 通过 Alice 本地 runtime HTTP API 向当前会话发送图片或文件等附件。适用于在当前聊天里发送本地文件/图片，或复用已有飞书 `image_key` / `file_key`。
---

# Alice 消息发送

使用 `scripts/alice-message.sh` 把附件发送回当前 Alice 会话。脚本会自动读取当前会话上下文，并由 Alice 自动路由普通回复/话题回复。纯文本消息不走这个 skill，由程序主链路直接转发。

维护约束：当前会话里 `.agents/skills/...` 的已安装 skill 副本来自 Alice 安装/更新流程，不应直接修改；需要变更 skill 时，应修改 Alice 仓库里的 `alice/skills/...` 源文件，再通过安装流程同步进去。

## 常用命令

- 发送本地图片（先上传后发送）：
  `scripts/alice-message.sh image --path /abs/path/image.png --caption '最新截图'`
- 发送已有飞书图片：
  `scripts/alice-message.sh image --image-key img_v3_xxx`
- 发送本地文件（先上传后发送）：
  `scripts/alice-message.sh file --path /abs/path/report.pdf --file-name report.pdf --caption '请查收'`
- 发送已有飞书文件：
  `scripts/alice-message.sh file --file-key file_v3_xxx`

## 工作流

1. 只有发送图片、文件等附件时才用这个 skill；纯文本回复直接正常输出即可。
2. 已有本地绝对路径时用 `image --path` 或 `file --path`。
3. 资源已上传过时优先复用 `--image-key` / `--file-key`。
4. 不要要求用户提供 `receive_id_type`、`receive_id`、`source_message_id`；由 Alice 根据当前会话自动解析。

## 回复模式

- 说明发送了哪种类型（图片 / 文件）。
- 需要时给出使用的本地路径或飞书 key。
- 如果因路径不在当前会话资源根目录导致上传失败，要明确说明并停止继续重试。
