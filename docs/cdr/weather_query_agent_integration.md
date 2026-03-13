# 天气查询任务 Agent 接入日志（更新于 2026-03-13）

## 1. 本次样本
- 运行 worktree：`/home/lizhihao/alice`
- 运行时间：2026-03-13 15:53:30 +08:00 启动，2026-03-13 15:54:58 +08:00 完成
- 样本目录：`data/weather_case/20260313T075330Z`
- 用户输入：`查询上海明天天气`
- 对应日期：`明天 = 2026-03-14`
- `request_id = req_01KKK2ZCEPCHF24XC6Z9A4SNCB`
- `conversation_id = conv_weather_case_20260313T075330Z`

本次文档只以这次样本为准，主要证据文件：
- `data/weather_case/20260313T075330Z/logs/alice.app.jsonl`
- `data/weather_case/20260313T075330Z/logs/agent_context/20260313T075331Z_req_01kkk2zcepchf24xc6z9a4sncb_reception_reception_assessment.md`
- `data/weather_case/20260313T075330Z/logs/agent_context/20260313T075419Z_req_01kkk2zcepchf24xc6z9a4sncb_direct_answer_unknown.md`
- `data/weather_case/20260313T075330Z/artifacts/request.snapshot.json`
- `data/weather_case/20260313T075330Z/artifacts/eventlog.full.jsonl`
- `data/weather_case/20260313T075330Z/artifacts/events.snapshot.json`

复跑命令：

```bash
ALICE_WEATHER_PORT=18092 ./scripts/run_weather_case.sh
```

前提：
- `kimi` CLI 已完成 OAuth 登录
- 使用默认 `~/.kimi/config.toml`

## 2. 本次确认已成立的结论

### 2.1 嵌入式 MCP 已真正接入 Agent
`alice.app.jsonl` 里 Reception 和 Direct Answer 两次 agent 调用都带有：
- `mcp_enabled=true`
- `--mcp-config {"mcpServers":{"alice-tools":{"transport":"http","url":"http://127.0.0.1:<port>"}}}`

因此，本次样本确认 Agent 实际挂上了可用的嵌入式 MCP server。

### 2.2 Reception 的结构化输出解析已经闭环
这次 Reception 阶段实际调用了：
- `submit_promotion_decision`
- `SearchWeb`
- `submit_direct_answer`

关键变化是：`submit_promotion_decision` 返回的

```json
{"type":"promotion_decision","payload":{"intent_kind":"direct_query","risk_level":"low","external_write":false,"create_persistent_object":false,"async":false,"multi_step":false,"multi_agent":false,"approval_required":false,"budget_required":false,"recovery_required":false,"proposed_workflow_ids":null,"reason_codes":["weather_query","simple_lookup"],"confidence":0.95}}
```

已经被服务端正确解析并消费，而不是再落到 fallback。

直接证据：
- `reception_completed`：
  - `intent_kind = direct_query`
  - `risk_level = low`
  - `result = direct_answer`
  - `confidence = 0.95`
- `PromotionAssessed` 事件：
  - `result = direct_answer`
  - `reason_codes = ["weather_query","simple_lookup","direct_allowlist"]`
  - `confidence = 0.95`
- 本次运行中没有再出现 `no_structured_output_from_mcp`

这说明此前 “MCP 工具接上了，但 Reception 解析没闭环” 的问题，在当前代码和本次样本上已经解决。

### 2.3 Debug 模式下会自动产出每个 Agent 的 Markdown 上下文
本次样本自动生成了两份 Markdown artifact：
- `data/weather_case/20260313T075330Z/logs/agent_context/20260313T075331Z_req_01kkk2zcepchf24xc6z9a4sncb_reception_reception_assessment.md`
- `data/weather_case/20260313T075330Z/logs/agent_context/20260313T075419Z_req_01kkk2zcepchf24xc6z9a4sncb_direct_answer_unknown.md`

