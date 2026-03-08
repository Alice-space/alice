# ADR

ADR（Architecture Decision Record）用于记录关键架构决策。

建议采用如下命名方式：

- `0001-choose-state-store.md`
- `0002-define-mcp-idempotency-key.md`
- `0003-snapshot-and-recovery-strategy.md`

每条 ADR 建议至少包含：

- 背景
- 决策
- 备选方案
- 影响
- 后续事项

当设计边界发生变化时，优先补 ADR，再进入实现。
