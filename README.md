# Alice

Alice 是一个基于 Go 的任务编排与协作系统，当前仓库已经包含 v1 内核的可运行实现（运行时、入口、状态存储、运维读面、CLI 测试面），并继续按 `docs/tdr` 与 `docs/adr` 迭代。

## 当前状态

已落地的核心能力（代码层）：

- `cmd/alice` 单二进制入口（`serve` + CLI client commands）
- 统一入口事件接入（CLI / webhook / scheduler fire）
- 事件日志、快照与读模型重建
- `request/task/schedule/approval/human-wait/deadletter` 读接口
- 管理面写接口（submit/resolve/cancel/reconcile/replay 等）

仍在持续演进：

- workflow 细节与策略收敛
- 恢复与运维能力补齐
- 文档与实现持续对齐

## 目录结构

```text
.
├── cmd/                     # 可执行程序入口（alice + mcp）
├── internal/                # 业务实现（app/bus/store/ingress/ops/workflow/...）
├── configs/                 # 本地配置与 workflow manifests
├── docs/                    # TDR/CDR/ADR 与设计文档
├── api/                     # 对外契约草案
├── scripts/                 # 开发与运维脚本
└── test/                    # 集成测试资产
```

## 开发命令

```bash
go build ./...
go test ./...
```

启动服务：

```bash
./bin/alice serve --config configs/alice.yaml
```

CLI 示例：

```bash
./bin/alice --server http://127.0.0.1:8080 submit message --text "hello" --wait
./bin/alice --server http://127.0.0.1:8080 list requests
```

## 文档入口

- 文档层级说明：[`docs/README.md`](./docs/README.md)
- TDR 总览：[`docs/tdr/README.md`](./docs/tdr/README.md)
- CDR 总览：[`docs/cdr/README.md`](./docs/cdr/README.md)
- ADR 总览：[`docs/adr/README.md`](./docs/adr/README.md)
