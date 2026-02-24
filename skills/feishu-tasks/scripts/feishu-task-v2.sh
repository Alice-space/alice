#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${FEISHU_BASE_URL:-https://open.feishu.cn/open-apis}"
USER_ID_TYPE="${FEISHU_USER_ID_TYPE:-open_id}"
DEFAULT_PAGE_SIZE="${FEISHU_PAGE_SIZE:-50}"

die() {
  echo "[ERROR] $*" >&2
  exit 1
}

has_cmd() {
  command -v "$1" >/dev/null 2>&1
}

need_cmd() {
  has_cmd "$1" || die "Missing command: $1"
}

print_json() {
  local raw="$1"
  if has_cmd jq; then
    jq . <<<"$raw"
  else
    printf '%s\n' "$raw"
  fi
}

user_token() {
  if [[ -n "${FEISHU_USER_ACCESS_TOKEN:-}" ]]; then
    printf '%s\n' "${FEISHU_USER_ACCESS_TOKEN}"
    return
  fi
  die "Set FEISHU_USER_ACCESS_TOKEN for user-scope Task v2 operations."
}

mixed_token() {
  if [[ -n "${FEISHU_USER_ACCESS_TOKEN:-}" ]]; then
    printf '%s\n' "${FEISHU_USER_ACCESS_TOKEN}"
    return
  fi
  if [[ -n "${FEISHU_TENANT_ACCESS_TOKEN:-}" ]]; then
    printf '%s\n' "${FEISHU_TENANT_ACCESS_TOKEN}"
    return
  fi
  if [[ -n "${FEISHU_ACCESS_TOKEN:-}" ]]; then
    printf '%s\n' "${FEISHU_ACCESS_TOKEN}"
    return
  fi
  die "Set FEISHU_USER_ACCESS_TOKEN or FEISHU_TENANT_ACCESS_TOKEN."
}

api_call() {
  local method="$1"
  local path="$2"
  local token="$3"
  local query="${4:-}"
  local data="${5:-}"

  local url="${BASE_URL}${path}"
  if [[ -n "$query" ]]; then
    url="${url}?${query}"
  fi

  if [[ -n "$data" ]]; then
    curl -sS -X "$method" "$url" \
      -H "Authorization: Bearer ${token}" \
      -H "Content-Type: application/json; charset=utf-8" \
      -d "$data"
  else
    curl -sS -X "$method" "$url" \
      -H "Authorization: Bearer ${token}" \
      -H "Content-Type: application/json; charset=utf-8"
  fi
}

usage() {
  cat <<'EOF'
Feishu Task v2 helper (official OpenAPI)

Required env:
  FEISHU_USER_ACCESS_TOKEN      for user-scoped operations (required by list-my-tasks)
  FEISHU_TENANT_ACCESS_TOKEN    optional fallback for create/update/delete
Optional env:
  FEISHU_ACCESS_TOKEN           fallback token when tenant token not set
  FEISHU_USER_ID_TYPE           open_id | user_id | union_id (default: open_id)
  FEISHU_PAGE_SIZE              default page size (default: 50)
  FEISHU_CURRENT_USER_ID        used by list-managed-tasklists to local-filter ownership/editor role
  FEISHU_APP_ID + FEISHU_APP_SECRET for get-tenant-token

Commands:
  help
  get-tenant-token
  list-my-tasks [completed:true|false] [page_size] [page_token] [type]
  list-tasklists [page_size] [page_token]
  list-managed-tasklists [page_size] [page_token]
  list-tasklist-tasks <tasklist_guid> [completed:true|false] [page_size] [page_token]
  create-task <summary> [description] [tasklist_guid]
  get-task <task_guid>
  update-task-summary <task_guid> <summary>
  delete-task <task_guid>
  assign-task <task_guid> <member_open_id> [role:assignee|follower]
  remove-task-member <task_guid> <member_open_id> [role:assignee|follower]
  set-deadline <task_guid> <timestamp_ms> [is_all_day:true|false]
  add-task-to-tasklist <task_guid> <tasklist_guid> [section_guid]
  remove-task-from-tasklist <task_guid> <tasklist_guid>
  create-tasklist <name>
  update-tasklist-name <tasklist_guid> <name>
  delete-tasklist <tasklist_guid>
  add-tasklist-member <tasklist_guid> <member_open_id> [role]
  remove-tasklist-member <tasklist_guid> <member_open_id> [role]
EOF
}

