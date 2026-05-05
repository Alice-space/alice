# 快速开始

5 分钟内让 Alice 运行起来并响应消息。

## 前置条件

- **Node.js**（用于 `npm install`）或 **Go 1.25+**（用于源码编译）
- 一个**飞书应用**，已启用 bot 能力和长连接
- 至少安装并认证好一个 **LLM CLI**：
  - [OpenCode](https://github.com/anomalyco/opencode)
  - [Codex](https://github.com/openai/codex)
  - [Claude](https://docs.anthropic.com/en/docs/claude-code)
  - [Gemini](https://cloud.google.com/gemini-cli)
  - [Kimi](https://github.com/moonshotai/kimi-cli)

如果还没搭建飞书应用，请先按照[飞书开放平台配置](feishu-platform-setup.md)教程操作。

## 第 1 步：安装

**通过 npm（推荐）：**
```bash
npm install -g @alice_space/alice
```

**通过安装脚本：**
```bash
curl -fsSL https://cdn.jsdelivr.net/gh/Alice-space/alice@main/scripts/alice-installer.sh | bash -s -- install
```

**从源码编译：**
```bash
git clone https://github.com/Alice-space/alice.git
cd alice
go build -o bin/alice ./cmd/connector
```

## 第 2 步：初始化

```bash
alice setup
```

这会在 `~/.alice/` 下创建目录结构，写入默认的 `config.yaml`，同步内置 bundled skill，并在 Linux 上注册 systemd 用户单元。

## 第 3 步：配置

编辑 `~/.alice/config.yaml`，至少填入以下内容：

```yaml
bots:
  my_bot:
    name: "Alice"
    feishu_app_id: "cli_xxxxxxxx"      # 来自飞书开放平台
    feishu_app_secret: "your_secret"    # 来自飞书开放平台
    llm_profiles:
      chat:
        provider: "opencode"
        model: "deepseek/deepseek-v4-flash"
      work:
        provider: "opencode"
        model: "deepseek/deepseek-v4-pro"
```

默认配置包含指向 DeepSeek 模型的 OpenCode profile。如果使用其他 LLM CLI，请参见[配置 LLM 后端](../how-to/configure-backend.md)。

## 第 4 步：验证后端认证

确保你的 LLM CLI 能正常认证：

```bash
opencode --version    # 或 codex、claude 等
```

## 第 5 步：启动

```bash
alice --feishu-websocket
```

你应该能看到飞书 WebSocket 连接和每个 bot runtime 初始化的日志。

## 第 6 步：用 Work 模式测试

大多数人用 Alice 是冲着 **work 模式**来的 — 工程任务、调试、自动化。用法：

在飞书群聊里 @你的 bot：

```
@Alice #work 修复 auth.go 里的登录超时问题
```

发生了什么：
1. Alice 为这个任务创建飞书 thread
2. 启动配置好的 LLM 后端（如 DeepSeek V4）
3. 进度和工具调用实时推送到 thread
4. 会话被持久化 — 之后可以从终端恢复

试试内置命令：
```
/help       — 显示命令列表
/status     — 查看当前会话和后端信息
/stop       — 取消正在运行的任务
```

### Chat 模式（闲聊）

Alice 也支持 casual `chat` 模式，bot 像群里的常住成员一样参与对话。直接 @ 即可：

```
@Alice 今天天气怎么样？
```

Chat 模式使用 `chat` LLM profile（轻量模型），整个群共享一个会话，不创建 thread。用 `/clear` 重置聊天会话。

> **提示**：默认 `config.example.yaml` 同时启用了两种模式。大多数运维场景以 work 模式为主。如果只需要 work，设置 `group_scenes.chat.enabled: false` 即可。

## 接下来？

- [配置独立的 chat 和 work 场景](../how-to/configure-chat-work.md)
- [切换到其他 LLM 后端](../how-to/configure-backend.md)
- [部署为持久化服务](../how-to/deploy.md)
- [理解系统模型](../explanation/system-model.md)
