# 编写 Bundled Skill

Bundled skill 扩展了 Alice，提供基于脚本的工具来调用 Runtime HTTP API。本指南将教你如何创建。

## Skill 结构

一个 bundled skill 是 `skills/` 下的一个目录：

```
skills/my-skill/
├── SKILL.md           # Skill 文档
├── scripts/
│   └── my-skill.sh    # 可执行脚本
└── agents/
    └── openai.yaml    # OpenAI agent 配置（可选）
```

## 第 1 步：创建目录

在 Alice 源码树的 `skills/` 下，或在 `${ALICE_HOME}/skills/` 下（本地开发），创建你的 skill。

## 第 2 步：编写 SKILL.md

`SKILL.md` 为人类和 LLM agent 提供 skill 文档：

```markdown
# my-skill

将活跃自动化任务的每日摘要发送到指定的飞书群聊。

## 用途

此 skill 由自动化系统触发。它从 runtime API 读取所有活跃任务，并发送格式化的摘要卡片。

## 环境

需要设置 `ALICE_RUNTIME_API_BASE_URL` 和 `ALICE_RUNTIME_API_TOKEN`。
```

## 第 3 步：编写脚本

脚本以子进程方式运行。Alice 注入以下环境变量：

| 变量 | 说明 |
|----------|-------------|
| `ALICE_RUNTIME_API_BASE_URL` | Runtime API 的 base URL（如 `http://127.0.0.1:7331`） |
| `ALICE_RUNTIME_API_TOKEN` | API 认证的 Bearer token |
| `ALICE_RUNTIME_BIN` | `alice` 二进制路径 |
| `ALICE_RECEIVE_ID_TYPE` | 接收目标的类型（如 `chat_id`） |
| `ALICE_RECEIVE_ID` | 接收目标的 ID |
| `ALICE_SOURCE_MESSAGE_ID` | 触发消息的 ID（如适用） |
| `ALICE_ACTOR_USER_ID` | 交互者的飞书 user ID |
| `ALICE_ACTOR_OPEN_ID` | 交互者的飞书 open ID |
| `ALICE_CHAT_TYPE` | 对话类型：`group` 或 `p2p` |
| `ALICE_SESSION_KEY` | 当前对话的规范 session key |

### 示例脚本

```bash
#!/usr/bin/env bash
set -euo pipefail

# 获取所有活跃任务
TASKS=$(curl -sS \
  -H "Authorization: Bearer ${ALICE_RUNTIME_API_TOKEN}" \
  "${ALICE_RUNTIME_API_BASE_URL}/api/v1/automation/tasks?status=active")

# 计数和格式化
COUNT=$(echo "$TASKS" | jq '. | length')
echo "Active tasks: $COUNT"
```

赋予执行权限：
```bash
chmod +x skills/my-skill/scripts/my-skill.sh
```

## 第 4 步：注册 Skill

将你的 skill 添加到 bot 的允许 skill 列表中：

```yaml
bots:
  my_bot:
    permissions:
      allowed_skills: ["alice-message", "alice-scheduler", "my-skill"]
```

## Skill 可用的 Runtime API 端点

Skill 主要使用以下端点：

| 端点 | 方法 | 用途 |
|----------|--------|---------|
| `/api/v1/messages/image` | POST | 向对话发送图片 |
| `/api/v1/messages/file` | POST | 向对话发送文件 |
| `/api/v1/automation/tasks` | GET | 列出自动化任务 |
| `/api/v1/automation/tasks` | POST | 创建自动化任务 |
| `/api/v1/automation/tasks/:id` | GET/PATCH/DELETE | 管理特定任务 |

所有请求都需要 `Authorization: Bearer <token>` 头部。

## 权限

Skill 在 bot 的运行时权限下运行：

```yaml
permissions:
  runtime_message: true       # 允许通过 API 发送消息
  runtime_automation: true    # 允许管理自动化任务
```

如果某权限被禁用，对应的 API 端点将返回 `403 Forbidden`。

## 内置 Skill 参考

Alice 内置了两个 bundled skill：

- **alice-message**：通过 runtime API 发送富文本消息和附件
- **alice-scheduler**：从飞书对话中管理自动化任务

研究它们的源码（`skills/alice-message/` 和 `skills/alice-scheduler/`），了解 skill 结构和 API 使用的实际示例。
