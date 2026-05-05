# Feishu Platform Setup

This tutorial walks through creating a Feishu app that Alice can connect to. Estimated time: 15 minutes.

## Overview

Alice needs a Feishu app with:

1. **Bot capability** enabled
2. **`im.message.receive_v1`** event subscription
3. Required message **permissions**
4. **Long connection** mode enabled

## Step 1: Log into Feishu Open Platform

Visit [Feishu Open Platform](https://open.feishu.cn) and sign in with your organization account.

> **Lark (international) users**: Visit [Lark Open Platform](https://open.larksuite.com) instead. Then set `feishu_base_url: "https://open.larksuite.com"` in your bot config.

## Step 2: Create an App

1. Click **Create App** (创建应用)
2. Choose **Enterprise Self-built App** (企业自建应用)
3. Name your app (e.g., "Alice Bot") and upload an icon
4. Click **Create**

## Step 3: Enable Bot Capability

1. In the left sidebar, go to **Features** → **Bot** (机器人)
2. Toggle **Enable Bot** (启用机器人)
3. Configure the bot's name, avatar, and description as desired

## Step 4: Add Event Subscription

1. Go to **Event Subscriptions** (事件订阅)
2. Click **Add Event** (添加事件)
3. Find and select **Receive Message** (接收消息) → **`im.message.receive_v1`**
4. Click **Confirm**

This is what allows Alice to receive all messages the bot can see.

## Step 5: Configure Permissions

1. Go to **Permissions** (权限管理)
2. Search for and enable these permissions:

| Permission | Why |
|-----------|-----|
| `im:message` | Read messages sent to the bot |
| `im:message:send_as_bot` | Send messages as the bot |
| `im:message:read` | Read message content |
| `im:resource` | Download images and files |
| `contact:user.id:readonly` | Resolve user names |
| `contact:group.id:readonly` | Access group chat info |

3. Click **Save** (保存)

## Step 6: Enable Long Connection

1. Go to **Features** → **Event Subscriptions** (事件订阅)
2. Find the **Connection Mode** (连接方式) section
3. Switch from **Request URL** to **Long Connection** (长连接)
4. Save the change

> This is critical. Alice uses WebSocket long connections, not HTTP webhooks. If long connection mode is not enabled, Alice cannot receive messages.

## Step 7: Get Credentials

1. Go to **App Settings** → **Basic Info** (基础信息)
2. Copy your **App ID** (应用凭证 → App ID)
3. Copy your **App Secret** (应用凭证 → App Secret)

These go into your `config.yaml`:
```yaml
bots:
  my_bot:
    feishu_app_id: "cli_xxxxxxxx"      # your App ID
    feishu_app_secret: "your_secret"    # your App Secret
```

## Step 8: Publish and Approve

1. Go to **Version Management** (版本管理与发布)
2. Click **Create Version** (创建版本), fill in version info
3. After creation, click **Apply for Release** (申请发布)
4. An admin in your Feishu org must approve the release
5. Once approved, users in your org can find and interact with the bot

> Tip: During development, you can add individual users as **App Collaborators** (应用协作者) under App Settings, allowing them to test the bot before publishing.

## Verification

After starting Alice with `alice --feishu-websocket`, check the logs:

```
feishu-codex connector started (long connection mode)
```

If you see WebSocket connection errors, double-check that long connection mode is enabled and your credentials are correct.

## Next Steps

- [Install and run Alice](../tutorials/quick-start.md)
- [Troubleshoot common issues](../how-to/troubleshoot.md)
