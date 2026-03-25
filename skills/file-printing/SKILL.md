---
name: file-printing
description: '通过本机 CUPS 打印用户提供的文件。适用于用户要求打印附件/本地文件、查看或设置默认打印机、检查队列状态、排查打印失败（例如“打印这个文件”“用HL5590打印”“查看打印任务状态”）。'
---

# 文件打印

按以下流程执行，保证打印结果可追踪、可反馈。

维护约束：当前会话里 `.agents/skills/...` 的已安装 skill 副本来自 Alice 安装/更新流程，不应直接修改；需要变更 skill 时，应修改 Alice 仓库里的 `alice/skills/...` 源文件，再通过安装流程同步进去。

## 工作流

1. 解析文件输入。
- 优先使用用户消息上下文里的附件路径。
- 用户直接给了绝对路径也可接受。
- 提交前先用 `test -r <file_path>` 校验可读性。

2. 解析目标打印机。
- 优先使用用户明确指定的打印机。
- 否则读取当前用户默认打印机：`lpoptions`（`Default <printer>`）。
- 若无默认且用户不反对，可执行 `lpoptions -d HL5590` 设置默认。

3. 提交打印任务。
- 默认执行：`scripts/print_file.sh <file_path>`。
- 仅在用户要求时附加参数：`--printer`、`--copies`、`--sides`、`--media`、`--option`。
- 成功后向用户返回 `JOB_ID` 与 `PRINTER`。

4. 确认队列状态。
- 查询未完成任务：`lpstat -W not-completed -o`（或 `lpstat -o`）。
- 回复中明确任务处于“排队/打印中”，并附相关行。

5. 明确处理异常。
- 若输出含 `Forbidden`，说明是 CUPS 权限策略限制，并询问是否走用户侧默认路径/降级方案。
- 若文件格式失败，执行 `file --mime-type <file_path>`，建议先转 PDF 再打印。
- 若打印机不可用，执行 `lpstat -p <printer>`，返回状态与原因。

## 常用命令

- 设置当前用户默认打印机：`lpoptions -d HL5590`
- 查看当前用户默认打印机：`lpoptions | sed -n '1p'`
- 查看已配置打印机：`lpstat -p`
- 查看设备映射：`lpstat -v`
- 查看队列：`lpstat -W not-completed -o`

## 脚本

- 主脚本：`scripts/print_file.sh`
- 作用：校验输入、通过 `lp` 提交任务、输出可解析结果（`PRINTER`、`JOB_ID`、`RAW`）
- 真正打印前可先做安全检查：`scripts/print_file.sh --dry-run <file_path>`

## 示例

用户：“把这个 PDF 打印两份，双面长边。”

执行：
`scripts/print_file.sh /abs/path/file.pdf --copies 2 --sides two-sided-long-edge`

回复中包含：
- 打印机名称
- 作业 ID
- 队列状态行
