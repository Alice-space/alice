# Runtime HTTP API

Alice exposes a local authenticated HTTP API on `127.0.0.1`. Bundled skills, automation scripts, and thin runtime tools use this API.

## Authentication

All endpoints (except `/healthz`) require a Bearer token:

```
Authorization: Bearer <token>
```

The token is from `bots.<id>.runtime_http_token` in config, or auto-generated if empty.

## Base URL

Default: `http://127.0.0.1:7331`. Multi-bot setups auto-increment: `7332`, `7333`, etc.

## Limits

- Request body: **1 MB** maximum
- Auth rate limit: **120 requests per minute**
- List endpoints: **200 items** maximum per request

---

## Health

### `GET /healthz`

No authentication required.

**Response** `200 OK`:
```json
{"status": "ok"}
```

---

## Messages

### `POST /api/v1/messages/image`

Send an image to the current conversation.

**Request** `multipart/form-data`:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `image` | file | Yes | Image file to upload |
| `caption` | string | No | Optional caption text |

**Response** `200 OK`:
```json
{"message_id": "om_xxxxxxxxxxxxx"}
```

**Errors:**
| Code | Description |
|------|-------------|
| `400` | Invalid or missing image file |
| `403` | `permissions.runtime_message` is disabled |
| `413` | Request body exceeds 1 MB |

### `POST /api/v1/messages/file`

Send a file to the current conversation.

**Request** `multipart/form-data`:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `file` | file | Yes | File to upload |
| `filename` | string | No | Display filename (default: original filename) |
| `caption` | string | No | Optional caption text |

**Response** `200 OK`:
```json
{"message_id": "om_xxxxxxxxxxxxx"}
```

**Errors:**
| Code | Description |
|------|-------------|
| `400` | Invalid or missing file |
| `403` | `permissions.runtime_message` is disabled |
| `413` | Request body exceeds 1 MB |

---

## Automation Tasks

### `GET /api/v1/automation/tasks`

List automation tasks.

**Query Parameters:**

| Param | Type | Default | Description |
|-------|------|---------|-------------|
| `limit` | int | 50 | Items per page (max 200) |
| `offset` | int | 0 | Pagination offset |
| `status` | string | — | Filter by status: `active`, `completed`, `cancelled` |

**Response** `200 OK`:
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

Create an automation task.

**Request** `application/json`:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `scope_type` | string | Yes | `"chat"` or `"user"` |
| `scope_id` | string | Yes | Target ID (chat_id or user_id) |
| `action` | string | Yes | `"send_text"`, `"run_llm"`, or `"run_workflow"` |
| `text` | string | For `send_text` | Message text |
| `prompt` | string | For `run_llm` | LLM prompt |
| `cron` | string | No | Cron expression for recurring tasks |
| `run_at` | string | No | ISO 8601 timestamp for one-shot tasks |

**Response** `201 Created`:
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

**Errors:**
| Code | Description |
|------|-------------|
| `400` | Invalid request body or missing required fields |
| `403` | `permissions.runtime_automation` is disabled |

### `GET /api/v1/automation/tasks/:taskID`

Get a single automation task.

**Response** `200 OK`: Same schema as list item.

**Errors:**
| Code | Description |
|------|-------------|
| `404` | Task not found |

### `PATCH /api/v1/automation/tasks/:taskID`

Update an automation task. Send a JSON merge-patch with fields to update.

**Request** `application/json`:
```json
{"status": "cancelled"}
```

Updatable fields: `status`, `cron`, `run_at`, `text`, `prompt`.

**Response** `200 OK`: Updated task object.

**Errors:**
| Code | Description |
|------|-------------|
| `400` | Invalid update |
| `403` | `permissions.runtime_automation` is disabled |
| `404` | Task not found |

### `DELETE /api/v1/automation/tasks/:taskID`

Delete an automation task.

**Response** `204 No Content`

**Errors:**
| Code | Description |
|------|-------------|
| `403` | `permissions.runtime_automation` is disabled |
| `404` | Task not found |

---

## Goal

### `GET /api/v1/goal`

Get the current active goal for the conversation scope.

**Response** `200 OK`:
```json
{
  "id": "goal_xyz",
  "description": "Review PR #42",
  "status": "in_progress",
  "created_at": "2025-01-15T10:00:00Z"
}
```

**Response** `204 No Content`: No active goal.

### `POST /api/v1/goal`

Create a new goal for the conversation scope.

**Request** `application/json`:
```json
{"description": "Review PR #42"}
```

**Response** `201 Created`: Created goal object.

### `POST /api/v1/goal/pause`

Pause the active goal.

**Response** `200 OK`.

### `POST /api/v1/goal/resume`

Resume a paused goal.

**Response** `200 OK`.

### `POST /api/v1/goal/complete`

Mark the active goal as completed.

**Response** `200 OK`.

### `DELETE /api/v1/goal`

Delete the active goal.

**Response** `204 No Content`.

---

## Common Error Response Format

All errors follow this format:

```json
{
  "error": "Human-readable error description"
}
```

HTTP status codes are used conventionally: `400` for client errors, `403` for permission denied, `404` for not found, `413` for payload too large, `429` for rate limited, `500` for internal errors.
