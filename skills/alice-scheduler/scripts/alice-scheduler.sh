#!/usr/bin/env bash
set -euo pipefail

script_path="$(readlink -f "${BASH_SOURCE[0]}")"
repo_root="$(cd "$(dirname "$script_path")/../../.." && pwd -P)"
runtime_bin="${ALICE_RUNTIME_BIN:-}"
repo_bin="$repo_root/bin/alice-connector"

if [[ "${1:-}" == "code-army-status" ]]; then
  shift
  if [[ -n "$runtime_bin" ]]; then
    exec "$runtime_bin" runtime workflow code-army-status "$@"
  fi
  if [[ -x "$repo_bin" ]]; then
    exec "$repo_bin" runtime workflow code-army-status "$@"
  fi
  exec alice-connector runtime workflow code-army-status "$@"
fi

if [[ -n "$runtime_bin" ]]; then
  exec "$runtime_bin" runtime automation "$@"
fi

if [[ -x "$repo_bin" ]]; then
  exec "$repo_bin" runtime automation "$@"
fi

exec alice-connector runtime automation "$@"
