# ADR

ADR（Architecture Decision Record）用于记录关键架构决策。

当前已记录：

- [`0001-choose-v1-system-topology.md`](./0001-choose-v1-system-topology.md)：确定第一版采用 Go 单核心运行时 + 进程内 BUS + 独立 MCP 的系统拓扑
- [`0002-jsonl-snapshot-state-store.md`](./0002-jsonl-snapshot-state-store.md)：确定第一版状态存储采用 JSONL 事件日志加快照
- [`0003-outbox-and-mcp-idempotency.md`](./0003-outbox-and-mcp-idempotency.md)：确定所有外部副作用统一走 `outbox` 与协议级幂等
- [`0004-version-bound-audit-aggregation.md`](./0004-version-bound-audit-aggregation.md)：确定审核采用版本绑定、固定席位与租约聚合
- [`0005-evaluation-budget-and-cancellation.md`](./0005-evaluation-budget-and-cancellation.md)：确定评测独立于代码审核，预算触发硬熔断并传播取消

建议采用如下命名方式：

- `0001-<decision>.md`
- `0002-<decision>.md`

每条 ADR 建议至少包含：

- 背景
- 决策
- 备选方案
- 影响
- 后续事项

当设计边界发生变化时，优先补 ADR，再进入实现。
