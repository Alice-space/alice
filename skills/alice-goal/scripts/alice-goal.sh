#!/usr/bin/env bash
set -euo pipefail
runtime_bin="${ALICE_RUNTIME_BIN:-}"
alice_home="${ALICE_HOME:-$HOME/.alice}"
home_bin="$alice_home/bin/alice"
if [[ -n "$runtime_bin" ]]; then
  exec "$runtime_bin" runtime goal "$@"
fi
if [[ -x "$home_bin" ]]; then
  exec "$home_bin" runtime goal "$@"
fi
exec alice runtime goal "$@"
