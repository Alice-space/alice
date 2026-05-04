#!/usr/bin/env bash
set -euo pipefail

runtime_bin="${ALICE_RUNTIME_BIN:-}"
alice_home="${ALICE_HOME:-$HOME/.alice}"
home_bin="$alice_home/bin/alice"

if [[ "${1:-}" == "create" ]]; then
  shift

  session_key="${ALICE_SESSION_KEY:-}"
  resume_thread_id="${ALICE_RESUME_THREAD_ID:-}"

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

  enriched_json=$(
    jq \
      --arg rtid "$resume_thread_id" \
      '
      if ($rtid != "" and (.resume_thread_id == null or .resume_thread_id == ""))
        then .resume_thread_id = $rtid
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
