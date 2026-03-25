# shellcheck shell=bash

bootstrap_create_payload() {
  local bootstrap_json="$1"
  jq -c 'del(.source_repo, .source_repos, .research_contract)' <<<"$bootstrap_json"
}

bootstrap_source_repos_json() {
  local bootstrap_json="$1"
  jq -c '
    def to_string_array:
      if . == null then []
      elif type == "array" then [ .[] | tostring ]
      elif type == "string" then [ . ]
      else [ tostring ]
      end;
    (
      if .source_repos != null then .source_repos
      elif .source_repo != null then [ .source_repo ]
      else []
      end
    )
    | map({
        repo_id: (.repo_id // ""),
        remote_url: (.remote_url // ""),
        local_path: (.local_path // .path // ""),
        default_branch: (.default_branch // ""),
        active_branches: ((.active_branches // .branches // []) | to_string_array),
        base_commit: (.base_commit // ""),
        notes: ((.notes // []) | to_string_array)
      })
  ' <<<"$bootstrap_json"
}

bootstrap_contract_json() {
  local bootstrap_json="$1"
  jq -c '
    def to_string_array:
      if . == null then []
      elif type == "array" then [ .[] | tostring ]
      elif type == "string" then [ . ]
      else [ tostring ]
      end;
    {
      objective: (
        if .research_contract.objective != null then .research_contract.objective
        elif .objective != null then .objective
        else []
        end
        | to_string_array
      ),
      constraints: ((.research_contract.constraints // []) | to_string_array),
      success_criteria: ((.research_contract.success_criteria // []) | to_string_array),
      non_goals: ((.research_contract.non_goals // []) | to_string_array),
      human_input: ((.research_contract.human_input // []) | to_string_array)
    }
  ' <<<"$bootstrap_json"
}

detect_git_current_branch() {
  local repo_path="$1"
  if [[ -n "$repo_path" ]] && git -C "$repo_path" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    git -C "$repo_path" branch --show-current 2>/dev/null || true
  fi
}

detect_git_head_commit() {
  local repo_path="$1"
  if [[ -n "$repo_path" ]] && git -C "$repo_path" rev-parse --verify HEAD >/dev/null 2>&1; then
    git -C "$repo_path" rev-parse HEAD 2>/dev/null || true
  fi
}

yaml_escape_double_quoted() {
  printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g'
}

replace_frontmatter_list() {
  local file="$1" key="$2"
  shift 2
  local tmp
  tmp="$(mktemp)"
  awk -v key="$key" -v values="$(printf '%s\034' "$@")" '
    BEGIN {
      raw_count = split(values, raw, "\034")
      count = 0
      for (i = 1; i <= raw_count; i++) {
        if (raw[i] != "") {
          items[++count] = raw[i]
        }
      }
      in_frontmatter = 0
      replaced = 0
      skipping = 0
    }
    NR == 1 && $0 == "---" {
      in_frontmatter = 1
      print
      next
    }
    in_frontmatter && $0 ~ ("^" key ":") {
      if (count == 0) {
        print key ": []"
      } else {
        print key ":"
        for (i = 1; i <= count; i++) {
          print "  - " items[i]
        }
      }
      replaced = 1
      skipping = 1
      next
    }
    skipping {
      if ($0 ~ /^  - /) {
        next
      }
      skipping = 0
    }
    in_frontmatter && $0 == "---" {
      if (!replaced) {
        if (count == 0) {
          print key ": []"
        } else {
          print key ":"
          for (i = 1; i <= count; i++) {
            print "  - " items[i]
          }
        }
        replaced = 1
      }
      in_frontmatter = 0
      print
      next
    }
    { print }
  ' "$file" >"$tmp" || {
    rm -f "$tmp"
    die "failed to update ${key} in ${file}"
  }
  mv "$tmp" "$file"
}

bootstrap_enrich_source_repo_json() {
  local repo_json="$1"
  local repo_id local_path remote_url default_branch active_branch base_commit notes_json active_branches_json

  repo_id="$(jq -r '.repo_id // ""' <<<"$repo_json")"
  local_path="$(jq -r '.local_path // ""' <<<"$repo_json")"
  remote_url="$(jq -r '.remote_url // ""' <<<"$repo_json")"
  default_branch="$(jq -r '.default_branch // ""' <<<"$repo_json")"
  base_commit="$(jq -r '.base_commit // ""' <<<"$repo_json")"
  notes_json="$(jq -c '.notes // []' <<<"$repo_json")"
  active_branches_json="$(jq -c '.active_branches // []' <<<"$repo_json")"

  if [[ -z "$repo_id" && -n "$local_path" ]]; then
    repo_id="$(slugify "$(basename "$local_path")")"
  fi
  [[ -n "$repo_id" ]] || die "bootstrap source repo is missing repo_id/local_path"

  active_branch="$(detect_git_current_branch "$local_path")"
  [[ -n "$default_branch" ]] || default_branch="${active_branch:-master}"
  [[ -n "$base_commit" ]] || base_commit="$(detect_git_head_commit "$local_path")"

  if [[ "$active_branches_json" == "[]" ]]; then
    if [[ -n "$active_branch" ]]; then
      active_branches_json="$(jq -cn --arg branch "$active_branch" '[$branch]')"
    else
      active_branches_json="$(jq -cn --arg branch "$default_branch" '[$branch] | map(select(. != ""))')"
    fi
  fi

  jq -cn \
    --arg repo_id "$repo_id" \
    --arg remote_url "$remote_url" \
    --arg local_path "$local_path" \
    --arg default_branch "$default_branch" \
    --arg base_commit "$base_commit" \
    --argjson active_branches "$active_branches_json" \
    --argjson notes "$notes_json" \
    '{
      repo_id: $repo_id,
      remote_url: $remote_url,
      local_path: $local_path,
      default_branch: $default_branch,
      active_branches: $active_branches,
      base_commit: $base_commit,
      notes: $notes
    }'
}

write_repo_reference_file() {
  local campaign_repo_path="$1" repo_json="$2"
  local file repo_id remote_url local_path default_branch base_commit

  repo_id="$(jq -r '.repo_id' <<<"$repo_json")"
  remote_url="$(jq -r '.remote_url // ""' <<<"$repo_json")"
  local_path="$(jq -r '.local_path // ""' <<<"$repo_json")"
  default_branch="$(jq -r '.default_branch // "master"' <<<"$repo_json")"
  base_commit="$(jq -r '.base_commit // ""' <<<"$repo_json")"
  file="${campaign_repo_path}/repos/${repo_id}.md"

  mkdir -p "${campaign_repo_path}/repos"
  {
    printf -- '---\n'
    printf 'repo_id: %s\n' "$repo_id"
    printf 'remote_url: "%s"\n' "$(yaml_escape_double_quoted "$remote_url")"
    printf 'local_path: "%s"\n' "$(yaml_escape_double_quoted "$local_path")"
    printf 'default_branch: %s\n' "$default_branch"
    printf 'active_branches:\n'
    while IFS= read -r branch; do
      [[ -n "$branch" ]] || continue
      printf '  - %s\n' "$branch"
    done < <(jq -r '.active_branches[]? // empty' <<<"$repo_json")
    printf 'base_commit: "%s"\n' "$(yaml_escape_double_quoted "$base_commit")"
    printf 'role: source\n'
    printf -- '---\n\n'
    printf '# Repo Reference\n\n'
    printf '## Notes\n'
    if jq -e '.notes | length > 0' >/dev/null 2>&1 <<<"$repo_json"; then
      while IFS= read -r note; do
        [[ -n "$note" ]] || continue
        printf -- '- %s\n' "$note"
      done < <(jq -r '.notes[]? // empty' <<<"$repo_json")
    else
      printf -- '- Source repo bootstrap metadata generated automatically.\n'
      if [[ -n "$local_path" ]]; then
        printf -- '- Local path: `%s`\n' "$local_path"
      fi
      if [[ -z "$base_commit" ]]; then
        printf -- '- Current worktree has no verified HEAD commit yet.\n'
      fi
    fi
  } >"$file"
}

write_research_contract_file() {
  local campaign_repo_path="$1" contract_json="$2" source_repos_json="$3"
  local file updated_at source_repo_paths default_constraint
  local -a objective_lines constraints_lines success_lines non_goal_lines human_input_lines

  file="${campaign_repo_path}/docs/research-contract.md"
  updated_at="$(date '+%Y-%m-%dT%H:%M:%S%z' | sed -e 's/\([0-9][0-9]\)$/:\1/')"
  source_repo_paths="$(jq -r '[.[].local_path | select(. != "")] | join(", ")' <<<"$source_repos_json")"
  default_constraint="先完成 planning / review / human approval，再进入执行阶段。"
  if [[ -n "$source_repo_paths" ]]; then
    default_constraint="Source repos: ${source_repo_paths}；先完成 planning / review / human approval，再进入执行阶段。"
  fi

  mapfile -t objective_lines < <(jq -r '.objective[]? // empty' <<<"$contract_json")
  mapfile -t constraints_lines < <(jq -r '.constraints[]? // empty' <<<"$contract_json")
  mapfile -t success_lines < <(jq -r '.success_criteria[]? // empty' <<<"$contract_json")
  mapfile -t non_goal_lines < <(jq -r '.non_goals[]? // empty' <<<"$contract_json")
  mapfile -t human_input_lines < <(jq -r '.human_input[]? // empty' <<<"$contract_json")

  if (( ${#constraints_lines[@]} == 0 )); then
    constraints_lines=("$default_constraint")
  fi
  if (( ${#success_lines[@]} == 0 )); then
    success_lines=("planner 能基于 source_repos 产出 proposal 和 draft tasks。")
  fi
  if (( ${#non_goal_lines[@]} == 0 )); then
    non_goal_lines=("当前不通过 generic reconcile worker 直接跳过 planning gate。")
  fi
  if (( ${#human_input_lines[@]} == 0 )); then
    human_input_lines=("Bootstrap baseline generated by alice-code-army.sh bootstrap.")
  fi

  mkdir -p "${campaign_repo_path}/docs"
  {
    printf -- '---\n'
    printf 'status: draft\n'
    printf 'owner: planner\n'
    printf 'updated_at: "%s"\n' "$updated_at"
    printf -- '---\n\n'
    printf '# Research Contract\n\n'

    printf '## Objective\n'
    if (( ${#objective_lines[@]} == 0 )); then
      printf -- '- 待补充\n'
    else
      for line in "${objective_lines[@]}"; do
        [[ -n "$line" ]] || continue
        printf -- '- %s\n' "$line"
      done
    fi
    printf '\n'

    printf '## Constraints\n'
    for line in "${constraints_lines[@]}"; do
      [[ -n "$line" ]] || continue
      printf -- '- %s\n' "$line"
    done
    printf '\n'

    printf '## Success Criteria\n'
    for line in "${success_lines[@]}"; do
      [[ -n "$line" ]] || continue
      printf -- '- %s\n' "$line"
    done
    printf '\n'

    printf '## Non-goals\n'
    for line in "${non_goal_lines[@]}"; do
      [[ -n "$line" ]] || continue
      printf -- '- %s\n' "$line"
    done
    printf '\n'

    printf '## Human Input\n'
    for line in "${human_input_lines[@]}"; do
      [[ -n "$line" ]] || continue
      printf -- '- %s\n' "$line"
    done
  } >"$file"
}

bootstrap_repo_first() {
  local bootstrap_json="$1"
  local create_json created campaign_id repo_path source_repos_json contract_json
  local enriched_source_repos_json repo_ids_json
  local -a repo_ids

  create_json="$(bootstrap_create_payload "$bootstrap_json")"
  created="$(create_repo_first "$create_json")"
  campaign_id="$(jq -r '.campaign.id // ""' <<<"$created")"
  [[ -n "$campaign_id" ]] || die "failed to extract campaign id from bootstrap response"
  repo_path="$(campaign_repo_path "$created")"
  [[ -n "$repo_path" ]] || die "campaign ${campaign_id} has no campaign_repo_path"

  ensure_campaign_repo_writable "$repo_path"

  source_repos_json="$(bootstrap_source_repos_json "$bootstrap_json")"
  enriched_source_repos_json="$(
    jq -cn --argjson repos "$source_repos_json" '
      $repos | map(.)
    '
  )"
  if jq -e 'length > 0' >/dev/null 2>&1 <<<"$source_repos_json"; then
    enriched_source_repos_json="$(
      while IFS= read -r repo_item; do
        bootstrap_enrich_source_repo_json "$repo_item"
      done < <(jq -c '.[]' <<<"$source_repos_json") | jq -s '.'
    )"
    while IFS= read -r repo_item; do
      write_repo_reference_file "$repo_path" "$repo_item"
    done < <(jq -c '.[]' <<<"$enriched_source_repos_json")
    repo_ids_json="$(jq -r '.[].repo_id' <<<"$enriched_source_repos_json")"
    mapfile -t repo_ids < <(printf '%s\n' "$repo_ids_json" | sed '/^$/d')
    if (( ${#repo_ids[@]} > 0 )); then
      replace_frontmatter_list "${repo_path}/campaign.md" "source_repos" "${repo_ids[@]}"
    fi
  fi

  contract_json="$(bootstrap_contract_json "$bootstrap_json")"
  write_research_contract_file "$repo_path" "$contract_json" "${enriched_source_repos_json:-[]}"

  run_campaigns repo-reconcile "$campaign_id"
}
