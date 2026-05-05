# Alice

[English](./README.md)
[![Dev CI](https://github.com/Alice-space/alice/actions/workflows/ci.yml/badge.svg)](https://github.com/Alice-space/alice/actions/workflows/ci.yml)
[![Main Release](https://github.com/Alice-space/alice/actions/workflows/main-release.yml/badge.svg)](https://github.com/Alice-space/alice/actions/workflows/main-release.yml)
[![Release On Tag](https://github.com/Alice-space/alice/actions/workflows/release-on-tag.yml/badge.svg)](https://github.com/Alice-space/alice/actions/workflows/release-on-tag.yml)

Alice 是一个面向飞书的长连接连接器，用来把 Codex、Claude、Gemini、Kimi、OpenCode 这类 CLI 型 LLM agent 接入飞书聊天。

它以本地多 bot runtime 的方式运行：

- 通过 WebSocket 接收飞书消息
- 把消息路由到 `chat` 或 `work` 场景
- 调用配置好的 LLM CLI
- 把进度、文本、文件、图片发回飞书
- 暴露本地 runtime API，供自带 skill 使用
- 提供 `alice delegate` 子命令，让 OpenCode agent（包括 DeepSeek）把子任务委托给 Codex、Claude 等后端

## 功能特性

- 单个 `config.yaml` 托管多个 bot
- 每个 bot 拥有隔离的 `workspace`、`SOUL.md`（位于 `alice_home` 下）和 prompt，默认共享 `CODEX_HOME`
- 支持群聊里的 `chat` / `work` 两种场景路由
- 提供 runtime HTTP API 给 skill 和自动化任务
- 长时间运行的 LLM 会显示运行状态卡片，包含后端活动和合并后的代码编辑信号
- 自动化任务 watchdog 会提醒过期未触发或疑似卡住的定时任务
- 自带 skill 会释放到 `${ALICE_HOME:-~/.alice}/skills`，再链接到 `~/.agents/skills`，并通过 `~/.claude/skills` 暴露给 Claude
- 二进制内嵌 prompts、skills、配置示例、`SOUL.md` 示例和 OpenCode delegate 插件
- `alice setup` 一键初始化：配置 + skills + systemd unit + OpenCode 插件

## 运行要求

- 源码构建需要 Go 1.25+
- 至少安装并登录一种后端 CLI：
  - `codex`
  - `claude`
  - `gemini`
  - `kimi`
  - `opencode`
- 飞书应用需要：
  - 开启机器人能力
  - 订阅 `im.message.receive_v1`
  - 开通所需消息权限
  - 启用长连接模式

## 快速开始

### 用 release 安装

**通过 npm 安装（推荐）：**

```bash
npm install -g @alice_space/alice
alice setup
```

**通过安装脚本：**

```bash
curl -fsSL https://cdn.jsdelivr.net/gh/Alice-space/alice@main/scripts/alice-installer.sh | bash -s -- install
```

然后：

1. 编辑 `${ALICE_HOME:-~/.alice}/config.yaml`
2. 设置 `bots.*.feishu_app_id` 和 `bots.*.feishu_app_secret`
3. 启动服务：

```bash
systemctl --user start alice.service   # Linux（alice setup 已写好 unit）
alice --feishu-websocket               # macOS / 手动启动

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
从 [config.example.yaml](./config.example.yaml) 开始改最稳妥。

## 使用说明

关于系统整体如何使用，以及 `chat` / `work` 模式怎么工作，见：

- [使用说明](./docs/usage.zh-CN.md)
- [Usage Guide](./docs/usage.md)

### 委托任务给其他 LLM

`alice delegate` 子命令可以把单次 prompt 发给任意已配置的 LLM CLI：

```bash
alice delegate --provider codex --prompt "重构 auth 模块"
alice delegate --provider claude --prompt "审查这个 PR diff" < diff.patch
```

### OpenCode 插件

`alice setup` 会写入 `~/.config/opencode/plugins/alice-delegate.js`。
该文件存在后，OpenCode agent（包括 DeepSeek）自动获得 `codex` 和 `claude` 两个 tool，
底层通过 `alice delegate` 命令完成子任务委托。无需额外配置，OpenCode 会自动加载该目录下的插件。

其他文档：

- [文档索引](./docs/README.md)
- [架构文档](./docs/architecture.zh-CN.md)
- [Architecture](./docs/architecture.md)

Alice 现在要求显式选择启动模式：真实飞书连接使用 `--feishu-websocket`，只跑本地 runtime/API 使用 `--runtime-only`。如果是隔离调试或临时 rerun runtime，必须使用 `alice-headless --runtime-only`；headless binary 不再允许启动飞书长连接。

LLM 后端抽象代码位于 `internal/llm/`（原 `agentbridge` 库已合并到此仓库）。

## `SOUL.md`

每个 bot 都可以在自己配置的 `soul_path` 中定义人格和机器可读元数据。
默认路径为 `<alice_home>/SOUL.md`；相对 `soul_path` 会相对于 `<alice_home>` 解析。

当前 Alice 接受的 frontmatter 键：

- `image_refs`
- `output_contract`

内置示例见 [prompts/SOUL.md.example](./prompts/SOUL.md.example)。

## 安装脚本

安装脚本位于 [scripts/alice-installer.sh](./scripts/alice-installer.sh)。

常用命令：

```bash
# 安装或更新到最新 stable release
curl -fsSL https://cdn.jsdelivr.net/gh/Alice-space/alice@main/scripts/alice-installer.sh | bash -s -- install

# 卸载
curl -fsSL https://cdn.jsdelivr.net/gh/Alice-space/alice@main/scripts/alice-installer.sh | bash -s -- uninstall
```

通过 npm 安装时，`npm install -g @alice_space/alice` 之后执行 `alice setup` 即可创建 ALICE_HOME 目录结构、写入初始配置、同步自带 skills、注册 systemd user unit（Linux）、安装 OpenCode delegate 插件。安装脚本仍然保留，供希望用单条 curl 命令完成安装或需要 release 下载 / checksum 校验的用户使用。

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
