---
campaign_id: __CAMPAIGN_ID__
title: "__CAMPAIGN_TITLE__"
objective: "__CAMPAIGN_OBJECTIVE__"
status: planned
campaign_repo_path: "__CAMPAIGN_REPO_PATH__"
current_phase: P01
current_direction: ""
current_winner_task: ""
source_repos: []
review_mode: repo_first
report_mode: live_and_periodic
default_executor:
  role: executor.codex
  provider: codex
  model: ""
  profile: ""
  workflow: code_army
  reasoning_effort: high
  personality: pragmatic
default_reviewer:
  role: reviewer.claude
  provider: claude
  model: ""
  profile: ""
  workflow: code_army
  reasoning_effort: high
  personality: analytical
---

# Campaign

## Objective
__CAMPAIGN_OBJECTIVE__

## Repo
- path: `__CAMPAIGN_REPO_PATH__`

## Pipeline Status
- planner proposals (manual): `pending`
- human input: `pending`
- merged master plan (manual): `draft`
- executor queue: `idle`
- reviewer queue: `idle`

## Gates
- 待补充

## Current State
- phase: `P01`
- status: `planned`
- current direction: `-`
- current winner task: `-`

## Active Tasks
- none

## Blockers
- none

## Roles
- executor default: `executor.codex`
- reviewer default: `reviewer.claude`

## Next
- 完成人工/交互式 proposal
- 人工合并 master plan
- 展开 phase/task 目录树
