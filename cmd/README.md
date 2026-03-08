# cmd

用于存放可执行程序入口。

建议后续按“一目录一个二进制”组织，例如：

- `cmd/alice/`：核心运行时主程序
- `cmd/mcp-github/`：GitHub MCP
- `cmd/mcp-gitlab/`：GitLab MCP
- `cmd/mcp-cluster/`：集群调度 MCP
- `cmd/mcp-control/`：控制面 MCP

入口层只负责参数解析、配置装配、依赖初始化和服务启动，不承载核心业务逻辑。
