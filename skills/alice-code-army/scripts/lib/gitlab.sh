# shellcheck shell=bash

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

gitlab_issue_time_api() {
  local repo="$1" iid="$2" suffix="$3"
  shift 3

  local helper="" endpoint
  endpoint="projects/$(repo_api_path "$repo")/issues/${iid}/${suffix}"
  if helper="$(resolve_ihep_gitlab_helper 2>/dev/null)"; then
    "$helper" api --host "$DEFAULT_GITLAB_HOST" "$endpoint" "$@"
    return
  fi
  require_cmd glab
  glab api --hostname "$DEFAULT_GITLAB_HOST" "$endpoint" "$@"
}

time_stats() {
  local campaign_id="$1" repo issue_iid
  IFS=$'\t' read -r repo issue_iid <<<"$(campaign_gitlab_target "$campaign_id")"
  gitlab_issue_time_api "$repo" "$issue_iid" "time_stats"
}

time_estimate() {
  local campaign_id="$1" duration="$2" repo issue_iid
  [[ -n "$duration" ]] || die "duration is empty"
  IFS=$'\t' read -r repo issue_iid <<<"$(campaign_gitlab_target "$campaign_id")"
  gitlab_issue_time_api "$repo" "$issue_iid" "time_estimate" --method POST -F duration="$duration"
}

time_spend() {
  local campaign_id="$1" duration="$2" summary="${3:-}" repo issue_iid
  [[ -n "$duration" ]] || die "duration is empty"
  IFS=$'\t' read -r repo issue_iid <<<"$(campaign_gitlab_target "$campaign_id")"

  local -a cmd
  cmd=(gitlab_issue_time_api "$repo" "$issue_iid" "add_spent_time" --method POST -F duration="$duration")
  if [[ -n "$summary" ]]; then
    cmd+=(-F summary="$summary")
  fi
  "${cmd[@]}"
}

