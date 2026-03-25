---
name: github
description: 通过 GitHub 的 git / REST API 处理仓库操作，尤其是推送 `dev`、创建和合并从 `dev` 到 `main` 的 pull request、等待 GitHub Actions 产出正式 release，以及在 Alice 仓库里执行发布后的自更新。用户提到 GitHub、PR、merge、release、tag、Actions、CI、创建新发行版、更新自己时使用。
---

# GitHub

优先使用 `scripts/github.sh`，并依赖当前机器上已经登录的 `gh`。这个 skill 主要为 Alice 当前的发布链路设计，但也能复用于同样遵循 `dev -> main`、merge commit、merge 后自动发 release 的仓库。

## Quick Start

- 创建新的发行版：
  `scripts/github.sh create-release --repo-dir /path/to/repo`
- 更新 Alice 运行时到最新稳定版：
  `scripts/github.sh update-self --repo-dir /path/to/alice`
- 更新 Alice 运行时到指定版本：
  `scripts/github.sh update-self --repo-dir /path/to/alice --version vX.Y.Z`

## Workflow

1. 先确认目标仓库的工作树干净，避免把未提交或无关改动推进 release。
2. 对 Alice 仓库，默认发布路径固定为 `dev -> main`，合并方式固定为 `merge`。
3. `create-release` 会自动执行这些步骤：
   - 校验 Git 仓库、当前分支和工作树状态
   - 推送 `dev`
   - 创建或复用 `dev -> main` 的 open PR
   - 轮询 PR 状态，直到可以用 merge commit 合并
   - 等待与 merge commit 对应的正式 GitHub Release 出现
4. `update-self` 会调用仓库里的 `scripts/alice-installer.sh update`，适合在 release 完成后更新当前 Alice 运行时。

## Guardrails

- 只有在仓库确实采用 `dev -> main` 自动发布流程时才使用默认参数；Alice 仓库的固定规则见 `references/alice-release-flow.md`。
- `create-release` 依赖 `gh auth status` 通过；脚本只通过 `gh` 使用当前机器上的 GitHub 登录态，不自己读取、拼接或打印 token。
- 如果当前仓库存在未提交改动，先停下来，确认这些改动是否应该进入这次 release。
- 如果 PR 长时间停留在 `blocked`、`behind` 或 `dirty`，不要强行改写分支；先检查分支保护、必需检查项或冲突原因。

## Alice Repo Notes

- 读 `references/alice-release-flow.md` 获取 Alice 仓库的分支与发布约束。
- `update-self` 默认更新 stable release；只有用户明确要求时才加 `--channel dev`。
- 发布完成后，在回复里给出 PR 编号、merge commit SHA、release tag 和 release URL。

## Response Pattern

- 明确说明执行的是 `create-release` 还是 `update-self`。
- 对 release 流程，回复里带上：
  - 仓库名
  - PR 编号
  - merge commit SHA
  - release tag 和 URL
- 对自更新，回复里带上：
  - 使用的仓库
  - channel / version
  - `ALICE_HOME` 或显式 `--home`
- 如果因为 `gh` 未登录、分支状态异常或仓库不匹配而未执行，明确说明阻塞点和下一步。
