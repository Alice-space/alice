# 排错指南

运行 Alice 的常见问题和解决方案。

## Bot 在群聊中不响应

**检查场景路由：**
- 确认 `group_scenes.chat.enabled` 为 `true`
- 如果两个场景都被禁用，检查 `trigger_mode`（应为 `at` 或 `prefix`）

**检查 bot 身份：**
- Bot 的 `open_id` 现在在启动时自动获取 — 无需手动配置 `feishu_bot_open_id`
- 确认 `feishu_app_id` 和 `feishu_app_secret` 是否正确

**检查日志：**
```bash
# 查找 WebSocket 连接状态
grep "long connection" ~/.alice/log/*.log
# 查找认证错误
grep "error" ~/.alice/log/*.log | head -20
```

## Work 模式从不触发

- 确认 `group_scenes.work.enabled` 为 `true`
- 确认 `trigger_tag` 已设置（如 `"#work"`）
- 消息必须包含 `@BotName #work ...` — @mention 和触发标签缺一不可
- 触发标签必须出现在同一条消息中 @mention 之后

## 模型或推理级别不正确

- 检查 `llm_profiles` 中 `provider`、`model` 和专属字段是否正确
- 确认场景指向正确的 profile key：
  ```yaml
  group_scenes:
    work:
      llm_profile: "work"  # 必须与 llm_profiles 下的 key 匹配
  ```
- 直接运行 provider CLI 验证认证：
  ```bash
  codex --version
  claude --version
  ```

## Skill 无法发送附件或管理任务

**检查权限：**
```yaml
permissions:
  runtime_message: true
  runtime_automation: true
```

**检查 API 连通性：**
```bash
# 在运行 Alice 的机器上执行
curl -s -H "Authorization: Bearer <token>" http://127.0.0.1:7331/healthz
# 应返回 {"status":"ok"}
```

Runtime HTTP API 绑定在 `runtime_http_addr` 指定的地址（默认 `127.0.0.1:7331`）。多 bot 设置会自动递增端口。

## 配置更改不生效

- **多 bot 模式**：配置热重载被禁用。需重启 Alice。
- **单 bot 模式**：支持部分热重载，但并非所有配置项都会被监听。
- 配置更改后务必检查日志：
  ```bash
  grep "config" ~/.alice/log/*.log | tail -5
  ```

## WebSocket 连接错误

如果日志中出现连接失败：

1. 确认飞书开放平台中已启用长连接模式
2. 确认应用已发布并通过审批
3. 确认能访问 `open.feishu.cn`（或 Lark 用户的 `open.larksuite.com`）的网络
4. 确认 Lark（国际版）用户已正确设置 `feishu_base_url`：
   ```yaml
   feishu_base_url: "https://open.larksuite.com"
   ```

## Provider CLI 未找到

Alice 默认在 `$PATH` 中查找 CLI 二进制文件。如果未找到：

1. 指定绝对路径：
   ```yaml
   llm_profiles:
     chat:
       command: "/usr/local/bin/opencode"
   ```
2. 或在 bot 的 env 中扩展 `$PATH`：
   ```yaml
   env:
     PATH: "/home/user/.local/bin:/usr/local/bin:/usr/bin:/bin"
   ```

## LLM 运行无限挂起

- 检查 LLM profile 中的 `timeout_secs`（默认：48 小时）
- 在飞书中使用 `/stop` 取消正在运行的 session
- 检查日志中 provider 专属的错误：
  ```bash
  grep -E "timeout|cancelled|killed" ~/.alice/log/*.log
  ```
- 对于 Codex，检查 `codex_idle_timeout_secs` 设置

## 日志没有有用信息

将日志级别提高到 `debug`：

```yaml
log_level: "debug"
```

重启 Alice。Debug 模式包含：
- 每次运行的 provider 和 agent 名称
- Thread/session ID
- 渲染后的输入 prompt
- 观察到的工具调用活动
- 最终输出或错误

> 警告：Debug 日志可能包含完整渲染后的 prompt，包括 SOUL.md 内容。
