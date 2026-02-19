#!/usr/bin/env bash
set -euo pipefail

branch_name="$(git rev-parse --abbrev-ref HEAD)"

if [[ "${branch_name}" == "master" || "${branch_name}" == "main" ]]; then
  echo "[ERROR] Current branch is ${branch_name}. Create and use a codex/* branch first."
  exit 1
fi

if [[ "${branch_name}" != codex/* ]]; then
  echo "[ERROR] Branch must start with codex/. Current branch: ${branch_name}"
  exit 1
fi

echo "[INFO] Branch check passed: ${branch_name}"
echo "[INFO] Running go test ./..."
go test ./...

echo "[INFO] Running make check..."
make check

echo "[OK] All required checks passed. Ready to commit."
