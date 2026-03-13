#!/usr/bin/env bash
set -euo pipefail

base_url="${ALICE_RUNTIME_API_BASE_URL:?missing ALICE_RUNTIME_API_BASE_URL}"
token="${ALICE_RUNTIME_API_TOKEN:?missing ALICE_RUNTIME_API_TOKEN}"

build_headers() {
  printf '%s\n' \
    "-H" "Authorization: Bearer ${token}" \
    "-H" "Accept: application/json" \
    "-H" "Content-Type: application/json" \
    "-H" "X-Alice-Receive-Id-Type: ${ALICE_MCP_RECEIVE_ID_TYPE:-}" \
    "-H" "X-Alice-Receive-Id: ${ALICE_MCP_RECEIVE_ID:-}" \
    "-H" "X-Alice-Resource-Root: ${ALICE_MCP_RESOURCE_ROOT:-}" \
    "-H" "X-Alice-Source-Message-Id: ${ALICE_MCP_SOURCE_MESSAGE_ID:-}" \
    "-H" "X-Alice-Actor-User-Id: ${ALICE_MCP_ACTOR_USER_ID:-}" \
    "-H" "X-Alice-Actor-Open-Id: ${ALICE_MCP_ACTOR_OPEN_ID:-}" \
    "-H" "X-Alice-Chat-Type: ${ALICE_MCP_CHAT_TYPE:-}" \
    "-H" "X-Alice-Session-Key: ${ALICE_MCP_SESSION_KEY:-}"
}

read_payload() {
  if [[ $# -gt 0 ]]; then
    printf '%s' "$1"
  else
    cat
  fi
}

curl_json() {
  local method="$1"
  local path="$2"
  local content_type="${3:-application/json}"
  local body="${4:-}"
  local -a args
  mapfile -t args < <(build_headers)
  args+=(-H "Content-Type: ${content_type}")
  if [[ -n "${body}" ]]; then
    curl -fsS -X "${method}" "${args[@]}" --data "${body}" "${base_url}${path}"
  else
    curl -fsS -X "${method}" "${args[@]}" "${base_url}${path}"
  fi
}

cmd="${1:-}"
shift || true

case "${cmd}" in
  list)
    status="${1:-}"
    limit="${2:-20}"
    query="?limit=${limit}"
    if [[ -n "${status}" ]]; then
      query="${query}&status=${status}"
    fi
    curl_json GET "/api/v1/automation/tasks${query}"
    ;;
  create)
    payload="$(read_payload "${1:-}")"
    curl_json POST /api/v1/automation/tasks application/json "${payload}"
    ;;
  get)
    task_id="${1:?task id is required}"
    curl_json GET "/api/v1/automation/tasks/${task_id}"
    ;;
  patch)
    task_id="${1:?task id is required}"
    payload="${2:-}"
    content_type="${3:-application/merge-patch+json}"
    if [[ -z "${payload}" ]]; then
      payload="$(read_payload)"
    fi
    curl_json PATCH "/api/v1/automation/tasks/${task_id}" "${content_type}" "${payload}"
    ;;
  delete)
    task_id="${1:?task id is required}"
    curl_json DELETE "/api/v1/automation/tasks/${task_id}"
    ;;
  code-army-status)
    state_key="${1:-}"
    if [[ -n "${state_key}" ]]; then
      curl_json GET "/api/v1/workflows/code-army/status?state_key=${state_key}"
    else
      curl_json GET /api/v1/workflows/code-army/status
    fi
    ;;
  *)
    echo "usage: $0 {list|create|get|patch|delete|code-army-status} ..." >&2
    exit 1
    ;;
esac
