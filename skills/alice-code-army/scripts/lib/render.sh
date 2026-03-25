# shellcheck shell=bash

render_issue_note() {
  local campaign_id="$1"
  local payload
  payload="$(normalized_campaign_json "$campaign_id")"
  jq -r '
    def blank($value): if ($value // "") == "" then "-" else $value end;
    def bullet_metrics($items):
      if ($items | length) == 0 then
        ["- none"]
      else
        $items | map(
          "- `" + .name + "` = " + (.value | tostring) +
          (if (.unit // "") == "" then "" else " " + .unit end) +
          (if (.context // "") == "" then "" else " (" + .context + ")" end)
        )
      end;
    def bullet_gates($items):
      if ($items | length) == 0 then
        ["- none"]
      else
        $items | map(
          "- `" + .metric + "` " + .operator + " " + (.target | tostring) +
          (if (.unit // "") == "" then "" else " " + .unit end) +
          (if (.context // "") == "" then "" else " (" + .context + ")" end)
        )
      end;
    def cell($value):
      if ($value // "") == "" then "-" else ($value | tostring | gsub("[\r\n]+"; " ") | gsub("\\|"; "\\\\|")) end;
    def guidance_lines($items):
      if ($items | length) == 0 then
        ["- none"]
      else
        $items | reverse | .[:5] | map(
          "- `" + (.created_at // "") + "` [" + blank(.source) + "] " +
          blank(if (.command // "") != "" then .command else .summary end)
        )
      end;
    def review_lines($items):
      if ($items | length) == 0 then
        ["- none"]
      else
        $items | reverse | .[:5] | map(
          "- `" + (.created_at // "") + "` [" + blank(.reviewer_id) + "] `" + blank(.verdict) + "` " +
          blank(.summary)
        )
      end;
    def pitfall_lines($items):
      if ($items | length) == 0 then
        ["- none"]
      else
        $items | reverse | .[:5] | map(
          "- `" + (.created_at // "") + "` " + blank(.summary) +
          (if (.reason // "") == "" then "" else " (reason: " + .reason + ")" end)
        )
      end;
    .campaign as $c |
    (
      [
        "# Alice Code Army Campaign Sync",
        "",
        "- campaign: `" + $c.id + "`",
        "- title: " + blank($c.title),
        "- objective: " + blank($c.objective),
        "- status: `" + ($c.status | tostring) + "`",
        "- current winner: `" + blank($c.current_winner_trial_id) + "`",
        "- repo: `" + blank($c.repo) + "`",
        "- campaign repo path: `" + blank($c.campaign_repo_path) + "`",
        "- issue: `" + blank($c.issue_iid) + "`",
        "- manage mode: `" + ($c.manage_mode | tostring) + "`",
        "- max parallel trials: `" + (($c.max_parallel_trials // 0) | tostring) + "`",
        "- revision: `" + (($c.revision // 0) | tostring) + "`",
        "- updated at: `" + blank($c.updated_at) + "`",
        "",
        "## Summary",
        "",
        (if ($c.summary // "") == "" then "_none_" else $c.summary end),
        "",
        "## Baseline"
      ]
      + bullet_metrics($c.baseline)
      + [
        "",
        "## Gates"
      ]
      + bullet_gates($c.gates)
      + [
        "",
        "## Trials",
        "",
        "| trial | status | verdict | branch | MR | executor | summary |",
        "| --- | --- | --- | --- | --- | --- | --- |"
      ]
      + (
        if (($c.trials | length) == 0) then
          ["| - | - | - | - | - | - | - |"]
        else
          $c.trials | map(
            "| `" + .id + "` | `" + cell(.status) + "` | `" + cell(.verdict) + "` | `" + cell(.branch) + "` | `" + cell(.merge_request) + "` | `" + cell(.executor) + "` | " + cell(.summary) + " |"
          )
        end
      )
      + [
        "",
        "## Guidance"
      ]
      + guidance_lines($c.guidance)
      + [
        "",
        "## Reviews"
      ]
      + review_lines($c.reviews)
      + [
        "",
        "## Pitfalls"
      ]
      + pitfall_lines($c.pitfalls)
    ) | join("\n")
  ' <<<"$payload"
}

render_trial_note() {
  local campaign_id="$1" trial_id="$2"
  local payload
  payload="$(normalized_campaign_json "$campaign_id")"
  jq -r --arg trial_id "$trial_id" '
    def blank($value): if ($value // "") == "" then "-" else $value end;
    def metric_rows($items):
      if ($items | length) == 0 then
        ["| - | - | - | - |", "| --- | --- | --- | --- |"]
      else
        ["| metric | value | unit | context |", "| --- | --- | --- | --- |"] +
        ($items | map(
          "| `" + .name + "` | `" + (.value | tostring) + "` | `" + blank(.unit) + "` | `" + blank(.context) + "` |"
        ))
      end;
    .campaign as $c |
    ($c.trials[] | select(.id == $trial_id)) as $trial |
    (
      [
        "# Alice Code Army Trial Sync",
        "",
        "- campaign: `" + $c.id + "`",
        "- trial: `" + $trial.id + "`",
        "- campaign status: `" + ($c.status | tostring) + "`",
        "- trial status: `" + ($trial.status | tostring) + "`",
        "- verdict: `" + blank($trial.verdict) + "`",
        "- branch: `" + blank($trial.branch) + "`",
        "- merge request: `" + blank($trial.merge_request) + "`",
        "- executor: `" + blank($trial.executor) + "`",
        "- resource: `" + blank($trial.resource) + "`",
        "- job id: `" + blank($trial.job_id) + "`",
        "- updated at: `" + blank($trial.updated_at) + "`",
        "",
        "## Hypothesis",
        "",
        (if ($trial.hypothesis // "") == "" then "_none_" else $trial.hypothesis end),
        "",
        "## Summary",
        "",
        (if ($trial.summary // "") == "" then "_none_" else $trial.summary end),
        "",
        "## Metrics",
        ""
      ]
      + metric_rows($trial.metrics)
      + [
        "",
        "## Latest Guidance"
      ]
      + (
        if (($c.guidance | length) == 0) then
          ["- none"]
        else
          $c.guidance | reverse | .[:3] | map(
            "- `" + (.created_at // "") + "` [" + blank(.source) + "] " +
            blank(if (.command // "") != "" then .command else .summary end)
          )
        end
      )
      + [
        "",
        "## Latest Reviews"
      ]
      + (
        if (($c.reviews | length) == 0) then
          ["- none"]
        else
          $c.reviews | reverse | .[:3] | map(
            "- `" + (.created_at // "") + "` [" + blank(.reviewer_id) + "] `" + blank(.verdict) + "` " + blank(.summary)
          )
        end
      )
    ) | join("\n")
  ' <<<"$payload"
}
