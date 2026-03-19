#!/usr/bin/env bash
set -euo pipefail

BRANCH="cdn"
MODE=""
VERSION=""
SOURCE_DIR=""
REPO_SLUG="${GITHUB_REPOSITORY:-}"

usage() {
  cat <<'USAGE'
Usage:
  publish-cdn-assets.sh --mode dev --source-dir release-assets [--branch cdn]
  publish-cdn-assets.sh --mode release --version vX.Y.Z --source-dir release-assets [--branch cdn]
USAGE
}

die() {
  printf '[publish-cdn-assets] ERROR: %s\n' "$*" >&2
  exit 1
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --branch)
        [[ $# -ge 2 ]] || die "--branch requires a value"
        BRANCH="$2"
        shift 2
        ;;
      --mode)
        [[ $# -ge 2 ]] || die "--mode requires a value"
        MODE="$2"
        shift 2
        ;;
      --version)
        [[ $# -ge 2 ]] || die "--version requires a value"
        VERSION="$2"
        shift 2
        ;;
      --source-dir)
        [[ $# -ge 2 ]] || die "--source-dir requires a value"
        SOURCE_DIR="$2"
        shift 2
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        die "unknown argument: $1"
        ;;
    esac
  done
}

ensure_git_identity() {
  git config user.name "github-actions[bot]"
  git config user.email "41898282+github-actions[bot]@users.noreply.github.com"
}

prepare_worktree() {
  local worktree_dir
  worktree_dir="$(mktemp -d)"
  if git ls-remote --exit-code --heads origin "$BRANCH" >/dev/null 2>&1; then
    git fetch origin "$BRANCH":"refs/remotes/origin/$BRANCH"
    git worktree add "$worktree_dir" "refs/remotes/origin/$BRANCH" >/dev/null
    (
      cd "$worktree_dir"
      git checkout -B "$BRANCH" >/dev/null
    )
  else
    git worktree add --detach "$worktree_dir" >/dev/null
    (
      cd "$worktree_dir"
      git checkout --orphan "$BRANCH" >/dev/null
      find . -mindepth 1 -maxdepth 1 ! -name .git -exec rm -rf {} +
    )
  fi
  printf '%s' "$worktree_dir"
}

sync_assets() {
  local target_root="$1"
  mkdir -p "$target_root"
  find "$target_root" -mindepth 1 -maxdepth 1 -exec rm -rf {} +
  cp -f "$SOURCE_DIR"/* "$target_root"/
}

main() {
  parse_args "$@"

  [[ -n "$MODE" ]] || die "--mode is required"
  [[ -n "$SOURCE_DIR" ]] || die "--source-dir is required"
  [[ -d "$SOURCE_DIR" ]] || die "source dir does not exist: $SOURCE_DIR"

  case "$MODE" in
    dev) ;;
    release)
      [[ -n "$VERSION" ]] || die "--version is required when --mode=release"
      ;;
    *)
      die "unsupported mode: $MODE"
      ;;
  esac

  ensure_git_identity

  local worktree_dir target_root
  worktree_dir="$(prepare_worktree)"
  trap 'git worktree remove --force "$worktree_dir" >/dev/null 2>&1 || true' EXIT

  case "$MODE" in
    dev)
      target_root="$worktree_dir/dev"
      ;;
    release)
      target_root="$worktree_dir/releases/$VERSION"
      ;;
  esac

  sync_assets "$target_root"

  (
    cd "$worktree_dir"
    git add .
    if git diff --cached --quiet; then
      printf '[publish-cdn-assets] no changes for %s\n' "$MODE"
      exit 0
    fi
    if [[ "$MODE" == "dev" ]]; then
      git commit -m "chore(cdn): update dev assets" >/dev/null
    else
      git commit -m "chore(cdn): publish $VERSION assets" >/dev/null
    fi
    git push origin "$BRANCH" >/dev/null
  )
}

main "$@"
