#!/usr/bin/env bash
set -euo pipefail

echo "[pre-commit] running make fmt"
make fmt

echo "[pre-commit] running make check"
make check
