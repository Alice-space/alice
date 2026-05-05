# 使用内置命令

Alice 提供多个斜杠命令，这些命令绕过 LLM，由连接器直接处理。所有命令在群聊和私聊中均可使用。

## `/help`

显示内置命令帮助卡片，列出所有可用命令。

```
/help
```

## `/status`

显示状态卡片，包含：
- 总计 session 和用量计数器
- 活动的自动化任务
- 当前 LLM 后端和 session 详情

```
/status
```

## `/clear`

重置当前 `chat` 场景的 session。下一条消息将以全新对话开始，不带有之前的上下文。

```
/clear
```

> 仅影响 `chat` 场景。`work` 场景是基于话题的，话题结束时自然重置。

## `/stop`

立即取消当前活跃 session 正在运行的 LLM 调用。

```
/stop
```

当 agent 陷入循环或运行时间过长时使用此命令。Bot 会确认停止，并恢复接受新消息。

## `/session`

将飞书 work 话题绑定到已有的后端 session。适用于重启后恢复长时间运行的任务。

```
/session <backend-session-id>
/session <backend-session-id> Continue the review
```

- 不带指令：绑定 session，不调用 LLM
- 带指令：绑定 session 并立即用该指令调用 LLM

> 仅在 `work` 场景话题中有效。

## `/cd`、`/ls`、`/pwd`

查看和更改当前 work session 的工作目录：

```
/pwd               # 显示当前目录
/ls                # 列出文件
/ls internal/      # 列出子目录中的文件
/cd /tmp/build     # 更改目录
```

这些命令仅影响 `work` session。目录更改在整个 session 期间持续有效。

## 命令优先级

当消息以 `/` 开头时，Alice 在路由到 LLM 之前先检查内置命令：

1. 匹配内置命令 → 直接处理
2. 不匹配 → 路由到场景（由 LLM 处理）

要强制将以 `/` 开头的消息发送给 LLM，请在前面加空格或使用 work 触发词：

```
 /some-custom-command     # 斜杠前的空格 → LLM 路径
@Alice #work /some-cmd    # Work 触发词 → LLM 路径
```