client_token() {
  if [[ -n "${FEISHU_CLIENT_TOKEN:-}" ]]; then
    printf '%s\n' "${FEISHU_CLIENT_TOKEN}"
  else
    date +%s%N
  fi
}

cmd="${1:-help}"
shift || true

case "$cmd" in
  help|-h|--help)
    usage
    ;;

  get-tenant-token)
    [[ -n "${FEISHU_APP_ID:-}" ]] || die "Set FEISHU_APP_ID."
    [[ -n "${FEISHU_APP_SECRET:-}" ]] || die "Set FEISHU_APP_SECRET."
    need_cmd jq
    payload="$(jq -nc \
      --arg app_id "${FEISHU_APP_ID}" \
      --arg app_secret "${FEISHU_APP_SECRET}" \
      '{app_id:$app_id, app_secret:$app_secret}')"
    resp="$(curl -sS -X POST "https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal" \
      -H "Content-Type: application/json; charset=utf-8" \
      -d "$payload")"
    print_json "$resp"
    ;;

  list-my-tasks)
    completed="${1:-}"
    page_size="${2:-$DEFAULT_PAGE_SIZE}"
    page_token="${3:-}"
    task_type="${4:-my_tasks}"
    query="user_id_type=${USER_ID_TYPE}&type=${task_type}&page_size=${page_size}"
    [[ -n "$completed" ]] && query="${query}&completed=${completed}"
    [[ -n "$page_token" ]] && query="${query}&page_token=${page_token}"
    resp="$(api_call GET "/task/v2/tasks" "$(user_token)" "$query")"
    print_json "$resp"
    ;;

  list-tasklists)
    page_size="${1:-$DEFAULT_PAGE_SIZE}"
    page_token="${2:-}"
    query="user_id_type=${USER_ID_TYPE}&page_size=${page_size}"
    [[ -n "$page_token" ]] && query="${query}&page_token=${page_token}"
    resp="$(api_call GET "/task/v2/tasklists" "$(mixed_token)" "$query")"
    print_json "$resp"
    ;;

  list-managed-tasklists)
    page_size="${1:-$DEFAULT_PAGE_SIZE}"
    page_token="${2:-}"
    query="user_id_type=${USER_ID_TYPE}&page_size=${page_size}"
    [[ -n "$page_token" ]] && query="${query}&page_token=${page_token}"
    resp="$(api_call GET "/task/v2/tasklists" "$(user_token)" "$query")"
    if [[ -n "${FEISHU_CURRENT_USER_ID:-}" ]] && has_cmd jq; then
      resp="$(jq --arg me "${FEISHU_CURRENT_USER_ID}" '
        if (.data.items | type) == "array" then
          .data.items = [
            .data.items[]
            | select(
                (.owner.id? == $me) or
                ((.members // []) | any((.id? == $me) and ((.role? == "editor") or (.role? == "owner"))))
              )
          ]
        else
          .
        end
      ' <<<"$resp")"
    fi
    print_json "$resp"
    ;;

  list-tasklist-tasks)
    tasklist_guid="${1:-}"
    [[ -n "$tasklist_guid" ]] || die "Usage: list-tasklist-tasks <tasklist_guid> [completed] [page_size] [page_token]"
    completed="${2:-}"
    page_size="${3:-$DEFAULT_PAGE_SIZE}"
    page_token="${4:-}"
    query="user_id_type=${USER_ID_TYPE}&page_size=${page_size}"
    [[ -n "$completed" ]] && query="${query}&completed=${completed}"
    [[ -n "$page_token" ]] && query="${query}&page_token=${page_token}"
    resp="$(api_call GET "/task/v2/tasklists/${tasklist_guid}/tasks" "$(mixed_token)" "$query")"
    print_json "$resp"
    ;;

  create-task)
    need_cmd jq
    summary="${1:-}"
    [[ -n "$summary" ]] || die "Usage: create-task <summary> [description] [tasklist_guid]"
    description="${2:-}"
    tasklist_guid="${3:-}"
    token="$(mixed_token)"
    ct="$(client_token)"
    body="$(jq -nc \
      --arg summary "$summary" \
      --arg description "$description" \
      --arg tasklist_guid "$tasklist_guid" \
      --arg client_token "$ct" '
      {
        summary: $summary,
        client_token: $client_token
      }
      + (if ($description | length) > 0 then {description: $description} else {} end)
      + (if ($tasklist_guid | length) > 0 then {tasklists: [{tasklist_guid: $tasklist_guid}]} else {} end)
    ')"
    resp="$(api_call POST "/task/v2/tasks" "$token" "user_id_type=${USER_ID_TYPE}" "$body")"
    print_json "$resp"
    ;;

  get-task)
    task_guid="${1:-}"
    [[ -n "$task_guid" ]] || die "Usage: get-task <task_guid>"
    resp="$(api_call GET "/task/v2/tasks/${task_guid}" "$(mixed_token)" "user_id_type=${USER_ID_TYPE}")"
    print_json "$resp"
    ;;

  update-task-summary)
    need_cmd jq
    task_guid="${1:-}"
    summary="${2:-}"
    [[ -n "$task_guid" && -n "$summary" ]] || die "Usage: update-task-summary <task_guid> <summary>"
    body="$(jq -nc --arg summary "$summary" '
      {
        task: {summary: $summary},
        update_fields: ["summary"]
      }
    ')"
    resp="$(api_call PATCH "/task/v2/tasks/${task_guid}" "$(mixed_token)" "user_id_type=${USER_ID_TYPE}" "$body")"
    print_json "$resp"
    ;;

  delete-task)
    task_guid="${1:-}"
    [[ -n "$task_guid" ]] || die "Usage: delete-task <task_guid>"
    resp="$(api_call DELETE "/task/v2/tasks/${task_guid}" "$(mixed_token)" "")"
    print_json "$resp"
    ;;

  assign-task)
    need_cmd jq
    task_guid="${1:-}"
    member_id="${2:-}"
    role="${3:-assignee}"
    [[ -n "$task_guid" && -n "$member_id" ]] || die "Usage: assign-task <task_guid> <member_open_id> [role:assignee|follower]"
    body="$(jq -nc --arg id "$member_id" --arg role "$role" --arg ct "$(client_token)" '
      {
        members: [{id: $id, type: "user", role: $role}],
        client_token: $ct
      }
    ')"
    resp="$(api_call POST "/task/v2/tasks/${task_guid}/add_members" "$(mixed_token)" "user_id_type=${USER_ID_TYPE}" "$body")"
    print_json "$resp"
    ;;

  remove-task-member)
    need_cmd jq
    task_guid="${1:-}"
    member_id="${2:-}"
    role="${3:-assignee}"
    [[ -n "$task_guid" && -n "$member_id" ]] || die "Usage: remove-task-member <task_guid> <member_open_id> [role:assignee|follower]"
    body="$(jq -nc --arg id "$member_id" --arg role "$role" '
      {
        members: [{id: $id, type: "user", role: $role}]
      }
    ')"
    resp="$(api_call POST "/task/v2/tasks/${task_guid}/remove_members" "$(mixed_token)" "user_id_type=${USER_ID_TYPE}" "$body")"
    print_json "$resp"
    ;;

  set-deadline)
    need_cmd jq
    task_guid="${1:-}"
    timestamp_ms="${2:-}"
    is_all_day="${3:-false}"
    [[ -n "$task_guid" && -n "$timestamp_ms" ]] || die "Usage: set-deadline <task_guid> <timestamp_ms> [is_all_day:true|false]"
    [[ "$is_all_day" == "true" || "$is_all_day" == "false" ]] || die "is_all_day must be true or false."
    body="$(jq -nc --arg ts "$timestamp_ms" --argjson all_day "$is_all_day" '
      {
        task: {due: {timestamp: $ts, is_all_day: $all_day}},
        update_fields: ["due"]
      }
    ')"
    resp="$(api_call PATCH "/task/v2/tasks/${task_guid}" "$(mixed_token)" "user_id_type=${USER_ID_TYPE}" "$body")"
    print_json "$resp"
    ;;

  add-task-to-tasklist)
    need_cmd jq
    task_guid="${1:-}"
    tasklist_guid="${2:-}"
    section_guid="${3:-}"
    [[ -n "$task_guid" && -n "$tasklist_guid" ]] || die "Usage: add-task-to-tasklist <task_guid> <tasklist_guid> [section_guid]"
    body="$(jq -nc --arg tasklist_guid "$tasklist_guid" --arg section_guid "$section_guid" '
      {tasklist_guid: $tasklist_guid}
      + (if ($section_guid | length) > 0 then {section_guid: $section_guid} else {} end)
    ')"
    resp="$(api_call POST "/task/v2/tasks/${task_guid}/add_tasklist" "$(mixed_token)" "user_id_type=${USER_ID_TYPE}" "$body")"
    print_json "$resp"
    ;;

  remove-task-from-tasklist)
    need_cmd jq
    task_guid="${1:-}"
    tasklist_guid="${2:-}"
    [[ -n "$task_guid" && -n "$tasklist_guid" ]] || die "Usage: remove-task-from-tasklist <task_guid> <tasklist_guid>"
    body="$(jq -nc --arg tasklist_guid "$tasklist_guid" '{tasklist_guid: $tasklist_guid}')"
    resp="$(api_call POST "/task/v2/tasks/${task_guid}/remove_tasklist" "$(mixed_token)" "user_id_type=${USER_ID_TYPE}" "$body")"
    print_json "$resp"
    ;;

  create-tasklist)
    need_cmd jq
    name="${1:-}"
    [[ -n "$name" ]] || die "Usage: create-tasklist <name>"
    body="$(jq -nc --arg name "$name" --arg ct "$(client_token)" '{name: $name, client_token: $ct}')"
    resp="$(api_call POST "/task/v2/tasklists" "$(mixed_token)" "user_id_type=${USER_ID_TYPE}" "$body")"
    print_json "$resp"
    ;;

  update-tasklist-name)
    need_cmd jq
    tasklist_guid="${1:-}"
    name="${2:-}"
    [[ -n "$tasklist_guid" && -n "$name" ]] || die "Usage: update-tasklist-name <tasklist_guid> <name>"
    body="$(jq -nc --arg name "$name" '
      {
        tasklist: {name: $name},
        update_fields: ["name"]
      }
    ')"
    resp="$(api_call PATCH "/task/v2/tasklists/${tasklist_guid}" "$(mixed_token)" "user_id_type=${USER_ID_TYPE}" "$body")"
    print_json "$resp"
    ;;

  delete-tasklist)
    tasklist_guid="${1:-}"
    [[ -n "$tasklist_guid" ]] || die "Usage: delete-tasklist <tasklist_guid>"
    resp="$(api_call DELETE "/task/v2/tasklists/${tasklist_guid}" "$(mixed_token)" "")"
    print_json "$resp"
    ;;

  add-tasklist-member)
    need_cmd jq
    tasklist_guid="${1:-}"
    member_id="${2:-}"
    role="${3:-editor}"
    [[ -n "$tasklist_guid" && -n "$member_id" ]] || die "Usage: add-tasklist-member <tasklist_guid> <member_open_id> [role]"
    body="$(jq -nc --arg id "$member_id" --arg role "$role" '
      {
        members: [{id: $id, type: "user", role: $role}]
      }
    ')"
    resp="$(api_call POST "/task/v2/tasklists/${tasklist_guid}/add_members" "$(mixed_token)" "user_id_type=${USER_ID_TYPE}" "$body")"
    print_json "$resp"
    ;;

  remove-tasklist-member)
    need_cmd jq
    tasklist_guid="${1:-}"
    member_id="${2:-}"
    role="${3:-editor}"
    [[ -n "$tasklist_guid" && -n "$member_id" ]] || die "Usage: remove-tasklist-member <tasklist_guid> <member_open_id> [role]"
    body="$(jq -nc --arg id "$member_id" --arg role "$role" '
      {
        members: [{id: $id, type: "user", role: $role}]
      }
    ')"
    resp="$(api_call POST "/task/v2/tasklists/${tasklist_guid}/remove_members" "$(mixed_token)" "user_id_type=${USER_ID_TYPE}" "$body")"
    print_json "$resp"
    ;;

  *)
    die "Unknown command: $cmd (run: $0 help)"
    ;;
esac
