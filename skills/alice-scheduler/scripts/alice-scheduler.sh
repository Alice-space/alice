#!/usr/bin/env bash
set -euo pipefail

runtime_bin="${ALICE_RUNTIME_BIN:-}"
alice_home="${ALICE_HOME:-$HOME/.alice}"
home_bin="$alice_home/bin/alice"

# current-session: return the current session's key and resume thread ID from
# the MCP environment variables injected by the Alice connector.  No binary
# call is needed — the values are always present in the env when Alice runs a
# skill as a tool call.
#
# jq is used for JSON encoding to correctly escape any special characters
# (quotes, backslashes, newlines) that may appear in the env values.
if [[ "${1:-}" == "current-session" ]]; then
  session_key="${ALICE_MCP_SESSION_KEY:-}"
  resume_thread_id="${ALICE_MCP_RESUME_THREAD_ID:-}"
  jq -n \
    --arg session_key "$session_key" \
    --arg resume_thread_id "$resume_thread_id" \
    '{"session_key": $session_key, "resume_thread_id": $resume_thread_id}'
  exit 0
fi

if [[ -n "$runtime_bin" ]]; then
  exec "$runtime_bin" runtime automation "$@"
fi

if [[ -x "$home_bin" ]]; then
  exec "$home_bin" runtime automation "$@"
fi

exec alice runtime automation "$@"
