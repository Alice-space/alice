# Internal

用于存放仓库私有实现包。

建议后续围绕以下方向拆分：

- `internal/app/`：启动装配与生命周期管理
- `internal/bus/`：事件流转、状态推进、投影与恢复
- `internal/store/`：事件日志、快照、读模型
- `internal/agent/`：Planner、Coding、Audit、Evaluation 等运行时集成
- `internal/mcp/`：MCP 客户端与调用编排
- `internal/platform/`：配置、日志、观测、鉴权等通用基础能力

未确认需要外部复用前，默认都应放在 `internal/`。
