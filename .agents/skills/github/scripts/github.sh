#!/usr/bin/env bash
set -euo pipefail

PROGRAM="$(basename "$0")"
ACTION="${1:-help}"
if [[ $# -gt 0 ]]; then
  shift
fi

REMOTE="origin"
REPO_DIR=""
REPO=""
HEAD_BRANCH="dev"
BASE_BRANCH="main"
TITLE=""
BODY=""
POLL_INTERVAL=15
TIMEOUT=1800
MERGE_METHOD="merge"
CHANNEL="release"
VERSION=""
ALICE_HOME_OVERRIDE=""
SERVICE_NAME=""
AUTH_SOURCE=""

usage() {
  cat <<EOF
Usage:
  $PROGRAM create-release [options]
  $PROGRAM update-self [options]
  $PROGRAM auth-status

create-release options:
  --repo-dir PATH        Local git repo root or any path inside it
  --repo OWNER/REPO      GitHub repository; defaults to remote URL parsing
  --remote NAME          Git remote name (default: origin)
  --head BRANCH          Source branch to promote (default: dev)
  --base BRANCH          Target branch (default: main)
  --title TEXT           Pull request title
  --body TEXT            Pull request body
  --poll-interval SEC    Poll interval while waiting (default: 15)
  --timeout SEC          Max wait time for merge/release (default: 1800)

update-self options:
  --repo-dir PATH        Alice repo root or any path inside it
  --repo OWNER/REPO      Release repo for installer lookup
  --channel NAME         Installer channel: release or dev (default: release)
  --version TAG          Install a pinned version, for example v0.3.5
  --home PATH            Override ALICE_HOME for installer
  --service NAME         Override systemd user service name

auth-status:
  Run gh auth status for the current machine.
EOF
}

log() {
  printf '[github-skill] %s\n' "$*" >&2
}

die() {
  printf '[github-skill] ERROR: %s\n' "$*" >&2
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"
}

uri_encode() {
  jq -nr --arg value "$1" '$value|@uri'
}

parse_repo_from_remote_url() {
  local url="$1" repo=""
  case "$url" in
    git@github.com:*)
      repo="${url#git@github.com:}"
      ;;
    ssh://git@github.com/*)
      repo="${url#ssh://git@github.com/}"
      ;;
    https://github.com/*)
      repo="${url#https://github.com/}"
      ;;
    http://github.com/*)
      repo="${url#http://github.com/}"
      ;;
    *)
      die "unsupported GitHub remote URL: $url"
      ;;
  esac
  repo="${repo%.git}"
  [[ "$repo" == */* ]] || die "failed to parse OWNER/REPO from remote URL: $url"
  printf '%s\n' "$repo"
}

resolve_repo_root() {
  local path="${1:-.}"
  git -C "$path" rev-parse --show-toplevel
}

resolve_repo_name() {
  if [[ -n "$REPO" ]]; then
    printf '%s\n' "$REPO"
    return
  fi

  local root remote_url
  root="$(resolve_repo_root "$REPO_DIR")"
  remote_url="$(git -C "$root" remote get-url "$REMOTE")"
  parse_repo_from_remote_url "$remote_url"
}

resolve_auth() {
  require_cmd gh
  if gh auth status >/dev/null 2>&1; then
    AUTH_SOURCE="gh auth status"
    return
  fi
  die "gh is not authenticated; run gh auth login first"
}

HTTP_STATUS=""
GITHUB_ERROR_TEXT=""
RESPONSE_BODY=""

github_request() {
  local method="$1" path="$2" data="${3:-}" tmp_out tmp_err response body
  tmp_out="$(mktemp)"
  tmp_err="$(mktemp)"
  path="${path#/}"

  local -a gh_args=(api -i --method "$method" "$path")
  if [[ -n "$data" ]]; then
    if ! printf '%s' "$data" | gh "${gh_args[@]}" --input - >"$tmp_out" 2>"$tmp_err"; then
      :
    fi
  else
    if ! gh "${gh_args[@]}" >"$tmp_out" 2>"$tmp_err"; then
      :
    fi
  fi

  response="$(cat "$tmp_out")"
  GITHUB_ERROR_TEXT="$(cat "$tmp_err")"
  rm -f "$tmp_out" "$tmp_err"

  HTTP_STATUS="$(printf '%s\n' "$response" | awk 'NR==1 {print $2}')"
  if [[ -z "$HTTP_STATUS" ]]; then
    if [[ -n "$GITHUB_ERROR_TEXT" ]]; then
      die "gh api failed: ${GITHUB_ERROR_TEXT%%$'\n'*}"
    fi
    die "gh api returned no HTTP status for ${method} ${path}"
  fi

  RESPONSE_BODY="$(printf '%s\n' "$response" | awk 'BEGIN{body=0} /^\r?$/ {body=1; next} body {print}')"
}

github_api() {
  local msg
  github_request "$@"
  if [[ "$HTTP_STATUS" -lt 200 || "$HTTP_STATUS" -ge 300 ]]; then
    msg="$(jq -r '.message // empty' <<<"$RESPONSE_BODY" 2>/dev/null || true)"
    if [[ -z "$msg" && -n "$GITHUB_ERROR_TEXT" ]]; then
      msg="${GITHUB_ERROR_TEXT%%$'\n'*}"
    fi
    die "GitHub API ${1} ${2} failed (${HTTP_STATUS})${msg:+: $msg}"
  fi
}

require_clean_worktree() {
  local root="$1"
  if [[ -n "$(git -C "$root" status --porcelain)" ]]; then
    die "working tree is not clean in ${root}; commit or stash changes before create-release"
  fi
}

require_branch() {
  local root="$1" current
  current="$(git -C "$root" symbolic-ref --quiet --short HEAD || true)"
  [[ -n "$current" ]] || die "repository is in detached HEAD state"
  [[ "$current" == "$HEAD_BRANCH" ]] || die "current branch is ${current}, expected ${HEAD_BRANCH}"
}

latest_release_tag() {
  local repo="$1" body msg
  github_request GET "/repos/${repo}/releases/latest"
  body="$RESPONSE_BODY"
  case "$HTTP_STATUS" in
    200)
      jq -r '.tag_name // empty' <<<"$body"
      ;;
    404)
      printf '\n'
      ;;
    *)
      msg="$(jq -r '.message // empty' <<<"$body" 2>/dev/null || true)"
      die "failed to query latest release (${HTTP_STATUS})${msg:+: $msg}"
      ;;
  esac
}

next_patch_tag() {
  local current="$1"
  if [[ -z "$current" ]]; then
    printf 'v0.1.0\n'
    return
  fi
  if [[ ! "$current" =~ ^v([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
    die "latest tag is not semver: $current"
  fi
  printf 'v%s.%s.%s\n' "${BASH_REMATCH[1]}" "${BASH_REMATCH[2]}" "$((BASH_REMATCH[3] + 1))"
}

resolve_tag_commit_sha() {
  local repo="$1" tag="$2" encoded ref_json obj_type obj_sha tag_json
  encoded="$(uri_encode "$tag")"
  github_api GET "/repos/${repo}/git/ref/tags/${encoded}"
  ref_json="$RESPONSE_BODY"
  obj_type="$(jq -r '.object.type // empty' <<<"$ref_json")"
  obj_sha="$(jq -r '.object.sha // empty' <<<"$ref_json")"
  [[ -n "$obj_type" && -n "$obj_sha" ]] || die "failed to resolve tag object for ${tag}"
  case "$obj_type" in
    commit)
      printf '%s\n' "$obj_sha"
      ;;
    tag)
      github_api GET "/repos/${repo}/git/tags/${obj_sha}"
      tag_json="$RESPONSE_BODY"
      jq -r '.object.sha // empty' <<<"$tag_json"
      ;;
    *)
      die "unsupported tag object type for ${tag}: ${obj_type}"
      ;;
  esac
}

find_recent_release_for_sha() {
  local repo="$1" target_sha="$2" baseline_tag="$3" releases release tag sha
  github_api GET "/repos/${repo}/releases?per_page=5"
  releases="$RESPONSE_BODY"
  while IFS= read -r release; do
    tag="$(jq -r '.tag_name // empty' <<<"$release")"
    [[ -n "$tag" ]] || continue
    [[ "$tag" != "$baseline_tag" ]] || continue
    sha="$(resolve_tag_commit_sha "$repo" "$tag")"
    if [[ "$sha" == "$target_sha" ]]; then
      printf '%s\n' "$release"
      return 0
    fi
  done < <(jq -c '.[] | select(.draft|not) | select(.prerelease|not)' <<<"$releases")
  return 1
}

wait_for_release() {
  local repo="$1" merge_sha="$2" baseline_tag="$3" expected_tag="$4"
  local deadline now body msg tag_sha release_json release_tag release_url
  deadline=$(( $(date +%s) + TIMEOUT ))

  while :; do
    now="$(date +%s)"
    if (( now >= deadline )); then
      die "timed out waiting for a formal release for merge commit ${merge_sha}"
    fi

    github_request GET "/repos/${repo}/releases/tags/$(uri_encode "$expected_tag")"
    body="$RESPONSE_BODY"
    case "$HTTP_STATUS" in
      200)
        if [[ "$(jq -r '.draft // false' <<<"$body")" == "true" ]] || [[ "$(jq -r '.prerelease // false' <<<"$body")" == "true" ]]; then
          log "release ${expected_tag} exists but is not a formal release yet"
        else
          tag_sha="$(resolve_tag_commit_sha "$repo" "$expected_tag")"
          if [[ "$tag_sha" == "$merge_sha" ]]; then
            printf '%s\n' "$body"
            return 0
          fi
          log "release ${expected_tag} points to ${tag_sha}, expected ${merge_sha}; scanning recent stable releases"
        fi
        ;;
      404)
        ;;
      *)
        msg="$(jq -r '.message // empty' <<<"$body" 2>/dev/null || true)"
        die "failed to query release tag ${expected_tag} (${HTTP_STATUS})${msg:+: $msg}"
        ;;
    esac

    if release_json="$(find_recent_release_for_sha "$repo" "$merge_sha" "$baseline_tag")"; then
      printf '%s\n' "$release_json"
      return 0
    fi

    release_tag="${expected_tag}"
    release_url=""
    if [[ -n "$baseline_tag" ]]; then
      log "waiting for stable release after ${baseline_tag}; expected ${release_tag}"
    else
      log "waiting for first stable release; expected ${release_tag}"
    fi
    if [[ -n "$release_url" ]]; then
      log "latest matching release URL: ${release_url}"
    fi
    sleep "$POLL_INTERVAL"
  done
}

ensure_release_pr() {
  local repo="$1" owner prs payload response
  owner="${repo%%/*}"
  github_api GET "/repos/${repo}/pulls?state=open&base=$(uri_encode "$BASE_BRANCH")&head=$(uri_encode "${owner}:${HEAD_BRANCH}")&per_page=100"
  prs="$RESPONSE_BODY"

  PR_NUMBER="$(jq -r '.[0].number // empty' <<<"$prs")"
  PR_URL="$(jq -r '.[0].html_url // empty' <<<"$prs")"
  if [[ -n "$PR_NUMBER" ]]; then
    log "reusing open PR #${PR_NUMBER}"
    return
  fi

  if [[ -z "$TITLE" ]]; then
    TITLE="Release ${HEAD_BRANCH} to ${BASE_BRANCH}"
  fi
  if [[ -z "$BODY" ]]; then
    BODY="Automated release promotion from ${HEAD_BRANCH} to ${BASE_BRANCH}."
  fi

  payload="$(jq -n \
    --arg title "$TITLE" \
    --arg head "$HEAD_BRANCH" \
    --arg base "$BASE_BRANCH" \
    --arg body "$BODY" \
    '{title:$title, head:$head, base:$base, body:$body, maintainer_can_modify:true}')"
  github_api POST "/repos/${repo}/pulls" "$payload"
  response="$RESPONSE_BODY"
  PR_NUMBER="$(jq -r '.number // empty' <<<"$response")"
  PR_URL="$(jq -r '.html_url // empty' <<<"$response")"
  [[ -n "$PR_NUMBER" ]] || die "GitHub created a PR without a number"
  log "created PR #${PR_NUMBER}"
}

merge_release_pr() {
  local repo="$1" pr_number="$2" deadline now pr_json state response msg merged
  deadline=$(( $(date +%s) + TIMEOUT ))

  while :; do
    now="$(date +%s)"
    if (( now >= deadline )); then
      die "timed out waiting to merge PR #${pr_number}"
    fi

    github_api GET "/repos/${repo}/pulls/${pr_number}"
    pr_json="$RESPONSE_BODY"
    if [[ "$(jq -r '.merged // false' <<<"$pr_json")" == "true" ]]; then
      MERGE_SHA="$(jq -r '.merge_commit_sha // empty' <<<"$pr_json")"
      [[ -n "$MERGE_SHA" ]] || die "PR #${pr_number} is merged but merge_commit_sha is empty"
      return 0
    fi

    state="$(jq -r '.mergeable_state // "unknown"' <<<"$pr_json")"
    case "$state" in
      dirty)
        die "PR #${pr_number} has merge conflicts"
        ;;
      blocked|behind|draft|unknown)
        log "PR #${pr_number} state is ${state}; waiting"
        sleep "$POLL_INTERVAL"
        continue
        ;;
    esac

    github_request PUT "/repos/${repo}/pulls/${pr_number}/merge" "$(jq -n --arg method "$MERGE_METHOD" '{merge_method:$method}')"
    response="$RESPONSE_BODY"
    case "$HTTP_STATUS" in
      200)
        merged="$(jq -r '.merged // false' <<<"$response")"
        if [[ "$merged" == "true" ]]; then
          MERGE_SHA="$(jq -r '.sha // empty' <<<"$response")"
          [[ -n "$MERGE_SHA" ]] || die "merge API succeeded but returned no merge SHA"
          return 0
        fi
        msg="$(jq -r '.message // "merge API returned merged=false"' <<<"$response")"
        log "$msg"
        ;;
      405|409|422)
        msg="$(jq -r '.message // "merge is not ready"' <<<"$response")"
        if [[ "$msg" == *"conflict"* ]] || [[ "$msg" == *"not mergeable"* ]]; then
          die "PR #${pr_number} cannot be merged automatically: ${msg}"
        fi
        log "merge not ready yet: ${msg}"
        ;;
      *)
        msg="$(jq -r '.message // empty' <<<"$response" 2>/dev/null || true)"
        die "merge API failed (${HTTP_STATUS})${msg:+: ${msg}}"
        ;;
    esac

    sleep "$POLL_INTERVAL"
  done
}

run_create_release() {
  local root repo baseline_tag expected_tag release_json release_tag release_url

  require_cmd git
  require_cmd jq
  require_cmd gh
  resolve_auth
  log "GitHub API auth source: ${AUTH_SOURCE}"

  root="$(resolve_repo_root "$REPO_DIR")"
  repo="$(resolve_repo_name)"
  require_clean_worktree "$root"
  require_branch "$root"

  git -C "$root" fetch "$REMOTE" --tags >/dev/null 2>&1 || true
  baseline_tag="$(latest_release_tag "$repo")"
  expected_tag="$(next_patch_tag "$baseline_tag")"

  log "pushing ${HEAD_BRANCH} to ${REMOTE}"
  git -C "$root" push "$REMOTE" "$HEAD_BRANCH"

  ensure_release_pr "$repo"
  merge_release_pr "$repo" "$PR_NUMBER"
  release_json="$(wait_for_release "$repo" "$MERGE_SHA" "$baseline_tag" "$expected_tag")"

  release_tag="$(jq -r '.tag_name // empty' <<<"$release_json")"
  release_url="$(jq -r '.html_url // empty' <<<"$release_json")"

  printf 'repo=%s\n' "$repo"
  printf 'pr_number=%s\n' "$PR_NUMBER"
  printf 'pr_url=%s\n' "$PR_URL"
  printf 'merge_sha=%s\n' "$MERGE_SHA"
  printf 'release_tag=%s\n' "$release_tag"
  printf 'release_url=%s\n' "$release_url"
}

find_installer_repo_root() {
  local root
  root="$(resolve_repo_root "$REPO_DIR")"
  [[ -f "$root/scripts/alice-installer.sh" ]] || die "missing scripts/alice-installer.sh under ${root}"
  printf '%s\n' "$root"
}

run_update_self() {
  local root installer

  require_cmd bash
  root="$(find_installer_repo_root)"
  installer="${root}/scripts/alice-installer.sh"

  local -a cmd=(bash "$installer" update --channel "$CHANNEL")
  if [[ -n "$VERSION" ]]; then
    cmd+=(--version "$VERSION")
  fi
  if [[ -n "$ALICE_HOME_OVERRIDE" ]]; then
    cmd+=(--home "$ALICE_HOME_OVERRIDE")
  fi
  if [[ -n "$REPO" ]]; then
    cmd+=(--repo "$REPO")
  fi
  if [[ -n "$SERVICE_NAME" ]]; then
    cmd+=(--service "$SERVICE_NAME")
  fi

  log "running installer update from ${installer}"
  "${cmd[@]}"

  printf 'repo_dir=%s\n' "$root"
  printf 'channel=%s\n' "$CHANNEL"
  printf 'version=%s\n' "${VERSION:-latest}"
  printf 'alice_home=%s\n' "${ALICE_HOME_OVERRIDE:-${ALICE_HOME:-~/.alice}}"
}

parse_create_release_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --repo-dir)
        [[ $# -ge 2 ]] || die "--repo-dir requires a value"
        REPO_DIR="$2"
        shift 2
        ;;
      --repo)
        [[ $# -ge 2 ]] || die "--repo requires a value"
        REPO="$2"
        shift 2
        ;;
      --remote)
        [[ $# -ge 2 ]] || die "--remote requires a value"
        REMOTE="$2"
        shift 2
        ;;
      --head)
        [[ $# -ge 2 ]] || die "--head requires a value"
        HEAD_BRANCH="$2"
        shift 2
        ;;
      --base)
        [[ $# -ge 2 ]] || die "--base requires a value"
        BASE_BRANCH="$2"
        shift 2
        ;;
      --title)
        [[ $# -ge 2 ]] || die "--title requires a value"
        TITLE="$2"
        shift 2
        ;;
      --body)
        [[ $# -ge 2 ]] || die "--body requires a value"
        BODY="$2"
        shift 2
        ;;
      --poll-interval)
        [[ $# -ge 2 ]] || die "--poll-interval requires a value"
        POLL_INTERVAL="$2"
        shift 2
        ;;
      --timeout)
        [[ $# -ge 2 ]] || die "--timeout requires a value"
        TIMEOUT="$2"
        shift 2
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        die "unknown create-release argument: $1"
        ;;
    esac
  done

  [[ -n "$REPO_DIR" ]] || REPO_DIR="."
}

parse_update_self_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --repo-dir)
        [[ $# -ge 2 ]] || die "--repo-dir requires a value"
        REPO_DIR="$2"
        shift 2
        ;;
      --repo)
        [[ $# -ge 2 ]] || die "--repo requires a value"
        REPO="$2"
        shift 2
        ;;
      --channel)
        [[ $# -ge 2 ]] || die "--channel requires a value"
        CHANNEL="$2"
        shift 2
        ;;
      --version)
        [[ $# -ge 2 ]] || die "--version requires a value"
        VERSION="$2"
        shift 2
        ;;
      --home)
        [[ $# -ge 2 ]] || die "--home requires a value"
        ALICE_HOME_OVERRIDE="$2"
        shift 2
        ;;
      --service)
        [[ $# -ge 2 ]] || die "--service requires a value"
        SERVICE_NAME="$2"
        shift 2
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        die "unknown update-self argument: $1"
        ;;
    esac
  done

  [[ -n "$REPO_DIR" ]] || REPO_DIR="."
}

case "$ACTION" in
  create-release)
    parse_create_release_args "$@"
    run_create_release
    ;;
  update-self)
    parse_update_self_args "$@"
    run_update_self
    ;;
  auth-status)
    require_cmd gh
    gh auth status
    ;;
  help|-h|--help|"")
    usage
    ;;
  *)
    die "unknown action: $ACTION"
    ;;
esac
