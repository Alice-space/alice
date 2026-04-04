---
task_id: T000
title: ""
phase: P01
status: pending   # pending | executing | review_pending | done | failed | blocked
depends_on: []
target_repos: []
working_branches: []
worktree_paths: []
write_scope: []
owner_agent: ""
lease_until: ""
executor:
  role: executor
  workflow: code_army
reviewer:
  role: reviewer
  workflow: code_army
dispatch_state: idle
review_status: pending
execution_round: 0
review_round: 0
base_commit: ""
head_commit: ""
last_run_path: ""
last_review_path: ""
wake_at: ""
wake_prompt: ""
report_snippet_path: "results/report-snippet.md"
artifacts: []
result_paths: []
---

# Task

## Goal
- 写清要改的 repo / 模块 / 路径范围、预期 end state 和任务边界

## Background
- 写清为什么要做、已知约束、上游依赖和前置事实

## Acceptance
- 写可观察的验收标准：命令、测试、产物、报告、diff 边界或指标

## Deliverables
- 写清具体产物路径、结果文件、文档或可追溯证据
