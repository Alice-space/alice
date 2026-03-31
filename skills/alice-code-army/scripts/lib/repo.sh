# shellcheck shell=bash

materialize_campaign_repo_template() {
  local payload="$1" dest="$2" template_root
  local campaign_id campaign_title campaign_objective escaped_id escaped_title escaped_objective escaped_repo_path existing_campaign_id

  template_root="$(campaign_repo_template_root)"
  [[ -d "$template_root" ]] || die "missing embedded campaign repo template at ${template_root}"

  campaign_id="$(jq -r '.campaign.id // ""' <<<"$payload")"
  campaign_title="$(jq -r '.campaign.title // ""' <<<"$payload")"
  campaign_objective="$(jq -r '.campaign.objective // ""' <<<"$payload")"

  if [[ -d "$dest" ]]; then
    if [[ -f "$dest/campaign.md" ]]; then
      existing_campaign_id="$(sed -n 's/^campaign_id:[[:space:]]*//p' "$dest/campaign.md" | head -1 | tr -d '"' | tr -d "'")"
      if [[ -n "$existing_campaign_id" && "$existing_campaign_id" != "$campaign_id" ]]; then
        die "campaign repo ${dest} already belongs to ${existing_campaign_id}; refusing to attach new campaign ${campaign_id}"
      fi
      return 0
    fi
    if find "$dest" -mindepth 1 -maxdepth 1 -print -quit | grep -q .; then
      die "campaign repo dir already exists and is not empty: ${dest}"
    fi
  fi

  mkdir -p "$dest"
  cp -R "$template_root"/. "$dest"/

  escaped_id="$(escape_sed_replacement "$campaign_id")"
  escaped_title="$(escape_sed_replacement "$campaign_title")"
  escaped_objective="$(escape_sed_replacement "$campaign_objective")"
  escaped_repo_path="$(escape_sed_replacement "$dest")"

  while IFS= read -r -d '' file; do
    sed -i \
      -e "s/__CAMPAIGN_ID__/${escaped_id}/g" \
      -e "s/__CAMPAIGN_TITLE__/${escaped_title}/g" \
      -e "s/__CAMPAIGN_OBJECTIVE__/${escaped_objective}/g" \
      -e "s#__CAMPAIGN_REPO_PATH__#${escaped_repo_path}#g" \
      "$file"
  done < <(find "$dest" -type f -name '*.md' -print0)

  ensure_campaign_repo_writable "$dest"
}

ensure_campaign_repo_writable() {
  local dest="$1"
  [[ -d "$dest" ]] || return 0
  chmod -R u+w "$dest"
}

ensure_campaign_repo_git_init() {
  local dest="$1"
  require_cmd git
  if ! git -C "$dest" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    git -C "$dest" init -q
  fi
}

init_campaign_repo() {
  local campaign_id="$1" dest_dir="${2:-}" payload current_path wanted_path patch_json
  payload="$(campaign_json "$campaign_id")"
  current_path="$(campaign_repo_path "$payload")"
  wanted_path="${dest_dir:-$current_path}"
  if [[ -z "$wanted_path" ]]; then
    wanted_path="$(default_campaign_repo_path "$campaign_id" "$payload")"
  fi
  mkdir -p "$(dirname "$wanted_path")"
  wanted_path="$(cd "$(dirname "$wanted_path")" && pwd)/$(basename "$wanted_path")"

  if [[ "$current_path" != "$wanted_path" ]]; then
    patch_json="$(jq -cn --arg path "$wanted_path" '{campaign_repo_path:$path}')"
    patch_campaign "$campaign_id" "$patch_json"
    payload="$(campaign_json "$campaign_id")"
  fi

  materialize_campaign_repo_template "$payload" "$wanted_path"
  ensure_campaign_repo_git_init "$wanted_path"
  commit_campaign_repo_if_dirty "$wanted_path" "chore(campaign): initialize repo"
  campaign_json "$campaign_id"
}