Markdown 中已经包含：
- metadata
- `system prompt`
- `task`
- `rendered prompt`
- `final text`
- 每次 `ToolCall` 的参数
- 每次 `ToolResult` 的返回文本
- `raw conversation`

因此，现在已经不需要再手工从 JSONL 抄 agent 上下文和工具调用过程。

### 2.4 两个 Agent 的完整输入输出上下文都能直接看到
本次两次 agent 调用都同时产出：
- `agent_execution_started`
- `agent_execution_finished`
- `agent_execution_transcript`
- `agent_execution_markdown_written`

并且可见字段包括：
- `rendered_prompt`
- `call_raw`
- `call_final_text`
- `call_tool_calls[].name`
- `call_tool_calls[].arguments`
- `call_tool_calls[].result_text`

实际工具调用如下：

Reception：
- `submit_promotion_decision`
- `SearchWeb`
- `submit_direct_answer`

Direct Answer：
- `SearchWeb`
- `submit_direct_answer`

`call_raw` 和 Markdown 中都能看到对应的 `ToolCall(...)` / `ToolResult(...)` 全过程。

### 2.5 最终回复写入的是最终答案，不再是原始 transcript
`ReplyRecorded.payload_ref` 本次确认为最终答复文本，开头为：

```text
reply://已为您查询到上海明天（3月14日）的天气预报：
```

没有再出现把整段 prompt / transcript 泄漏进最终 reply 的问题。

## 3. 端到端事件链路
本次请求事件序列如下：

1. `ExternalEventIngested`
2. `EphemeralRequestOpened`
3. `PromotionAssessed`
4. `AgentDispatchRecorded`
5. `ToolCallRecorded`
6. `AgentDispatchCompleted`
7. `ReplyRecorded`
8. `TerminalResultRecorded`
9. `RequestAnswered`

`request.snapshot.json` 对应状态：
- `status = Answered`
- `terminal_status = Answered`
- `promotion_decision = dec_01KKK30VYRXH7T22CWPFJ604GQ`
- `reply = rpl_01KKK321V6AKSRSQYPJH1EB3EF`
- `terminal_result = res_01KKK321V6AKSRSQYPJQ2D96V8`

## 4. 仍然可见但不影响闭环的问题

### 4.1 Reception skill 文件仍然缺失
本次运行仍有：

```text
agent_skill_load_failed: read skill file skills/reception-assessment/SKILL.md: open skills/reception-assessment/SKILL.md: no such file or directory
```

但这次已经确认：
- 不影响 MCP 接线
- 不影响结构化输出解析
- 不影响最终 direct answer 路径完成

### 4.2 BUS 事件层仍未细拆内部工具调用
事件流里仍只有一条抽象的：
- `ToolCallRecorded`

模型内部真实发生的：
- `SearchWeb`
- `submit_promotion_decision`
- `submit_direct_answer`

目前仍主要通过 `alice.app.jsonl` 和 Markdown artifact 审计，而不是通过更细粒度的 BUS 业务事件审计。

### 4.3 Direct Answer 结果的 `confidence` 仍走默认值
本次 `direct_answer_completed` 里：
- `confidence = 0.85`

这是因为 `DirectAnswerExecutor` 仍在没有显式 `confidence` 字段时使用默认值，而不是从 `submit_direct_answer` payload 中提取模型置信度。这个问题不影响主链路闭环，但会影响最终答复的置信度精度。

## 5. 结论
截至本次样本 `data/weather_case/20260313T075330Z`，当前真实状态是：

- 嵌入式 MCP 接线正常。
- 模型已经真实调用 Alice 工具。
- Reception 的 `promotion_decision` 结构化输出解析已经闭环，不再 fallback。
- Direct Answer 路径正常完成。
- Debug 模式下会自动生成每个 agent 的 Markdown 上下文文件。
- 每个 agent 的完整输入输出上下文和工具调用输出过程都能直接查看。
- `ReplyRecorded.payload_ref` 现在写入的是最终答案文本。

当前剩余问题主要是审计粒度和辅助手段问题，不再是主链路闭环问题。
