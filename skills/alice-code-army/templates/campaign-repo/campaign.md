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
  role: executor
  provider: ""
  model: ""
  profile: ""
  workflow: code_army
  reasoning_effort: high
  personality: pragmatic
default_reviewer:
  role: reviewer
  provider: ""
  model: ""
  profile: ""
  workflow: code_army
  reasoning_effort: high
  personality: analytical
default_planner:
  role: planner
  provider: ""
  model: ""
  profile: ""
  workflow: code_army
  reasoning_effort: high
  personality: analytical
default_planner_reviewer:
  role: planner_reviewer
  provider: ""
  model: ""
  profile: ""
  workflow: code_army
  reasoning_effort: high
  personality: analytical
plan_round: 0
plan_status: idle
---

# Campaign

## Objective
__CAMPAIGN_OBJECTIVE__

## Repo
- path: `__CAMPAIGN_REPO_PATH__`

## Pipeline Status
- plan status: `idle`
- plan round: `0`
- executor queue: `idle`
- reviewer queue: `idle`

## Gates
- TBD

## Current State
- phase: `P01`
- status: `planned`

## Roles
- planner default: `planner`
- planner reviewer default: `planner_reviewer`
- executor default: `executor`
- reviewer default: `reviewer`
- concrete provider / model / profile are dispatched by Alice runtime unless explicitly overridden in frontmatter

## Next
- Campaign will auto-start planning phase on first reconcile
- Planner generates proposal and draft tasks
- Planner reviewer evaluates plan
- Human approves plan via `/alice approve-plan`
- Execution begins automatically after approval
