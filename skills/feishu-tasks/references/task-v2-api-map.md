# Feishu Task v2 API Map

## Scope Separation

- `Alice任务` means connector-local automation tasks.
- `飞书任务` means official Feishu OpenAPI task resources.
- This skill is only for `飞书任务`.

## Token Rules

- `GET /open-apis/task/v2/tasks` (`list tasks`) is user-token only in official SDK metadata.
- Most other Task v2 endpoints accept both tenant token and user token.
- For "当前人的任务", prefer `user_access_token`.

## CRUD Coverage

### Task CRUD

- Create task: `POST /open-apis/task/v2/tasks`
- Get task: `GET /open-apis/task/v2/tasks/:task_guid`
- List tasks: `GET /open-apis/task/v2/tasks`
- Update task: `PATCH /open-apis/task/v2/tasks/:task_guid`
- Delete task: `DELETE /open-apis/task/v2/tasks/:task_guid`

### Task Member / Assignment

- Add assignees: `POST /open-apis/task/v2/tasks/:task_guid/add_members`
- Remove assignees: `POST /open-apis/task/v2/tasks/:task_guid/remove_members`

### Deadline / Due

- Update due time: `PATCH /open-apis/task/v2/tasks/:task_guid` with:
  - `task.due.timestamp` (ms)
  - `task.due.is_all_day` (boolean)
  - `update_fields: ["due"]`

### Tasklist CRUD

- Create tasklist: `POST /open-apis/task/v2/tasklists`
- Get tasklist: `GET /open-apis/task/v2/tasklists/:tasklist_guid`
- List tasklists: `GET /open-apis/task/v2/tasklists`
- Update tasklist: `PATCH /open-apis/task/v2/tasklists/:tasklist_guid`
- Delete tasklist: `DELETE /open-apis/task/v2/tasklists/:tasklist_guid`

### Tasklist Membership

- Add tasklist members: `POST /open-apis/task/v2/tasklists/:tasklist_guid/add_members`
- Remove tasklist members: `POST /open-apis/task/v2/tasklists/:tasklist_guid/remove_members`

### Tasklist Task Queries

- List tasks in a tasklist: `GET /open-apis/task/v2/tasklists/:tasklist_guid/tasks`

## Required User Requests -> API Mapping

- 检查当前你管理的任务:
  - `GET /task/v2/tasklists`
  - `GET /task/v2/tasklists/:tasklist_guid/tasks`
- 当前人的任务:
  - `GET /task/v2/tasks?type=my_tasks`
- 发布新的任务:
  - `POST /task/v2/tasks`
- assign 特定的人:
  - `POST /task/v2/tasks/:task_guid/add_members`
- 设置 deadline:
  - `PATCH /task/v2/tasks/:task_guid` (`update_fields=["due"]`)
- 创建和管理任务清单:
  - `POST/GET/PATCH/DELETE /task/v2/tasklists...`

## SDK Pointers

- Node SDK:
  - package: `@larksuiteoapi/node-sdk`
  - namespace examples:
    - `client.task.v2.task.create(...)`
    - `client.task.v2.task.addMembers(...)`
    - `client.task.v2.tasklist.create(...)`
- Go SDK:
  - module: `github.com/larksuite/oapi-sdk-go/v3`
  - namespace examples:
    - `client.Task.V2.Task.Create(...)`
    - `client.Task.V2.Task.AddMembers(...)`
    - `client.Task.V2.Tasklist.Create(...)`
