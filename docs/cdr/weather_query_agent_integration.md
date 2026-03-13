# 天气查询任务 Agent 接入说明（以 Agent 为中心）

## 1. 目的与范围
本文描述 Alice 在“查询天气”场景下的 Agent 接入情况，重点覆盖：
- Agent 输入上下文（从 BUS/路由进入 Agent 的字段）
- Agent 返回响应（结构化决策与自然语言回复）
- ToolCall 与 MCP 挂接状态
- 与事件日志的一一对应关系

基线样本使用最近一次完整运行：`data/weather_case/20260311T145708Z`。

## 2. 运行基线
本次样本配置（`data/weather_case/20260311T145708Z/config.yaml`）中的 Agent 关键项：
- `agent.kimi_executable = kimi`
- `agent.enable_direct_answer = true`
- `agent.skills_dir = skills`
- `agent.timeout = 90s`
- `agent.max_steps = 8`
- `mcp.domains = {}`（空）

对应启动日志可见 LocalAgent 与 LLMReception 已注入（见 `server.stdout.log`）。

## 3. Agent 拓扑（天气查询）
天气查询路径中，涉及两个 Agent 层组件：

1. Reception Agent（策略评估）
- 代码入口：`internal/policy/llm_reception.go`
- 作用：把外部输入评估为 `PromotionDecision`
- 产出：`PromotionAssessed` 事件

2. Direct Answer Agent（直接回答执行）
- 代码入口：`internal/agent/direct_answer.go` + `internal/agent/local.go`
- 作用：在 direct-answer 路径中生成最终回复
- 产出：`AgentDispatchRecorded` / `ToolCallRecorded` / `ReplyRecorded` 等事件

## 4. Reception Agent 视角
### 4.1 输入上下文
Reception 的输入来自 `domain.ReceptionInput`，本案例关键字段：
- `event.source_kind = direct_input`
- `event.transport_kind = cli`
- `event.source_ref = 查询上海明天天气`
- `event.conversation_id = conv_weather_case_20260311T145708Z`
- `event.thread_id = root`
- `route_snapshot.matched_by = new_request`
- `route_snapshot.route_keys = [conversation:direct_input:conv_weather_case_20260311T145708Z:root]`

日志对应：
- `reception_started`
- `reception_completed`

### 4.2 提示词与技能挂接
Reception 执行时使用：
- `Skill = reception-assessment`
- `SystemPrompt = reception_assessment_system`
- `TaskPrompt = reception_assessment_task`
- 约束：`ReadOnly=true`

本次运行存在告警：
- `failed to load skill reception-assessment`（`skills/reception-assessment/SKILL.md` 不存在）

### 4.3 返回与回退
本次运行中，Reception 未拿到 MCP 结构化输出，触发：
- `no_structured_output_from_mcp`
- 回退到 `simpleDecision`，得到：
  - `intent_kind = direct_query`
  - `result = direct_answer`
  - `reason_codes = [general_query, direct_allowlist]`
  - `confidence = 0.8`

事件对应：
- `PromotionAssessed`（HLC `#0003`）

## 5. Direct Answer Agent 视角
### 5.1 输入上下文
BUS 在 `appendDirectAnswerEvents` 里构造并下发：
- `request_id = req_01KKEPDMES034Y903QGPAWNGK3`
- `event_id = evt_01KKEPDMES034Y903QGMSGK2D9`
- `user_input = 查询上海明天天气`
- `intent_kind = direct_query`
- `skill = ""`（`directAnswerSkillForIntent` 对 `direct_query` 返回空）

并先落盘：
- `AgentDispatchRecorded`，其中
  - `requested_role = helper`
  - `goal = direct_answer:direct_query`
  - `allowed_tools = [local_agent]`
  - `write_scope_ref = read_only`

### 5.2 LocalAgent 执行参数
LocalAgent 调 kimi CLI 的参数核心为：
- `--print`
- `--yolo`
- `--work-dir .`
- `--max-steps-per-turn 8`
- `--prompt <构造后的任务提示>`
- `--skills-dir skills`

说明：本次未注入 `MCPServer`，因此不会带 `--mcp-config`。

### 5.3 返回响应
Direct Answer 执行结束后：
- 日志：`direct_answer_started` -> `direct_answer_completed`
- `ToolCallRecorded`：
  - `tool_or_mcp = direct_answer`
  - `request_ref = tool://direct_answer`
  - `response_ref = result://direct_answer`
  - `status = success`
- `ReplyRecorded`：`reply_channel = direct_input`
- `TerminalResultRecorded`：`final_status = Answered`

## 6. MCP 与 ToolCall 挂接结论
### 6.1 MCP 挂接状态（本次样本）
- 配置层：`mcp.domains = {}`
- Agent 层：`LocalAgent.MCPServer = nil`
- 结果：本次没有真实 MCP server tool-call 往返，仅有 BUS 侧的抽象 `ToolCallRecorded`。

### 6.2 ToolCall 记录状态（本次样本）
- `ToolCallRecorded` 已完整落盘（HLC `#0005`）
- Tool 语义是 direct-answer 执行，不是外部 MCP domain 调用

## 7. 事件时间线（按 HLC）
来自 `artifacts/event_sequence.tsv`：
1. `ExternalEventIngested` (`#0001`)
2. `EphemeralRequestOpened` (`#0002`)
3. `PromotionAssessed` (`#0003`)
4. `AgentDispatchRecorded` (`#0004`)
5. `ToolCallRecorded` (`#0005`)
6. `AgentDispatchCompleted` (`#0006`)
7. `ReplyRecorded` (`#0007`)
8. `TerminalResultRecorded` (`#0008`)
9. `RequestAnswered` (`#0009`)

## 8. 对外响应（CLI）
`cli.submit.json` 返回：
- `accepted = true`
- `event_id = evt_01KKEPDMES034Y903QGMSGK2D9`
- `request_id = req_01KKEPDMES034Y903QGPAWNGK3`
- `route_target_kind = request`
- `commit_hlc = 2026-03-11T14:58:00.378553308Z#0009`

## 9. 关键观察
1. Agent 链路完整：Reception 与 Direct Answer 都实际执行并可审计。
2. 本样本中 Reception 走了“无结构化输出回退”路径，但最终业务路径仍符合天气直答预期。
3. 当前天气案例是“Agent + 内建 direct-answer ToolCall 抽象”路径，不是“真实 MCP domain 查询”路径。
4. 若要验证完整 MCP 工具闭环，需要在 `mcp.domains` 和 `LocalAgent.MCPServer` 层补齐配置。

## 10. 附件索引
- `data/weather_case/20260311T145708Z/config.yaml`
- `data/weather_case/20260311T145708Z/artifacts/cli.submit.json`
- `data/weather_case/20260311T145708Z/artifacts/eventlog.full.jsonl`
- `data/weather_case/20260311T145708Z/artifacts/event_types.summary.txt`
- `data/weather_case/20260311T145708Z/artifacts/event_sequence.tsv`
- `data/weather_case/20260311T145708Z/logs/alice.app.jsonl`
- `data/weather_case/20260311T145708Z/logs/server.stdout.log`
