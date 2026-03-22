#!/usr/bin/env bash
set -euo pipefail

PROGRAM="$(basename "$0")"
ALICE_HOME_DIR="${ALICE_HOME:-$HOME/.alice}"
DEFAULT_GITLAB_HOST="${ALICE_CODE_ARMY_GITLAB_HOST:-code.ihep.ac.cn}"

usage() {
  cat <<EOF
Usage:
  $PROGRAM list|get|create|patch|upsert-trial|add-guidance|add-review|add-pitfall ...
  $PROGRAM apply-command CAMPAIGN_ID COMMAND [SOURCE]
  $PROGRAM render-issue-note CAMPAIGN_ID
  $PROGRAM render-trial-note CAMPAIGN_ID TRIAL_ID
  $PROGRAM sync-issue CAMPAIGN_ID
  $PROGRAM sync-trial CAMPAIGN_ID TRIAL_ID
  $PROGRAM sync-all CAMPAIGN_ID

Environment:
  ALICE_RUNTIME_BIN            Override the alice runtime binary path.
  ALICE_HOME                   Override Alice home (default: ~/.alice).
  ALICE_CODE_ARMY_GITLAB_HOST  Default GitLab host for sync commands (default: code.ihep.ac.cn).

Visibility contract:
  create requires a visible GitLab issue (`issue_iid`) in the payload.
  patch / upsert-trial / add-guidance / add-review / add-pitfall / apply-command
  auto-sync the issue (and MR when available) after mutating campaign state.
EOF
}

die() {
  printf '[alice-code-army] ERROR: %s\n' "$*" >&2
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"
}

resolve_ihep_gitlab_helper() {
  local script_dir candidate
  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

  for candidate in \
    "$script_dir/../../ihep-gitlab/scripts/ihep-gitlab.sh" \
    "$script_dir/../../../../IHEP-cluster-skill/skills/ihep-gitlab/scripts/ihep-gitlab.sh" \
    "$ALICE_HOME_DIR/.codex/skills/ihep-gitlab/scripts/ihep-gitlab.sh" \
    "$ALICE_HOME_DIR"/bots/*/.codex/skills/ihep-gitlab/scripts/ihep-gitlab.sh \
    "$HOME/.alice/.codex/skills/ihep-gitlab/scripts/ihep-gitlab.sh" \
    "$HOME/.alice"/bots/*/.codex/skills/ihep-gitlab/scripts/ihep-gitlab.sh
  do
    if [[ -x "$candidate" ]]; then
      printf '%s\n' "$candidate"
      return 0
    fi
  done
  if command -v ihep-gitlab.sh >/dev/null 2>&1; then
    command -v ihep-gitlab.sh
    return 0
  fi
  return 1
}

resolve_alice_bin() {
  if [[ -n "${ALICE_RUNTIME_BIN:-}" ]]; then
    printf '%s\n' "$ALICE_RUNTIME_BIN"
    return
  fi
  if [[ -x "$ALICE_HOME_DIR/bin/alice" ]]; then
    printf '%s\n' "$ALICE_HOME_DIR/bin/alice"
    return
  fi
  if command -v alice >/dev/null 2>&1; then
    command -v alice
    return
  fi
  die "unable to locate alice runtime binary"
}

ALICE_BIN="$(resolve_alice_bin)"

run_campaigns() {
  "$ALICE_BIN" runtime campaigns "$@"
}

campaign_json() {
  local campaign_id="$1"
  run_campaigns get "$campaign_id"
}

