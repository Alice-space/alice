# 发版流程

Alice 版本的构建和发布方式。

## 分支策略

- 日常开发在 **`dev`** 分支进行
- 发版仅通过 **`dev → main`** 合并提交进行
- 绝不直接推送到 `main`

## CI 流水线

### `dev` Push 时
1. 运行质量门禁（`make check`）
2. 构建 dev 二进制
3. 更新预发布 `dev-latest`

### `dev` 合并到 `main` 时
1. 运行质量门禁（`make check`）
2. 自动创建下一个 `vX.Y.Z` tag
3. 构建所有平台的 release 二进制
4. 发布 GitHub Release

### 手动 `v*` Tag
- 推送 `v*` tag 会直接触发 release workflow

## Release 产出物

每个 Release 发布：
- 平台二进制构建：linux-amd64、linux-arm64、darwin-amd64、darwin-arm64、win32-x64
- npm 包：`@alice_space/alice`
- 安装脚本：`scripts/alice-installer.sh`

## 执行发版

1. 确保 `dev` 通过所有检查且准备就绪
2. 创建从 `dev` 到 `main` 的 PR
3. 使用 **merge commit** 合并（**不要** squash 或 rebase）
4. CI 自动创建 tag 并发布 Release
5. 验证 GitHub Release 显示所有产出物

## 版本号

Tag 遵循 semver：`vX.Y.Z`。CI 从上一个 release tag 自动递增补丁版本号。

## 发版后

- 安装脚本（`alice-installer.sh`）自动获取最新 release
- npm 用户通过 `npm update -g @alice_space/alice` 获取更新

## CI Workflow 文件

- `.github/workflows/ci.yml` — Dev 分支质量门禁和 dev 二进制
- `.github/workflows/main-release.yml` — Main 分支 release 构建
- `.github/workflows/release-on-tag.yml` — 手动 tag release
