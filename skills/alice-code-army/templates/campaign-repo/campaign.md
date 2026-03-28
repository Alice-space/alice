---
campaign_id: __CAMPAIGN_ID__
title: "__CAMPAIGN_TITLE__"
objective: "__CAMPAIGN_OBJECTIVE__"
campaign_repo_path: "__CAMPAIGN_REPO_PATH__"
current_phase: P01
current_direction: ""
current_winner_task: ""
source_repos: []
review_mode: repo_first
report_mode: live_and_periodic
plan_round: 0
plan_status: idle
---

# Campaign

## Objective
__CAMPAIGN_OBJECTIVE__

## Repo
- path: `__CAMPAIGN_REPO_PATH__`

## Status Tracking
- repo truth lives in campaign frontmatter: `current_phase`, `plan_status`, `plan_round`
- runtime campaign status should be checked via `alice-code-army.sh get` / runtime campaign APIs
- queue / ready / blocked summary lives in `reports/live-report.md`

## Gates
- TBD

## Roles
- planner default: `planner`
- planner reviewer default: `planner_reviewer`
- executor default: `executor`
- reviewer default: `reviewer`
- concrete provider / model / profile come from `config.yaml` `campaign_role_defaults` + `llm_profiles`

## Next
- Campaign will auto-start planning phase on first reconcile
- Planner generates proposal, phase docs, and refined task packages
- Planner reviewer evaluates plan
- Human approves plan via `/alice approve-plan` only after `repo-lint --for-approval` passes
- Execution begins automatically after approval
