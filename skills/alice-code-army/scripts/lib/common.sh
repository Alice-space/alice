# shellcheck shell=bash

usage() {
  cat <<EOF
Usage:
  $PROGRAM list|get|create|init-repo|repo-scan|repo-reconcile|patch|upsert-trial|add-guidance|add-review|add-pitfall ...
  $PROGRAM apply-command CAMPAIGN_ID COMMAND [SOURCE]
  $PROGRAM render-issue-note CAMPAIGN_ID
  $PROGRAM render-trial-note CAMPAIGN_ID TRIAL_ID
  $PROGRAM time-stats CAMPAIGN_ID
  $PROGRAM time-estimate CAMPAIGN_ID DURATION
  $PROGRAM time-spend CAMPAIGN_ID DURATION [SUMMARY]
  $PROGRAM sync-issue CAMPAIGN_ID
  $PROGRAM sync-trial CAMPAIGN_ID TRIAL_ID
  $PROGRAM sync-all CAMPAIGN_ID

Environment:
  ALICE_RUNTIME_BIN            Override the alice runtime binary path.
  ALICE_HOME                   Override Alice home (default: ~/.alice).
  ALICE_CODE_ARMY_GITLAB_HOST  Default GitLab host for sync commands (default: code.ihep.ac.cn).

Repo-first contract:
  create initializes a campaign and, by default, scaffolds a campaign repo template.
  campaign_repo_path in the payload is optional; if omitted, a local ./campaigns/<slug> path is used.
  Alice also runs a background repo reconciler that refreshes live-report and syncs wake tasks from task frontmatter.
  GitLab issue / MR sync is optional mirror behavior, no longer the primary state path.
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
  script_dir="$SCRIPT_DIR"

  for candidate in \
    "$script_dir/../../ihep-gitlab/scripts/ihep-gitlab.sh" \
    "$script_dir/../../../../IHEP-cluster-skill/skills/ihep-gitlab/scripts/ihep-gitlab.sh" \
    "$ALICE_HOME_DIR/.codex/skills/ihep-gitlab/scripts/ihep-gitlab.sh" \
    "$HOME/.alice/.codex/skills/ihep-gitlab/scripts/ihep-gitlab.sh"
  do
    if [[ -x "$candidate" ]]; then
      printf '%s\n' "$candidate"
      return 0
    fi
  done

  local -a helper_patterns=(
    "$ALICE_HOME_DIR"/bots/*/.codex/skills/ihep-gitlab/scripts/ihep-gitlab.sh
    "$HOME/.alice"/bots/*/.codex/skills/ihep-gitlab/scripts/ihep-gitlab.sh
  )
  local -a helper_matches=()
  local pattern match
  shopt -s nullglob
  for pattern in "${helper_patterns[@]}"; do
    for match in $pattern; do
      [[ -x "$match" ]] || continue
      helper_matches+=("$match")
    done
  done
  shopt -u nullglob
  if (( ${#helper_matches[@]} == 1 )); then
    printf '%s\n' "${helper_matches[0]}"
    return 0
  fi
  if (( ${#helper_matches[@]} > 1 )); then
    printf '[alice-code-army] ERROR: multiple ihep-gitlab helpers found; set ALICE_HOME or PATH explicitly\n' >&2
    printf '%s\n' "${helper_matches[@]}" >&2
    return 1
  fi
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

campaign_repo_path() {
  local payload="$1"
  jq -r '.campaign.campaign_repo_path | if . == null then "" else tostring end' <<<"$payload"
}

campaign_issue_iid() {
  local payload="$1"
  jq -r '.campaign.issue_iid | if . == null then "" else tostring end' <<<"$payload"
}

skill_script_dir() {
  cd "$SCRIPT_DIR" && pwd
}

campaign_repo_template_root() {
  printf '%s\n' "$(skill_script_dir)/../templates/campaign-repo"
}

slugify() {
  local raw="${1:-}"
  raw="$(printf '%s' "$raw" | tr '[:upper:]' '[:lower:]')"
  raw="$(printf '%s' "$raw" | tr -cs '[:alnum:]' '-')"
  raw="${raw#-}"
  raw="${raw%-}"
  if [[ -z "$raw" ]]; then
    raw="campaign"
  fi
  printf '%s\n' "$raw"
}

default_campaign_repo_path() {
  local campaign_id="$1" payload="$2" title slug
  title="$(jq -r '.campaign.title | if . == null then "" else tostring end' <<<"$payload")"
  slug="$(slugify "${title:-$campaign_id}")"
  printf '%s/campaigns/%s\n' "$(pwd)" "$slug"
}

escape_sed_replacement() {
  printf '%s' "$1" | sed -e 's/[\\/&]/\\&/g'
}

repo_api_path() {
  printf '%s' "${1//\//%2F}"
}

campaign_gitlab_target() {
  local campaign_id="$1" payload repo issue_iid
  payload="$(campaign_json "$campaign_id")"
  repo="$(campaign_repo "$payload")"
  issue_iid="$(campaign_issue_iid "$payload")"
  [[ -n "$repo" ]] || die "campaign repo is empty"
  [[ -n "$issue_iid" ]] || die "campaign ${campaign_id} is not GitLab-visible yet: missing issue_iid"
  printf '%s\t%s\n' "$repo" "$issue_iid"
}

require_visible_create_payload() {
  local create_json="$1" repo issue_iid
  repo="$(jq -r '.repo | if . == null then "" else tostring end' <<<"$create_json")"
  issue_iid="$(jq -r '.issue_iid | if . == null then "" else tostring end' <<<"$create_json")"
  [[ -n "$repo" ]] || die "visible campaign requires repo in create payload"
  [[ -n "$issue_iid" ]] || die "visible campaign requires issue_iid; create or bind a GitLab issue first"
}

