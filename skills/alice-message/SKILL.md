---
name: alice-message
description: Send text, images, or files into the current Alice conversation through Alice's local runtime HTTP API. Use when the user asks to reply in the current chat, send a follow-up message, upload and send a local image/file, or reuse an existing Feishu `image_key` / `file_key` without manually specifying receive IDs.
---

# Alice Message

Use `scripts/alice-message.sh` to send content back into the current Alice conversation. The script reads the current session context automatically and lets Alice route replies/thread replies on its own.

## Commands

- Send text:
  `scripts/alice-message.sh text '已完成，见附件。'`
- Send an uploaded local image:
  `scripts/alice-message.sh image --path /abs/path/image.png --caption '最新截图'`
- Send an existing Feishu image:
  `scripts/alice-message.sh image --image-key img_v3_xxx`
- Send an uploaded local file:
  `scripts/alice-message.sh file --path /abs/path/report.pdf --file-name report.pdf --caption '请查收'`
- Send an existing Feishu file:
  `scripts/alice-message.sh file --file-key file_v3_xxx`

## Workflow

1. Prefer `text` for plain follow-up replies.
2. Use `image --path` or `file --path` when you have a local absolute path in the current session resource directory.
3. Reuse `--image-key` / `--file-key` when the asset is already uploaded.
4. Do not ask for `receive_id_type`, `receive_id`, or `source_message_id`; Alice resolves the target from the current conversation.

## Reply Pattern

- State whether you sent text, image, or file.
- Mention the local path or Feishu key you used when relevant.
- If upload fails because the path is outside the session resource root, say that explicitly and stop.
