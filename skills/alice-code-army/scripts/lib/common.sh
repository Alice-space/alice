# shellcheck shell=bash

usage() {
  cat <<EOF
Usage:
  $PROGRAM list|get|delete|create|bootstrap|init-repo|repo-scan|repo-reconcile|repo-lint|task-self-check|task-guidance|patch ...
  $PROGRAM approve-plan CAMPAIGN_ID
  $PROGRAM plan-status CAMPAIGN_ID
  $PROGRAM task-self-check CAMPAIGN_ID TASK_ID executor|reviewer
  $PROGRAM task-guidance CAMPAIGN_ID TASK_ID accept|resume GUIDANCE
  $PROGRAM apply-command CAMPAIGN_ID COMMAND [SOURCE]

Environment:
  ALICE_RUNTIME_BIN  Override the alice runtime binary path.
  ALICE_HOME         Override Alice home (default: ~/.alice).

Repo-first contract:
  create initializes a campaign and, by default, scaffolds a campaign repo template.
  bootstrap is the safe-start entrypoint: it creates the campaign, fills baseline repo facts, and runs repo-reconcile to dispatch the official planner.
  repo-lint validates the campaign repo contract; use --for-approval before human approval.
  delete removes the runtime campaign record; pass --delete-repo to also remove the local campaign repo path.
  campaign_repo_path in the payload is optional; if omitted, a local ./campaigns/<slug> path is used.
  Alice also runs a background repo reconciler that refreshes live-report and syncs wake tasks from task frontmatter.
  Campaign repo markdown/frontmatter is the only shipped long-term state path for this skill.
EOF
}

die() {
  printf '[alice-code-army] ERROR: %s\n' "$*" >&2
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"
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

campaign_repo() {
  local payload="$1"
  jq -r '.campaign.repo | if . == null then "" else tostring end' <<<"$payload"
}

campaign_repo_path() {
  local payload="$1"
  jq -r '.campaign.campaign_repo_path | if . == null then "" else tostring end' <<<"$payload"
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

git_config_get_optional() {
  local repo_path="$1" scope="$2" key="$3"
  git -C "$repo_path" config "$scope" --get "$key" 2>/dev/null || true
}

resolve_git_identity_for_repo() {
  local repo_path="$1"
  local local_name local_email global_name global_email

  RESOLVED_GIT_IDENTITY_NAME=""
  RESOLVED_GIT_IDENTITY_EMAIL=""

  local_name="$(git_config_get_optional "$repo_path" --local user.name)"
  local_email="$(git_config_get_optional "$repo_path" --local user.email)"
  if [[ -n "$local_name" || -n "$local_email" ]]; then
    [[ -n "$local_name" && -n "$local_email" ]] || die "git identity incomplete in local config for ${repo_path}: set both user.name and user.email or clear the partial config"
    RESOLVED_GIT_IDENTITY_NAME="$local_name"
    RESOLVED_GIT_IDENTITY_EMAIL="$local_email"
    return 0
  fi

  global_name="$(git_config_get_optional "$repo_path" --global user.name)"
  global_email="$(git_config_get_optional "$repo_path" --global user.email)"
  if [[ -n "$global_name" || -n "$global_email" ]]; then
    [[ -n "$global_name" && -n "$global_email" ]] || die "git identity incomplete in global config for ${repo_path}: set both user.name and user.email"
    RESOLVED_GIT_IDENTITY_NAME="$global_name"
    RESOLVED_GIT_IDENTITY_EMAIL="$global_email"
    return 0
  fi

  die "git identity required for ${repo_path}: set repo local user.name/user.email or global user.name/user.email before Alice commits or merges"
}

commit_campaign_repo_if_dirty() {
  local repo_path="$1" message="${2:-chore(campaign): update repo state}"
  [[ -n "$repo_path" && -d "$repo_path" ]] || return 0
  require_cmd git
  if ! git -C "$repo_path" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    return 0
  fi
  if [[ -z "$(git -C "$repo_path" status --porcelain)" ]]; then
    return 0
  fi
  resolve_git_identity_for_repo "$repo_path"
  git -C "$repo_path" add -A
  if [[ -z "$(git -C "$repo_path" status --porcelain)" ]]; then
    return 0
  fi
  env \
    -u GIT_AUTHOR_NAME \
    -u GIT_AUTHOR_EMAIL \
    -u GIT_AUTHOR_DATE \
    -u GIT_COMMITTER_NAME \
    -u GIT_COMMITTER_EMAIL \
    -u GIT_COMMITTER_DATE \
    git -C "$repo_path" \
    -c "user.name=${RESOLVED_GIT_IDENTITY_NAME}" \
    -c "user.email=${RESOLVED_GIT_IDENTITY_EMAIL}" \
    commit -q -m "$message"
}
