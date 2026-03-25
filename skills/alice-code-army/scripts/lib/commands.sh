# shellcheck shell=bash

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

mutate_campaign_and_return() {
  local subcmd="$1" campaign_id="$2" payload_json="$3"
  run_campaigns "$subcmd" "$campaign_id" "$payload_json" >/dev/null
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

create_repo_first() {
  local create_json="$1" created campaign_id requested_path
  created="$(run_campaigns create "$create_json")"
  campaign_id="$(jq -r '.campaign.id // ""' <<<"$created")"
  [[ -n "$campaign_id" ]] || die "failed to extract campaign id from create response"
  requested_path="$(jq -r '.campaign_repo_path | if . == null then "" else tostring end' <<<"$create_json")"
  init_campaign_repo "$campaign_id" "$requested_path"
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

upsert_trial_and_return() {
  local campaign_id="$1" payload_json="$2" trial_id
  trial_id="$(jq -r '.trial.id // ""' <<<"$payload_json")"
  [[ -n "$trial_id" ]] || die "trial payload missing trial.id"
  run_campaigns upsert-trial "$campaign_id" "$payload_json" >/dev/null
  campaign_json "$campaign_id"
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
  elif [[ "$command_text" =~ ^/alice[[:space:]]+(needs-human|needs_human|needshuman|needs[[:space:]]+human)([[:space:]]+(.+))?$ ]]; then
    summary="${BASH_REMATCH[3]:-Needs human intervention requested}"
    patch_json="$(jq -cn \
      --arg status "hold" \
      --arg summary "Needs human intervention: ${summary}" \
      '{status:$status, summary:$summary}')"
    patch_campaign "$campaign_id" "$patch_json"
    summary="Needs human intervention: ${summary}"
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
  elif [[ "$command_text" == "/alice approve-plan" ]]; then
    summary="Plan approved by human, starting execution"
    patch_json="$(jq -cn --arg status "running" --arg summary "$summary" '{status:$status, summary:$summary}')"
    patch_campaign "$campaign_id" "$patch_json"
    # Update plan_status in campaign repo if the campaign has a repo path
    local repo_path
    repo_path="$(jq -r '.campaign.campaign_repo_path // ""' <<<"$payload")"
    if [[ -n "$repo_path" && -f "${repo_path}/campaign.md" ]]; then
      update_campaign_plan_status "$repo_path" "human_approved"
    fi
  elif [[ "$command_text" =~ ^/alice[[:space:]]+steer[[:space:]]+(.+)$ ]]; then
    summary="Updated campaign direction: ${BASH_REMATCH[1]}"
    patch_json="$(jq -cn --arg summary "$summary" '{summary:$summary}')"
    patch_campaign "$campaign_id" "$patch_json"
  elif [[ "$command_text" =~ ^/alice[[:space:]]+(replan|re-plan|re_plan)([[:space:]]+(.+))?$ ]]; then
    local replan_reason="${BASH_REMATCH[3]:-Executor requested replanning}"
    summary="Replan requested: ${replan_reason}"
    # Write replan reason to findings.md so planner can read it
    local repo_path
    repo_path="$(jq -r '.campaign.campaign_repo_path // ""' <<<"$payload")"
    if [[ -n "$repo_path" ]]; then
      mkdir -p "$repo_path"
      printf '\n## Replan Request (%s)\n\n%s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$replan_reason" >> "${repo_path}/findings.md"
      # Reset plan_status to planning with incremented plan_round
      if [[ -f "${repo_path}/campaign.md" ]]; then
        local current_round
        current_round="$(sed -n 's/^plan_round:[[:space:]]*//p' "${repo_path}/campaign.md" | head -1)"
        current_round="${current_round:-0}"
        sed -i "s/^plan_round:.*/plan_round: $(( current_round + 1 ))/" "${repo_path}/campaign.md"
        update_campaign_plan_status "$repo_path" "planning"
      fi
    fi
    patch_json="$(jq -cn --arg summary "$summary" '{summary:$summary}')"
    patch_campaign "$campaign_id" "$patch_json"
  elif [[ "$command_text" =~ ^/alice[[:space:]]+blocked([[:space:]]+(.+))?$ ]]; then
    local blocked_reason="${BASH_REMATCH[2]:-No reason given}"
    summary="Task blocked: ${blocked_reason}"
    patch_json="$(jq -cn --arg summary "$summary" '{summary:$summary}')"
    patch_campaign "$campaign_id" "$patch_json"
  elif [[ "$command_text" =~ ^/alice[[:space:]]+discovery([[:space:]]+(.+))?$ ]]; then
    local discovery_finding="${BASH_REMATCH[2]:-No details}"
    summary="Discovery: ${discovery_finding}"
    local repo_path
    repo_path="$(jq -r '.campaign.campaign_repo_path // ""' <<<"$payload")"
    if [[ -n "$repo_path" ]]; then
      mkdir -p "$repo_path"
      printf '\n## Discovery (%s)\n\n%s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$discovery_finding" >> "${repo_path}/findings.md"
    fi
    patch_json="$(jq -cn --arg summary "$summary" '{summary:$summary}')"
    patch_campaign "$campaign_id" "$patch_json"
  else
    die "unsupported command: ${command_text}"
  fi

  append_guidance "$campaign_id" "$source" "$command_text" "$summary"
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

approve_plan() {
  local campaign_id="$1"
  apply_command "$campaign_id" "/alice approve-plan" "manual"
}

plan_status() {
  local campaign_id="$1" payload repo_path plan_status plan_round
  payload="$(campaign_json "$campaign_id")"
  repo_path="$(jq -r '.campaign.campaign_repo_path // ""' <<<"$payload")"
  if [[ -z "$repo_path" || ! -f "${repo_path}/campaign.md" ]]; then
    jq -cn --arg status "no_repo" '{"status":"ok","plan_status":$status,"plan_round":0}'
    return 0
  fi
  plan_status="$(sed -n 's/^plan_status:[[:space:]]*//p' "${repo_path}/campaign.md" | head -1 | tr -d '"' | tr -d "'")"
  plan_round="$(sed -n 's/^plan_round:[[:space:]]*//p' "${repo_path}/campaign.md" | head -1)"
  jq -cn \
    --arg plan_status "${plan_status:-idle}" \
    --argjson plan_round "${plan_round:-0}" \
    '{"status":"ok","plan_status":$plan_status,"plan_round":$plan_round}'
}

update_campaign_plan_status() {
  local repo_path="$1" new_status="$2"
  local campaign_file="${repo_path}/campaign.md"
  [[ -f "$campaign_file" ]] || return 0
  # Use sed to update plan_status in the YAML frontmatter
  if grep -q '^plan_status:' "$campaign_file"; then
    sed -i "s/^plan_status:.*/plan_status: ${new_status}/" "$campaign_file"
  fi
}

