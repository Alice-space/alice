# Glossary — 统一术语表

本文件由 Orchestrator 初始化，Planner 和 Executor 在发现歧义时补充。
所有角色对相同事物必须使用相同术语。

## 术语定义

| 术语 | 定义 | 备注 |
|------|------|------|
| Campaign | 一次完整的多阶段任务运行 | |
| Phase | Campaign 中必须串行的阶段 | 同一 phase 内的 task 可并行 |
| Task | 一个可独立执行的最小工作单元 | 对应一次 Executor 调用 |
| Executor | 负责执行具体 task 的 Codex agent | |
| Reviewer | 负责审阅 task 执行结果的 Sonnet agent | |
| Planner | 负责制定整体计划的 Opus agent | |
| Planner_Reviewer | 负责审阅计划的 Codex agent | |
| Orchestrator | 负责统筹调度的 Claude Sonnet（当前 session）| |

## 项目专有术语

（由 Planner 在规划阶段补充，用于消除项目代码库中的歧义概念）

| 术语 | 定义 | 来源 |
|------|------|------|
| | | |
