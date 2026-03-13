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

json_body() {
  python3 - "$@" <<'PY'
import json
import sys

payload = {}
for item in sys.argv[1:]:
    key, value = item.split("=", 1)
    payload[key] = value
print(json.dumps(payload, ensure_ascii=False))
PY
}

curl_json() {
  local method="$1"
  local path="$2"
  local body="${3:-}"
  local -a args
  mapfile -t args < <(build_headers)
  if [[ -n "${body}" ]]; then
    curl -fsS -X "${method}" "${args[@]}" --data "${body}" "${base_url}${path}"
  else
    curl -fsS -X "${method}" "${args[@]}" "${base_url}${path}"
  fi
}

cmd="${1:-}"
shift || true

case "${cmd}" in
  context)
    curl_json GET /api/v1/memory/context
    ;;
  write-session)
    content="${1:-}"
    curl_json PUT /api/v1/memory/long-term "$(json_body scope_type=session content="${content}")"
    ;;
  write-global)
    content="${1:-}"
    curl_json PUT /api/v1/memory/long-term "$(json_body scope_type=global content="${content}")"
    ;;
  daily-summary)
    summary="${1:-}"
    curl_json POST /api/v1/memory/daily-summary "$(json_body summary="${summary}")"
    ;;
  *)
    echo "usage: $0 {context|write-session|write-global|daily-summary} [content]" >&2
    exit 1
    ;;
esac
