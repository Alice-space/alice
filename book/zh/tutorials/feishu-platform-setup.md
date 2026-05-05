# 飞书开放平台配置

本教程将指导你创建一个可供 Alice 连接的飞书应用。预计耗时：15 分钟。

## 概览

Alice 需要一个具备以下条件的飞书应用：

1. 已启用 **bot 能力**
2. 已订阅 **`im.message.receive_v1`** 事件
3. 已配置所需的消息**权限**
4. 已启用**长连接**模式

## 第 1 步：登录飞书开放平台

访问[飞书开放平台](https://open.feishu.cn)，使用你的组织账号登录。

> **Lark（国际版）用户**：请访问 [Lark Open Platform](https://open.larksuite.com)，并在 bot 配置中设置 `feishu_base_url: "https://open.larksuite.com"`。

## 第 2 步：创建应用

1. 点击**创建应用**
2. 选择**企业自建应用**
3. 为应用命名（例如 "Alice Bot"）并上传图标
4. 点击**创建**

## 第 3 步：启用 Bot 能力

1. 在左侧边栏，进入**功能** → **机器人**
2. 开启**启用机器人**
3. 根据需要配置 bot 的名称、头像和简介

## 第 4 步：添加事件订阅

1. 进入**事件订阅**
2. 点击**添加事件**
3. 找到并选择**接收消息** → **`im.message.receive_v1`**
4. 点击**确认**

这样 Alice 就能收到 bot 可见的所有消息了。

## 第 5 步：配置权限

1. 进入**权限管理**
2. 搜索并开通以下权限：

| 权限 | 用途 |
|-----------|-----|
| `im:message` | 读取发送给 bot 的消息 |
| `im:message:send_as_bot` | 以 bot 身份发送消息 |
| `im:message:read` | 读取消息内容 |
| `im:resource` | 下载图片和文件 |
| `contact:user.id:readonly` | 获取用户名称 |
| `contact:group.id:readonly` | 获取群聊信息 |

3. 点击**保存**

## 第 6 步：启用长连接

1. 进入**功能** → **事件订阅**
2. 找到**连接方式**区域
3. 从**Request URL**切换为**长连接**
4. 保存更改

> 这一步至关重要。Alice 使用 WebSocket 长连接，而非 HTTP webhook。如果不启用长连接模式，Alice 将无法接收消息。

## 第 7 步：获取凭据

1. 进入**应用设置** → **基础信息**
2. 复制你的 **App ID**（应用凭证 → App ID）
3. 复制你的 **App Secret**（应用凭证 → App Secret）

将这些填入 `config.yaml`：
```yaml
bots:
  my_bot:
    feishu_app_id: "cli_xxxxxxxx"      # 你的 App ID
    feishu_app_secret: "your_secret"    # 你的 App Secret
```

## 第 8 步：发布并审批

1. 进入**版本管理与发布**
2. 点击**创建版本**，填写版本信息
3. 创建后，点击**申请发布**
4. 你的飞书组织管理员需要审批通过
5. 审批通过后，组织内用户即可搜索并与 bot 互动

> 提示：开发期间，可以在应用设置中将个人用户添加为**应用协作者**，这样在发布前就可以测试 bot。

## 验证

使用 `alice --feishu-websocket` 启动 Alice 后，检查日志：

```
feishu-codex connector started (long connection mode)
```

如果看到 WebSocket 连接错误，请确认长连接模式已启用且凭据无误。

## 下一步

- [安装并运行 Alice](../tutorials/quick-start.md)
- [排解常见问题](../how-to/troubleshoot.md)
