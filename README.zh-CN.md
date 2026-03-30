# Alice

[English](./README.md)
[![Dev CI](https://github.com/Alice-space/alice/actions/workflows/ci.yml/badge.svg)](https://github.com/Alice-space/alice/actions/workflows/ci.yml)
[![Main Release](https://github.com/Alice-space/alice/actions/workflows/main-release.yml/badge.svg)](https://github.com/Alice-space/alice/actions/workflows/main-release.yml)
[![Release On Tag](https://github.com/Alice-space/alice/actions/workflows/release-on-tag.yml/badge.svg)](https://github.com/Alice-space/alice/actions/workflows/release-on-tag.yml)

Alice 是一个面向飞书的长连接连接器，用来把 Codex、Claude、Gemini、Kimi 这类 CLI 型 LLM agent 接入飞书聊天。

它以本地多 bot runtime 的方式运行：

- 通过 WebSocket 接收飞书消息
- 把消息路由到 `chat` 或 `work` 场景
- 调用配置好的 LLM CLI
- 把进度、文本、文件、图片发回飞书
- 暴露本地 runtime API，供自带 skill 使用

## 功能特性

- 单个 `config.yaml` 托管多个 bot
- 每个 bot 拥有隔离的 `workspace`、`SOUL.md` 和 prompt，默认共享 `CODEX_HOME`
- 支持群聊里的 `chat` / `work` 两种场景路由
- 提供 runtime HTTP API 给 skill 和自动化任务
- 自带 skill 会释放到 `${ALICE_HOME:-~/.alice}/skills`，再链接到 `~/.agents/skills`，并通过 `~/.claude/skills` 暴露给 Claude
- `alice-code-army` 通过 Alice runtime 按角色派发 planner / reviewer / executor 流程，模板不再把具体模型写死
- `alice-code-army` 的 campaign repo 现在会用 `repo-lint` 校验细化后的 task package（`task.md` / `context.md` / `plan.md`），`approve-plan` 必须先过 review，review 文件也收进各自 task 文件夹
- `alice-code-army` 现在会在每一轮 executor / reviewer 结束后强制校验 task invariant：executor 必须以合法 handoff 状态收尾（`review_pending`、`waiting_external` 或 `blocked`），reviewer 必须落下合法的 task-local review 文件
- `alice-code-army` 的“等待人工批准”卡片现在会直接提供“批准 / 不批准”按钮，并带计划轮次过期保护；点击后会把原卡片更新成“已批准 / 已拒绝”的终态显示
- 二进制内嵌 prompts、skills、配置示例和 `SOUL.md` 示例
- 提供适合 `systemd --user` 的安装脚本

## 运行要求

- 源码构建需要 Go 1.25+
- 至少安装并登录一种后端 CLI：
  - `codex`
  - `claude`
  - `gemini`
  - `kimi`
- 飞书应用需要：
  - 开启机器人能力
  - 订阅 `im.message.receive_v1`
  - 开通所需消息权限
  - 启用长连接模式

## 快速开始

### 用 release 安装

```bash
curl -fsSL https://cdn.jsdelivr.net/gh/Alice-space/alice@main/scripts/alice-installer.sh | bash -s -- install
```

然后：

1. 编辑 `${ALICE_HOME:-~/.alice}/config.yaml`
2. 设置 `bots.*.feishu_app_id` 和 `bots.*.feishu_app_secret`
3. 重启服务：

```bash
systemctl --user restart alice.service
```

### 从源码运行

```bash
cp config.example.yaml ~/.alice/config.yaml
# 编辑 ~/.alice/config.yaml

go mod tidy
go test ./...
go run ./cmd/connector --feishu-websocket
```

## 配置

Alice 现在使用纯多 bot 配置模型。

你最需要关注的配置概念：

- `bots.<id>`：一个运行中的 bot
- `llm_profiles`：命名模型档位
- `group_scenes.chat`：群聊里的聊天场景
- `group_scenes.work`：群聊里的任务场景
- `trigger_mode`：两种 scene 都关闭时的旧触发回退
- `workspace_dir` / `prompt_dir`：每个 bot 的运行目录
- `codex_home`：共享 `CODEX_HOME` 的可选 bot 级覆盖，默认是 `~/.codex`
- `image_generation`：可选的角色生图链路

从 [config.example.yaml](./config.example.yaml) 开始改最稳妥。

## 使用说明

关于系统整体如何使用，以及 `chat` / `work` 模式怎么工作，见：

- [使用说明](./docs/usage.zh-CN.md)
- [Usage Guide](./docs/usage.md)

其他文档：

- [架构文档](./docs/architecture.zh-CN.md)
- [Architecture](./docs/architecture.md)
- [飞书消息流说明](./docs/feishu-message-flow.zh-CN.md)
- [CodeArmy 隔离调试手册](./docs/codearmy-isolated-debug.zh-CN.md)

Alice 现在要求显式选择启动模式：真实飞书连接使用 `--feishu-websocket`，只跑本地 runtime/API 使用 `--runtime-only`。如果是隔离调试或临时 rerun runtime，必须使用 `alice-headless --runtime-only`；headless binary 不再允许启动飞书长连接。

## `SOUL.md`

每个 bot 可以在 `workspace/SOUL.md` 中定义人格和机器可读元数据。

当前 Alice 接受的 frontmatter 键：

- `image_refs`
- `image_generation`
- `output_contract`

内置示例见 [SOUL.md.example](./SOUL.md.example)。

## 安装脚本

安装脚本位于 [scripts/alice-installer.sh](./scripts/alice-installer.sh)。

常用命令：

```bash
# 安装或更新到最新 stable release
curl -fsSL https://cdn.jsdelivr.net/gh/Alice-space/alice@main/scripts/alice-installer.sh | bash -s -- install

# 卸载
curl -fsSL https://cdn.jsdelivr.net/gh/Alice-space/alice@main/scripts/alice-installer.sh | bash -s -- uninstall
```

## 开发

```bash
make check
make build
make run
```

`make check` 会执行格式检查、`vet`、单测和 connector 的 race 测试。

贡献规范见 [CONTRIBUTING.md](./CONTRIBUTING.md)。

## 发布流程

- 日常开发在 `dev`
- 常规发布路径是 `dev -> main`
- GitHub Actions 负责打包和发布 tag release

相关 workflow：

- [.github/workflows/ci.yml](./.github/workflows/ci.yml)
- [.github/workflows/main-release.yml](./.github/workflows/main-release.yml)
- [.github/workflows/release-on-tag.yml](./.github/workflows/release-on-tag.yml)

## 许可证

[MIT](./LICENSE)
