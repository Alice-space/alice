#!/usr/bin/env bash
set -euo pipefail

runtime_bin="${ALICE_RUNTIME_BIN:-}"
alice_home="${ALICE_HOME:-$HOME/.alice}"
home_bin="$alice_home/bin/alice"

if [[ "${1:-}" == "code-army-status" ]]; then
  shift
  if [[ -n "$runtime_bin" ]]; then
    exec "$runtime_bin" runtime workflow code-army-status "$@"
  fi
  if [[ -x "$home_bin" ]]; then
    exec "$home_bin" runtime workflow code-army-status "$@"
  fi
  exec alice runtime workflow code-army-status "$@"
fi

if [[ -n "$runtime_bin" ]]; then
  exec "$runtime_bin" runtime automation "$@"
fi

if [[ -x "$home_bin" ]]; then
  exec "$home_bin" runtime automation "$@"
fi

exec alice runtime automation "$@"