# Keep campaign sync idempotent by collapsing exact duplicate log entries.
campaign_exact_dedupe_patch_json() {
  local payload="$1"
  jq -c '
    def dedupe_guidance($arr):
      reduce ($arr // [])[] as $item ([]; 
        if any(.[]; (.source // "") == ($item.source // "") and (.command // "") == ($item.command // "") and (.summary // "") == ($item.summary // ""))
        then .
        else . + [$item]
        end
      );
    def dedupe_reviews($arr):
      reduce ($arr // [])[] as $item ([]; 
        if any(.[]; (.reviewer_id // "") == ($item.reviewer_id // "") and (.verdict // "") == ($item.verdict // "") and (.summary // "") == ($item.summary // ""))
        then .
        else . + [$item]
        end
      );
    def dedupe_pitfalls($arr):
      reduce ($arr // [])[] as $item ([]; 
        if any(.[]; (.summary // "") == ($item.summary // "") and (.reason // "") == ($item.reason // "") and (.related_trial_id // "") == ($item.related_trial_id // ""))
        then .
        else . + [$item]
        end
      );
    .campaign | {
      guidance: dedupe_guidance(.guidance),
      reviews: dedupe_reviews(.reviews),
      pitfalls: dedupe_pitfalls(.pitfalls)
    }
  ' <<<"$payload"
}

apply_campaign_exact_dedupe_patch() {
  local payload="$1" patch_json="$2"
  jq -c --argjson patch "$patch_json" '
    .campaign.guidance = $patch.guidance
    | .campaign.reviews = $patch.reviews
    | .campaign.pitfalls = $patch.pitfalls
  ' <<<"$payload"
}

normalized_campaign_json() {
  local campaign_id="$1" payload patch_json
  payload="$(campaign_json "$campaign_id")"
  patch_json="$(campaign_exact_dedupe_patch_json "$payload")"
  apply_campaign_exact_dedupe_patch "$payload" "$patch_json"
}

ensure_campaign_exact_entries_deduped() {
  local campaign_id="$1" payload current_patch wanted_patch
  payload="$(campaign_json "$campaign_id")"
  current_patch="$(jq -c '
    .campaign | {
      guidance: (.guidance // []),
      reviews: (.reviews // []),
      pitfalls: (.pitfalls // [])
    }
  ' <<<"$payload")"
  wanted_patch="$(campaign_exact_dedupe_patch_json "$payload")"
  if [[ "$current_patch" != "$wanted_patch" ]]; then
    patch_campaign "$campaign_id" "$wanted_patch"
    payload="$(campaign_json "$campaign_id")"
  fi
  printf '%s\n' "$payload"
}

campaign_exists() {
  local campaign_id="$1"
  campaign_json "$campaign_id" >/dev/null
}

campaign_repo() {
  local payload="$1"
  jq -r '.campaign.repo | if . == null then "" else tostring end' <<<"$payload"
}

campaign_issue_iid() {
  local payload="$1"
  jq -r '.campaign.issue_iid | if . == null then "" else tostring end' <<<"$payload"
}

require_visible_create_payload() {
  local create_json="$1" repo issue_iid
  repo="$(jq -r '.repo | if . == null then "" else tostring end' <<<"$create_json")"
  issue_iid="$(jq -r '.issue_iid | if . == null then "" else tostring end' <<<"$create_json")"
  [[ -n "$repo" ]] || die "visible campaign requires repo in create payload"
  [[ -n "$issue_iid" ]] || die "visible campaign requires issue_iid; create or bind a GitLab issue first"
}

find_trial_json() {
  local campaign_payload="$1" trial_id="$2"
  jq -ce --arg trial_id "$trial_id" '
    .campaign.trials[] | select(.id == $trial_id)
  ' <<<"$campaign_payload"
}

extract_mr_iid() {
  local ref="$1"
  ref="${ref#"${ref%%[![:space:]]*}"}"
  ref="${ref%"${ref##*[![:space:]]}"}"
  if [[ -z "$ref" ]]; then
    die "merge request reference is empty"
  fi
  if [[ "$ref" =~ ^!([0-9]+)$ ]]; then
    printf '%s\n' "${BASH_REMATCH[1]}"
    return
  fi
  if [[ "$ref" =~ ^[0-9]+$ ]]; then
    printf '%s\n' "$ref"
    return
  fi
  if [[ "$ref" =~ /merge_requests/([0-9]+) ]]; then
    printf '%s\n' "${BASH_REMATCH[1]}"
    return
  fi
  die "unable to parse merge request iid from ${ref}"
}

campaign_issue_sync_marker() {
  local campaign_id="$1"
  printf '<!-- alice-code-army:campaign-sync:%s -->\n' "$campaign_id"
}

campaign_issue_legacy_match() {
  local campaign_id="$1"
  printf '# Alice Code Army Campaign Sync\n\n- campaign: `%s`' "$campaign_id"
}

trial_sync_marker() {
  local campaign_id="$1" trial_id="$2"
  printf '<!-- alice-code-army:trial-sync:%s:%s -->\n' "$campaign_id" "$trial_id"
}

trial_sync_legacy_match() {
  local campaign_id="$1" trial_id="$2"
  printf '# Alice Code Army Trial Sync\n\n- campaign: `%s`\n- trial: `%s`' "$campaign_id" "$trial_id"
}

prepend_managed_note_marker() {
  local marker="$1" body="$2"
  printf '%s\n%s\n' "$marker" "$body"
}

gitlab_note_issue() {
  local repo="$1" iid="$2" body="$3" marker="${4:-}" legacy_match="${5:-}" helper=""
  if helper="$(resolve_ihep_gitlab_helper 2>/dev/null)"; then
    local -a cmd
    cmd=("$helper" issue-note --host "$DEFAULT_GITLAB_HOST" --repo "$repo" --iid "$iid" --message "$body")
    if [[ -n "$marker" ]]; then
      cmd+=(--upsert-marker "$marker")
    fi
    if [[ -n "$legacy_match" ]]; then
      cmd+=(--upsert-fallback-substring "$legacy_match")
    fi
    if "${cmd[@]}"; then
      return
    fi
    if [[ -n "$marker" || -n "$legacy_match" ]]; then
      die "managed issue note sync failed via ihep-gitlab helper for ${repo}#${iid}; refusing raw glab fallback"
    fi
  elif [[ -n "$marker" || -n "$legacy_match" ]]; then
    die "managed issue note sync requires ihep-gitlab helper for ${repo}#${iid}; refusing raw glab fallback"
  fi
  require_cmd glab
  GITLAB_HOST="$DEFAULT_GITLAB_HOST" glab issue note "$iid" -R "$repo" -m "$body"
}

gitlab_note_mr() {
  local repo="$1" iid="$2" body="$3" marker="${4:-}" legacy_match="${5:-}" helper=""
  if helper="$(resolve_ihep_gitlab_helper 2>/dev/null)"; then
    local -a cmd
    cmd=("$helper" mr-note --host "$DEFAULT_GITLAB_HOST" --repo "$repo" --iid "$iid" --message "$body")
    if [[ -n "$marker" ]]; then
      cmd+=(--upsert-marker "$marker")
    fi
    if [[ -n "$legacy_match" ]]; then
      cmd+=(--upsert-fallback-substring "$legacy_match")
    fi
    if "${cmd[@]}"; then
      return
    fi
    if [[ -n "$marker" || -n "$legacy_match" ]]; then
      die "managed MR note sync failed via ihep-gitlab helper for ${repo}!${iid}; refusing raw glab fallback"
    fi
  elif [[ -n "$marker" || -n "$legacy_match" ]]; then
    die "managed MR note sync requires ihep-gitlab helper for ${repo}!${iid}; refusing raw glab fallback"
  fi
  require_cmd glab
  GITLAB_HOST="$DEFAULT_GITLAB_HOST" glab mr note "$iid" -R "$repo" -m "$body"
}

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

append_guidance() {
  local campaign_id="$1" source="$2" command_text="$3" summary="$4"
  local payload
  payload="$(jq -cn \
    --arg source "$source" \
    --arg command "$command_text" \
    --arg summary "$summary" \
    '{guidance:{source:$source, command:$command, summary:$summary, applied:true}}')"
  run_campaigns add-guidance "$campaign_id" "$payload" >/dev/null
}

patch_campaign() {
  local campaign_id="$1" patch_json="$2"
  run_campaigns patch "$campaign_id" "$patch_json" >/dev/null
}

upsert_trial_json() {
  local campaign_id="$1" trial_json="$2"
  local payload
  payload="$(jq -cn --argjson trial "$trial_json" '{trial:$trial}')"
  run_campaigns upsert-trial "$campaign_id" "$payload" >/dev/null
}

mutate_campaign_and_sync_issue() {
  local subcmd="$1" campaign_id="$2" payload_json="$3"
  run_campaigns "$subcmd" "$campaign_id" "$payload_json" >/dev/null
  sync_issue "$campaign_id" >/dev/null
  campaign_json "$campaign_id"
}

create_visible() {
  local create_json="$1" created campaign_id
  require_visible_create_payload "$create_json"
  created="$(run_campaigns create "$create_json")"
  campaign_id="$(jq -r '.campaign.id // ""' <<<"$created")"
  [[ -n "$campaign_id" ]] || die "failed to extract campaign id from create response"
  sync_issue "$campaign_id" >/dev/null
  campaign_json "$campaign_id"
}

upsert_trial_and_sync() {
  local campaign_id="$1" payload_json="$2" trial_id payload merge_request
  trial_id="$(jq -r '.trial.id // ""' <<<"$payload_json")"
  [[ -n "$trial_id" ]] || die "trial payload missing trial.id"
  run_campaigns upsert-trial "$campaign_id" "$payload_json" >/dev/null
  sync_issue "$campaign_id" >/dev/null
  payload="$(campaign_json "$campaign_id")"
  merge_request="$(jq -r --arg trial_id "$trial_id" '
    .campaign.trials[]? | select(.id == $trial_id) | .merge_request | if . == null then "" else tostring end
  ' <<<"$payload")"
  if [[ -n "$merge_request" ]]; then
    sync_trial "$campaign_id" "$trial_id" >/dev/null
    payload="$(campaign_json "$campaign_id")"
  fi
  printf '%s\n' "$payload"
}

apply_command() {
  local campaign_id="$1" command_text="$2" source="${3:-manual}"
  local payload trial_id current trial_json updated_trial patch_json winner_id summary merge_request
  payload="$(campaign_json "$campaign_id")"
  command_text="${command_text#"${command_text%%[![:space:]]*}"}"
  command_text="${command_text%"${command_text##*[![:space:]]}"}"
  [[ -n "$command_text" ]] || die "command text is empty"

  if [[ "$command_text" == "/alice hold" ]]; then
    summary="Campaign put on hold by guidance"
    patch_json="$(jq -cn --arg status "hold" --arg summary "$summary" '{status:$status, summary:$summary}')"
    patch_campaign "$campaign_id" "$patch_json"
  elif [[ "$command_text" =~ ^/alice[[:space:]]+cancel[[:space:]]+([^[:space:]]+)$ ]]; then
    trial_id="${BASH_REMATCH[1]}"
    trial_json="$(find_trial_json "$payload" "$trial_id")" || die "trial ${trial_id} not found"
    updated_trial="$(jq -c --arg summary "Canceled by guidance: ${command_text}" '
      .status = "aborted"
      | .verdict = "aborted"
      | .summary = $summary
    ' <<<"$trial_json")"
    upsert_trial_json "$campaign_id" "$updated_trial"
    winner_id="$(jq -r '.campaign.current_winner_trial_id // ""' <<<"$payload")"
    if [[ "$winner_id" == "$trial_id" ]]; then
      patch_campaign "$campaign_id" '{"current_winner_trial_id":""}'
    fi
    summary="Canceled ${trial_id}"
  elif [[ "$command_text" =~ ^/alice[[:space:]]+accept[[:space:]]+([^[:space:]]+)$ ]]; then
    trial_id="${BASH_REMATCH[1]}"
    trial_json="$(find_trial_json "$payload" "$trial_id")" || die "trial ${trial_id} not found"
    updated_trial="$(jq -c '
      if (.status == "merged" or .status == "completed") then . else .status = "candidate" end
    ' <<<"$trial_json")"
    upsert_trial_json "$campaign_id" "$updated_trial"
    patch_json="$(jq -cn \
      --arg winner "$trial_id" \
      --arg status "running" \
      --arg summary "Accepted current winner candidate: ${trial_id}" \
      '{current_winner_trial_id:$winner, status:$status, summary:$summary}')"
    patch_campaign "$campaign_id" "$patch_json"
    summary="Accepted ${trial_id} as current winner"
  elif [[ "$command_text" =~ ^/alice[[:space:]]+steer[[:space:]]+(.+)$ ]]; then
    summary="Updated campaign direction: ${BASH_REMATCH[1]}"
    patch_json="$(jq -cn --arg summary "$summary" '{summary:$summary}')"
    patch_campaign "$campaign_id" "$patch_json"
  else
    die "unsupported command: ${command_text}"
  fi

  append_guidance "$campaign_id" "$source" "$command_text" "$summary"
  sync_issue "$campaign_id" >/dev/null
  if [[ -n "${trial_id:-}" ]]; then
    payload="$(campaign_json "$campaign_id")"
    merge_request="$(jq -r --arg trial_id "$trial_id" '
      .campaign.trials[]? | select(.id == $trial_id) | .merge_request | if . == null then "" else tostring end
    ' <<<"$payload")"
    if [[ -n "$merge_request" ]]; then
      sync_trial "$campaign_id" "$trial_id" >/dev/null
    fi
  fi
  run_campaigns get "$campaign_id"
}

sync_issue() {
  local campaign_id="$1" payload repo issue_iid body marker legacy_match
  payload="$(ensure_campaign_exact_entries_deduped "$campaign_id")"
  repo="$(campaign_repo "$payload")"
  issue_iid="$(campaign_issue_iid "$payload")"
  [[ -n "$repo" ]] || die "campaign repo is empty"
  [[ -n "$issue_iid" ]] || die "campaign ${campaign_id} is not GitLab-visible yet: missing issue_iid"
  body="$(render_issue_note "$campaign_id")"
  marker="$(campaign_issue_sync_marker "$campaign_id")"
  legacy_match="$(campaign_issue_legacy_match "$campaign_id")"
  body="$(prepend_managed_note_marker "$marker" "$body")"
  gitlab_note_issue "$repo" "$issue_iid" "$body" "$marker" "$legacy_match"
}

sync_trial() {
  local campaign_id="$1" trial_id="$2" payload repo merge_request mr_iid body marker legacy_match
  payload="$(ensure_campaign_exact_entries_deduped "$campaign_id")"
  repo="$(jq -r '.campaign.repo // ""' <<<"$payload")"
  [[ -n "$repo" ]] || die "campaign repo is empty"
  merge_request="$(jq -r --arg trial_id "$trial_id" '
    .campaign.trials[] | select(.id == $trial_id) | .merge_request // ""
  ' <<<"$payload")"
  [[ -n "$merge_request" ]] || die "trial ${trial_id} has no merge_request"
  mr_iid="$(extract_mr_iid "$merge_request")"
  body="$(render_trial_note "$campaign_id" "$trial_id")"
  marker="$(trial_sync_marker "$campaign_id" "$trial_id")"
  legacy_match="$(trial_sync_legacy_match "$campaign_id" "$trial_id")"
  body="$(prepend_managed_note_marker "$marker" "$body")"
  gitlab_note_mr "$repo" "$mr_iid" "$body" "$marker" "$legacy_match"
}

sync_all() {
  local campaign_id="$1" payload trial_ids trial_id
  sync_issue "$campaign_id"
  payload="$(campaign_json "$campaign_id")"
  mapfile -t trial_ids < <(jq -r '
    .campaign.trials[]
    | select((.merge_request // "") != "")
    | .id
  ' <<<"$payload")
  for trial_id in "${trial_ids[@]}"; do
    sync_trial "$campaign_id" "$trial_id"
  done
}

main() {
  local cmd="${1:-help}"
  case "$cmd" in
    help|-h|--help)
      usage
      ;;
    list|get)
      shift
      exec "$ALICE_BIN" runtime campaigns "$cmd" "$@"
      ;;
    create)
      [[ $# -eq 2 ]] || die "usage: $PROGRAM create CREATE_JSON"
      create_visible "$2"
      ;;
    patch)
      [[ $# -eq 3 ]] || die "usage: $PROGRAM patch CAMPAIGN_ID PATCH_JSON"
      mutate_campaign_and_sync_issue "patch" "$2" "$3"
      ;;
    upsert-trial)
      [[ $# -eq 3 ]] || die "usage: $PROGRAM upsert-trial CAMPAIGN_ID TRIAL_PAYLOAD_JSON"
      upsert_trial_and_sync "$2" "$3"
      ;;
    add-guidance)
      [[ $# -eq 3 ]] || die "usage: $PROGRAM add-guidance CAMPAIGN_ID GUIDANCE_PAYLOAD_JSON"
      mutate_campaign_and_sync_issue "add-guidance" "$2" "$3"
      ;;
    add-review)
      [[ $# -eq 3 ]] || die "usage: $PROGRAM add-review CAMPAIGN_ID REVIEW_PAYLOAD_JSON"
      mutate_campaign_and_sync_issue "add-review" "$2" "$3"
      ;;
    add-pitfall)
      [[ $# -eq 3 ]] || die "usage: $PROGRAM add-pitfall CAMPAIGN_ID PITFALL_PAYLOAD_JSON"
      mutate_campaign_and_sync_issue "add-pitfall" "$2" "$3"
      ;;
    render-issue-note)
      [[ $# -eq 2 ]] || die "usage: $PROGRAM render-issue-note CAMPAIGN_ID"
      render_issue_note "$2"
      ;;
    render-trial-note)
      [[ $# -eq 3 ]] || die "usage: $PROGRAM render-trial-note CAMPAIGN_ID TRIAL_ID"
      render_trial_note "$2" "$3"
      ;;
    sync-issue)
      [[ $# -eq 2 ]] || die "usage: $PROGRAM sync-issue CAMPAIGN_ID"
      sync_issue "$2"
      ;;
    sync-trial)
      [[ $# -eq 3 ]] || die "usage: $PROGRAM sync-trial CAMPAIGN_ID TRIAL_ID"
      sync_trial "$2" "$3"
      ;;
    sync-all)
      [[ $# -eq 2 ]] || die "usage: $PROGRAM sync-all CAMPAIGN_ID"
      sync_all "$2"
      ;;
    apply-command)
      [[ $# -ge 3 && $# -le 4 ]] || die "usage: $PROGRAM apply-command CAMPAIGN_ID COMMAND [SOURCE]"
      apply_command "$2" "$3" "${4:-manual}"
      ;;
    *)
      die "unknown command: ${cmd}"
      ;;
  esac
}

main "$@"
