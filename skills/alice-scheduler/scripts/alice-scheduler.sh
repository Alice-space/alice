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
  session_key="${ALICE_SESSION_KEY:-}"
  resume_thread_id="${ALICE_RESUME_THREAD_ID:-}"
  jq -n \
    --arg session_key "$session_key" \
    --arg resume_thread_id "$resume_thread_id" \
    '{"session_key": $session_key, "resume_thread_id": $resume_thread_id}'
  exit 0
fi

# For the "create" subcommand, auto-inject resume_session_key and
# action.resume_thread_id from env vars if they are absent in the provided JSON.
# This prevents the LLM from having to call current-session and manually copy
# these values into the create payload — the script handles it transparently.
#
# Injection rules (both use null-guard so explicit values in JSON take precedence):
#   resume_session_key        ← $ALICE_SESSION_KEY       (if field is null/missing)
#   action.resume_thread_id   ← $ALICE_RESUME_THREAD_ID  (if field is null/empty/"")
if [[ "${1:-}" == "create" ]]; then
  shift  # remove "create" from args; remaining args may contain the JSON body

  session_key="${ALICE_SESSION_KEY:-}"
  resume_thread_id="${ALICE_RESUME_THREAD_ID:-}"

  # Read the raw JSON from the positional arg or stdin
  if [[ $# -gt 0 ]]; then
    raw_json="$1"
    shift
  else
    stat_result=$(stat -c "%F" /proc/self/fd/0 2>/dev/null || echo "")
    if [[ "$stat_result" == "character special file" ]]; then
      echo "alice-scheduler create: missing JSON body (no argument and stdin is a terminal)" >&2
      exit 1
    fi
    raw_json=$(cat)
  fi

  # Inject missing fields using jq:
  #   - resume_session_key is set only when absent/null and ALICE_SESSION_KEY is non-empty
  #   - action.resume_thread_id is set only when absent/null/"" and ALICE_RESUME_THREAD_ID is non-empty
  enriched_json=$(
    jq \
      --arg sk "$session_key" \
      --arg rtid "$resume_thread_id" \
      '
      if ($sk != "" and (.resume_session_key == null or .resume_session_key == ""))
        then .resume_session_key = $sk
        else .
      end |
      if ($rtid != "" and (.action.resume_thread_id == null or .action.resume_thread_id == ""))
        then .action.resume_thread_id = $rtid
        else .
      end
      ' <<< "$raw_json"
  )

  if [[ -n "$runtime_bin" ]]; then
    exec "$runtime_bin" runtime automation create "$enriched_json" "$@"
  fi
  if [[ -x "$home_bin" ]]; then
    exec "$home_bin" runtime automation create "$enriched_json" "$@"
  fi
  exec alice runtime automation create "$enriched_json" "$@"
fi

if [[ -n "$runtime_bin" ]]; then
  exec "$runtime_bin" runtime automation "$@"
fi

if [[ -x "$home_bin" ]]; then
  exec "$home_bin" runtime automation "$@"
fi

exec alice runtime automation "$@"
