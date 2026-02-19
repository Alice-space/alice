# Contributing

本仓库欢迎贡献。为保证代码质量和可维护性，请按以下规则协作。

## 1. 分支与变更范围

- 请基于最新 `master` 创建分支。
- 分支命名建议：`feat/*`、`fix/*`、`docs/*`、`chore/*`。
- 每次提交只做一件事，避免把不相关改动混在一个提交里。

## 2. 提交信息规范

- 推荐使用 Conventional Commits：
  - `feat:` 新功能
  - `fix:` 缺陷修复
  - `docs:` 文档更新
  - `chore:` 工程或依赖调整
  - `test:` 测试相关改动
  - `refactor:` 重构（不改行为）
- 提交标题尽量简洁明确，能直接看出改动目的。

## 3. 提交前必须检查

首次在本地仓库执行：

```bash
make precommit-install
```

每次提交前必须通过：

```bash
make check
```

`make check` 包含：

- `make fmt-check`（`gofmt` 检查）
- `go vet ./...`
- `go test ./...`

如格式不通过，可先执行：

```bash
make fmt
```

## 4. 代码规则

- 统一使用 `gofmt` 格式化代码。
- 新增或修改行为时，必须补充/更新对应测试。
- 不要在日志中输出敏感信息（例如 app secret、token、用户隐私内容）。
- 变更 CLI 参数时需保证向后兼容，或在文档中明确说明破坏性变更。

## 5. 配置变更规则

- 本项目使用 YAML 配置文件（`-c config.yaml`），不使用环境变量作为主配置入口。
- 若新增配置项，必须同时更新：
  - `config.example.yaml`
  - `/Users/alice/Developer/alicespace/internal/config/config.go` 中默认值与校验逻辑
  - 相关文档（中英文 README）

## 6. 文档同步规则

- 任何用户可见行为变更（命令、参数、配置、运行方式）都必须同步更新：
  - `/Users/alice/Developer/alicespace/README.md`
  - `/Users/alice/Developer/alicespace/README.zh-CN.md`
- 保持中英文文档内容一致，避免一份文档过期。

## 7. 合并前自检清单

- 本地 `make check` 通过。
- 关键路径可运行（至少验证一次启动命令）：

```bash
go run ./cmd/connector -c config.yaml
```

- 文档与示例命令已同步更新。
- 不包含无关文件或临时调试内容。
