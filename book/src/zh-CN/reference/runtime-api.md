# Runtime HTTP API

Alice 在 `127.0.0.1` 上暴露一个本地认证的 HTTP API。Bundled skill、自动化脚本和薄运行时工具使用此 API。

## 认证

所有端点（`/healthz` 除外）都需要 Bearer token：

```
Authorization: Bearer <token>
```

Token 来自配置中的 `bots.<id>.runtime_http_token`，如果为空则自动生成。

## Base URL

默认：`http://127.0.0.1:7331`。多 bot 设置会自动递增：`7332`、`7333`……。

## 限制

- 请求体：最多 **1 MB**
- 认证速率限制：**每分钟 120 个请求**
- 列表端点：每次请求最多 **200 条**

---

## 健康检查

### `GET /healthz`

无需认证。

**响应** `200 OK`：
```json
{"status": "ok"}
```

---

## 消息

### `POST /api/v1/messages/image`

向当前对话发送图片。

**请求** `multipart/form-data`：

| 字段 | 类型 | 必填 | 说明 |
|-------|------|----------|-------------|
| `image` | file | 是 | 要上传的图片文件 |
| `caption` | string | 否 | 可选的说明文字 |

**响应** `200 OK`：
```json
{"message_id": "om_xxxxxxxxxxxxx"}
```

**错误：**
| 状态码 | 说明 |
|------|-------------|
| `400` | 图片文件无效或缺失 |
| `403` | `permissions.runtime_message` 已禁用 |
| `413` | 请求体超过 1 MB |

### `POST /api/v1/messages/file`

向当前对话发送文件。

**请求** `multipart/form-data`：

| 字段 | 类型 | 必填 | 说明 |
|-------|------|----------|-------------|
| `file` | file | 是 | 要上传的文件 |
| `filename` | string | 否 | 显示文件名（默认：原始文件名） |
| `caption` | string | 否 | 可选的说明文字 |

**响应** `200 OK`：
```json
{"message_id": "om_xxxxxxxxxxxxx"}
```

**错误：**
| 状态码 | 说明 |
|------|-------------|
| `400` | 文件无效或缺失 |
| `403` | `permissions.runtime_message` 已禁用 |
| `413` | 请求体超过 1 MB |

---

## 自动化任务

### `GET /api/v1/automation/tasks`

列出自动化任务。

**查询参数：**

| 参数 | 类型 | 默认值 | 说明 |
|-------|------|---------|-------------|
| `limit` | int | 50 | 每页条数（最大 200） |
| `offset` | int | 0 | 分页偏移 |
| `status` | string | — | 按状态筛选：`active`、`completed`、`cancelled` |

**响应** `200 OK`：
```json
[
  {
    "id": "task_abc123",
    "scope_type": "chat",
    "scope_id": "oc_xxxxxxxxxxxxx",
    "action": "send_text",
    "status": "active",
    "cron": "0 9 * * *",
    "created_at": "2025-01-15T09:00:00Z",
    "updated_at": "2025-01-15T09:00:00Z"
  }
]
```

### `POST /api/v1/automation/tasks`

创建自动化任务。

**请求** `application/json`：

| 字段 | 类型 | 必填 | 说明 |
|-------|------|----------|-------------|
| `scope_type` | string | 是 | `"chat"` 或 `"user"` |
| `scope_id` | string | 是 | 目标 ID（chat_id 或 user_id） |
| `action` | string | 是 | `"send_text"`、`"run_llm"` 或 `"run_workflow"` |
| `text` | string | `send_text` 时 | 消息文本 |
| `prompt` | string | `run_llm` 时 | LLM prompt |
| `cron` | string | 否 | 重复任务的 cron 表达式 |
| `run_at` | string | 否 | 一次性任务的 ISO 8601 时间戳 |

**响应** `201 Created`：
```json
{
  "id": "task_abc123",
  "scope_type": "chat",
  "scope_id": "oc_xxxxxxxxxxxxx",
  "action": "send_text",
  "status": "active",
  "cron": "0 9 * * *",
  "created_at": "2025-01-15T09:00:00Z"
}
```

**错误：**
| 状态码 | 说明 |
|------|-------------|
| `400` | 请求体无效或缺少必填字段 |
| `403` | `permissions.runtime_automation` 已禁用 |

### `GET /api/v1/automation/tasks/:taskID`

获取单个自动化任务。

**响应** `200 OK`：与列表项相同的结构。

**错误：**
| 状态码 | 说明 |
|------|-------------|
| `404` | 任务未找到 |

### `PATCH /api/v1/automation/tasks/:taskID`

更新自动化任务。发送 JSON merge-patch，包含要更新的字段。

**请求** `application/json`：
```json
{"status": "cancelled"}
```

可更新字段：`status`、`cron`、`run_at`、`text`、`prompt`。

**响应** `200 OK`：更新后的任务对象。

**错误：**
| 状态码 | 说明 |
|------|-------------|
| `400` | 无效更新 |
| `403` | `permissions.runtime_automation` 已禁用 |
| `404` | 任务未找到 |

### `DELETE /api/v1/automation/tasks/:taskID`

删除自动化任务。

**响应** `204 No Content`

**错误：**
| 状态码 | 说明 |
|------|-------------|
| `403` | `permissions.runtime_automation` 已禁用 |
| `404` | 任务未找到 |

---

## Goal

### `GET /api/v1/goal`

获取对话作用域的当前活跃 goal。

**响应** `200 OK`：
```json
{
  "id": "goal_xyz",
  "description": "Review PR #42",
  "status": "in_progress",
  "created_at": "2025-01-15T10:00:00Z"
}
```

**响应** `204 No Content`：无活跃 goal。

### `POST /api/v1/goal`

为对话作用域创建新 goal。

**请求** `application/json`：
```json
{"description": "Review PR #42"}
```

**响应** `201 Created`：创建的 goal 对象。

### `POST /api/v1/goal/pause`

暂停活跃 goal。

**响应** `200 OK`。

### `POST /api/v1/goal/resume`

恢复已暂停的 goal。

**响应** `200 OK`。

### `POST /api/v1/goal/complete`

将活跃 goal 标记为完成。

**响应** `200 OK`。

### `DELETE /api/v1/goal`

删除活跃 goal。

**响应** `204 No Content`。

---

## 通用错误响应格式

所有错误遵循此格式：

```json
{
  "error": "Human-readable error description"
}
```

HTTP 状态码按惯例使用：`400` 表示客户端错误，`403` 表示权限不足，`404` 表示未找到，`413` 表示载荷过大，`429` 表示速率限制，`500` 表示内部错误。
