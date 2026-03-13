#!/usr/bin/env bash
set -euo pipefail

script_path="$(readlink -f "${BASH_SOURCE[0]}")"
repo_root="$(cd "$(dirname "$script_path")/../../.." && pwd -P)"
runtime_bin="${ALICE_RUNTIME_BIN:-}"
repo_bin="$repo_root/bin/alice-connector"

if [[ -n "$runtime_bin" ]]; then
  exec "$runtime_bin" runtime message "$@"
fi

if [[ -x "$repo_bin" ]]; then
  exec "$repo_bin" runtime message "$@"
fi

exec alice-connector runtime message "$@"
