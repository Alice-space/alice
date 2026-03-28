# CodeArmy 隔离环境 Debug 手册

这份文档总结一个高效的 Alice / CodeArmy 调试方法：

- 用最新源码临时编译 `alice` / `alice-headless`
- 准备全新的 `ALICE_HOME`
- 在本地小 repo 上跑一条真实 campaign
- 同时观察 campaign repo、runtime automation 状态和 runtime 日志

这个方法特别适合排查下面几类问题：

- `campaign_role_defaults` / `llm_profiles` 是否真的落到 dispatch task
- planner / planner reviewer / executor / reviewer 的 workflow 是否按预期推进
- repo-first 产物是否和 runtime summary 一致
- skill 模板、prompt 模板、runtime reconcile 是否真的联通

## 推荐流程

1. 在 `workspace/.tmp/` 下创建一套临时目录，例如 `codearmy-e2e-rerunN/`。
2. 用最新源码编译临时 `alice` 和 `alice-headless`，不要复用系统里旧 binary。
3. 准备新的 `ALICE_HOME`，至少保证下面几项隔离：
   - 独立 `config.yaml`
   - 独立 `runtime_http_addr`
   - 独立 `runtime_http_token`
   - 独立 bot runtime state / automation db / campaign db
4. 准备一个足够小但完整的 source repo。
   - 推荐用 Rust terminal calculator、最小 Cargo skeleton、最小 Go CLI 这类项目。
   - 目标不是业务复杂，而是能覆盖 planning / review / execution / repo artifact。
5. 用 `alice-headless` 启动独立 runtime，并显式设置新的 `ALICE_HOME`。
6. 用 `alice-code-army.sh bootstrap` 创建 campaign，让 planner 正式走 workflow。
7. 在每个关键节点同时检查三处：
   - campaign repo：`campaign.md`、`plans/`、`phases/`、`reports/live-report.md`
   - runtime API：`alice runtime campaigns get`、`alice runtime automation list/get`
   - runtime log：`<ALICE_HOME>/log/YYYY-MM-DD.log`
8. 遇到状态不推进时，优先判断：
   - repo 产物还没写出来
   - repo 产物已写出来，但 verdict / summary 还没被下一次 reconcile 应用
   - task 其实跑挂了，只是 runtime summary 还没刷新

## 会话隔离建议

如果只是本地验证 workflow，不希望向真实飞书会话发消息，可以用假的会话路由环境：

- `ALICE_MCP_RECEIVE_ID_TYPE`
- `ALICE_MCP_RECEIVE_ID`
- `ALICE_MCP_CHAT_TYPE`
- `ALICE_MCP_SESSION_KEY`
- `ALICE_MCP_ACTOR_OPEN_ID`

这样做的效果：

- campaign / automation scope 仍然可以正常建立
- 但通知发送会失败，`last_result` 里常见 `invalid receive_id`

这类错误在隔离测试里通常是预期噪音，不代表 CodeArmy workflow 自身坏了。

## 这次实跑得到的经验

### 1. 只改源码模板还不够

如果你改了 `alice/skills/...` 下的 skill 源文件，运行时真正使用的安装副本可能还是旧的。

需要显式执行：

```bash
alice skills sync --alice-home ~/.alice --skill alice-code-army
```

否则你会看到：

- prompt 已经是新逻辑
- 但 `alice-code-army.sh`、campaign repo 模板、安装后的 `SKILL.md` 仍然是旧版本

### 2. 临时 runtime 命令必须显式指定 API 环境

如果当前 shell 里已经有主 Alice 会话注入的：

- `ALICE_RUNTIME_API_BASE_URL`
- `ALICE_RUNTIME_API_TOKEN`

那么你即使切了新的 `ALICE_HOME`，`alice runtime ...` 也可能打到主实例，而不是临时实例。

隔离调试时建议每条命令都显式设置：

```bash
ALICE_RUNTIME_API_BASE_URL=http://127.0.0.1:<temp-port>
ALICE_RUNTIME_API_TOKEN=<temp-token>
```

### 3. 旧 campaign 不能代表新模板

如果一个 campaign 是在 skill sync 之前创建的，它已经把旧模板内容 materialize 到 repo 里了。

所以：

- 修了模板以后，老 campaign 不会自动变成新模板
- 要验证“最新模板 + 最新代码”的真实效果，应该重新起一个全新 campaign

### 4. prompt 约束不等于机器校验

planner 即使在 proposal 里写了“我已经修复 X”，也不一定真的把 `master-plan.md`、`phase.md`、`task.md` 同步改完。

所以隔离调试时一定要交叉对照：

- `plans/proposals/round-XXX-plan.md`
- `plans/merged/master-plan.md`
- `phases/Pxx/phase.md`
- `phases/Pxx/tasks/Txxx/task.md`

如果这四层不一致，说明还需要更强的 repo-lint / consistency check，而不能只靠 prompt。

## 建议的最小检查清单

- 模型配置：
  - dispatch task 的 `provider` / `model` / `profile` 是否符合 `config.yaml`
- planning：
  - proposal、master plan、phase docs、task packages 是否同一轮一致
- review：
  - `concern` / `blocking` 的使用是否符合预期
- reconcile：
  - review 文件写出后，下一次 reconcile 是否正确推进 `plan_round` / `plan_status`
- execution：
  - executor 产物、review 文件、judge verdict 是否能闭环
- skill 分发：
  - 安装副本是否已经 sync 到最新 embedded skill

## 推荐结论格式

每次隔离调试结束后，建议至少记录：

- 使用的 commit / binary
- 临时 `ALICE_HOME`
- campaign id
- source repo 路径
- 当前停在哪个阶段
- 已确认修复的问题
- 仍然存在的问题
- 哪些是 workflow bug，哪些只是隔离环境噪音
