# 安装 Alice

三种安装方式。选择适合你的那一种。

## npm（推荐）

```bash
npm install -g @alice_space/alice
```

安装后运行设置向导：

```bash
alice setup
```

此命令会创建 `~/.alice/`、写入初始 `config.yaml`、同步内置 bundled skill、注册 systemd 用户单元（Linux），并安装 OpenCode delegate 插件。

**要求：** Node.js 18+

## 安装脚本

通过一行命令从 GitHub Releases 安装：

```bash
# 安装最新稳定版
curl -fsSL https://cdn.jsdelivr.net/gh/Alice-space/alice@main/scripts/alice-installer.sh | bash -s -- install

# 安装指定版本
curl -fsSL https://cdn.jsdelivr.net/gh/Alice-space/alice@main/scripts/alice-installer.sh | bash -s -- install --version v1.2.3

# 卸载
curl -fsSL https://cdn.jsdelivr.net/gh/Alice-space/alice@main/scripts/alice-installer.sh | bash -s -- uninstall
```

安装脚本会自动下载你平台的正确二进制文件（darwin-amd64、darwin-arm64、linux-amd64、linux-arm64、win32-x64）并校验 checksum。

安装后运行 `alice setup` 初始化配置和 skill 目录。

**要求：** `curl`、`tar`

## 从源码编译

```bash
git clone https://github.com/Alice-space/alice.git
cd alice
go build -o bin/alice ./cmd/connector
```

可选：安装到 PATH：

```bash
cp bin/alice /usr/local/bin/alice
```

**要求：** Go 1.25+

## 验证安装

```bash
alice --version
```

应输出版本号。如果已运行过 `alice setup`，还可以检查：

```bash
ls ~/.alice/
# config.yaml  skills/  log/  bots/
```

## Runtime Home

Alice 根据构建渠道使用不同的默认 home 目录：

| 构建渠道 | 默认 Home |
|-------|-------------|
| Release（npm / 安装脚本） | `~/.alice` |
| Dev（源码编译） | `~/.alice-dev` |

可通过 `--alice-home` 或环境变量 `ALICE_HOME` 覆盖。
