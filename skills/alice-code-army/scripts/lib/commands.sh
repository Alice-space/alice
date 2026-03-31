# shellcheck shell=bash

patch_campaign() {
  local campaign_id="$1" patch_json="$2"
  run_campaigns patch "$campaign_id" "$patch_json" >/dev/null
}

mutate_campaign_and_return() {
  local subcmd="$1" campaign_id="$2" payload_json="$3"
  run_campaigns "$subcmd" "$campaign_id" "$payload_json" >/dev/null
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

apply_command() {
  local campaign_id="$1" command_text="$2" source="${3:-manual}"
  local payload patch_json summary
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
  elif [[ "$command_text" == "/alice approve-plan" ]]; then
    approve_plan "$campaign_id" >/dev/null
    summary="Plan approved by human after repo-lint and plan review gate"
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
      commit_campaign_repo_if_dirty "$repo_path" "chore(campaign): apply replan guidance"
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
      commit_campaign_repo_if_dirty "$repo_path" "chore(campaign): record discovery"
    fi
    patch_json="$(jq -cn --arg summary "$summary" '{summary:$summary}')"
    patch_campaign "$campaign_id" "$patch_json"
  else
    die "unsupported command: ${command_text}"
  fi

  run_campaigns get "$campaign_id"
}

approve_plan() {
  local campaign_id="$1"
  run_campaigns approve-plan "$campaign_id" >/dev/null
  run_campaigns repo-reconcile "$campaign_id" >/dev/null
  run_campaigns get "$campaign_id"
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
