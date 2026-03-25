# Alice Release Flow

这个参考文件描述 `Alice-space/alice` 当前的固定发布链路。只有目标仓库和这里一致时，才直接使用 `scripts/github.sh create-release` 的默认参数。

## Branch Rules

- 默认开发分支：`dev`
- 禁止直接 push 到 `main`
- `main` 只接受来自 `dev` 的 PR
- `dev -> main` 必须使用 merge commit，不能 squash / rebase

## CI / Release Behavior

- push 到 `dev`
  - 运行质量检查
  - 产出 dev 二进制
  - 更新 prerelease `dev-latest`
- merge `dev -> main`
  - 再次运行质量检查
  - 自动计算下一个 `vX.Y.Z`
  - 创建 GitHub Release 并上传构建产物

## Runtime Update

- 稳定版安装目录默认是 `~/.alice`
- dev 预发布安装目录默认是 `~/.alice-dev`
- 发布后的本地更新命令来自仓库脚本：
  `scripts/alice-installer.sh update`

## Practical Implication

- `create-release` 只负责把已经准备好的 `dev` 推到 `main` 并等待正式 release。
- `update-self` 只负责调用 `alice-installer.sh update` 更新本地运行时。
- 如果仓库的默认分支、PR 规则或 release workflow 发生变化，先更新这个 skill，再继续自动化发布。
