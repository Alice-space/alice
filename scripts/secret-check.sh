#!/usr/bin/env bash
set -euo pipefail

ROOT="$(git rev-parse --show-toplevel)"
cd "$ROOT"

if ! command -v rg >/dev/null 2>&1; then
  echo "[secret-check] ripgrep (rg) is required." >&2
  echo "[secret-check] install rg and retry." >&2
  exit 2
fi

if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  echo "[secret-check] must run inside a git repository." >&2
  exit 2
fi

matches=""

collect_matches() {
  local m="$1"
  if [[ -n "$m" ]]; then
    if [[ -n "$matches" ]]; then
      matches+=$'\n'
    fi
    matches+="$m"
  fi
}

# 1) Well-known token/key formats
collect_matches "$(git ls-files -z | xargs -0 -r rg -n --no-heading -I -S \
  -e 'AKIA[0-9A-Z]{16}' \
  -e 'sk-[A-Za-z0-9]{20,}' \
  -e 'xox[baprs]-[A-Za-z0-9-]{20,}' \
  -e 'gh[pousr]_[A-Za-z0-9]{30,}' \
  -e '-----BEGIN [A-Z ]*PRIVATE KEY-----' || true)"

# 2) Generic assignment patterns for common secret-like keys (value length guarded)
collect_matches "$(git ls-files -z | xargs -0 -r rg -n --no-heading -I -S -i \
  '(app_secret|client_secret|access_token|tenant_access_token|refresh_token|api[_-]?key|secret[_-]?key|signing_secret|encrypt_key|password)[[:space:]]*[:=][[:space:]]*["'"'"']?[A-Za-z0-9._~+/-=]{12,}["'"'"']?' || true)"

if [[ -n "$matches" ]]; then
  echo "[secret-check] potential secrets found in tracked files:"
  echo "$matches"
  cat <<'EOF'

[secret-check] blocked commit.
Use placeholders in examples (short dummy values), keep real secrets in untracked files like config.yaml/.env.
If this is a false positive, adjust the value format or update the scan rule with explicit rationale.
EOF
  exit 1
fi

echo "[secret-check] no secret patterns found."
