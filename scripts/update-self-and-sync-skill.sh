#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
default_repo="$(cd "$script_dir/.." && pwd -P)"
repo="${ALICE_REPO:-$default_repo}"
service_name="alice-codex-connector.service"
skip_pull=0
skip_restart=0
restart_cmd="${ALICE_RESTART_CMD:-}"

alice_home="${ALICE_HOME:-${HOME}/.alice}"
install_bin="$alice_home/bin/alice"
pid_file="$alice_home/run/alice.pid"

if [[ -n "${CODEX_HOME:-}" ]]; then
  default_codex_home="$CODEX_HOME"
else
  default_codex_home="${alice_home}/.codex"
fi
sync_state_file="$default_codex_home/state/alice/sync-state.md"

usage() {
  cat <<'USAGE'
Usage:
  update-self-and-sync-skill.sh [--repo PATH] [--service NAME] [--install-bin PATH] [--pid-file PATH] [--restart-cmd CMD] [--sync-state-file PATH] [--skip-pull] [--skip-restart]

Options:
  --repo PATH             Target alice repository path (default: infer from script location)
  --service NAME          User systemd service name (default: alice-codex-connector.service)
  --install-bin PATH      Install target binary path (default: $ALICE_HOME/bin/alice)
  --pid-file PATH         Runtime pid file path for non-systemd mode (default: $ALICE_HOME/run/alice.pid)
  --restart-cmd CMD       Custom restart command (highest priority when restart is enabled)
  --sync-state-file PATH  Sync snapshot markdown path (default: $CODEX_HOME/state/alice/sync-state.md or $ALICE_HOME/.codex/state/alice/sync-state.md)
  --skip-pull             Skip git pull (still build/restart/sync)
  --skip-restart          Skip restart attempt (still pull/build/sync)
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --repo)
      repo="${2:-}"
      shift 2
      ;;
    --service)
      service_name="${2:-}"
      shift 2
      ;;
    --install-bin)
      install_bin="${2:-}"
      shift 2
      ;;
    --pid-file)
      pid_file="${2:-}"
      shift 2
      ;;
    --restart-cmd)
      restart_cmd="${2:-}"
      shift 2
      ;;
    --sync-state-file)
      sync_state_file="${2:-}"
      shift 2
      ;;
    --skip-pull)
      skip_pull=1
      shift
      ;;
    --skip-restart)
      skip_restart=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

if [[ ! -d "$repo" ]]; then
  echo "repo not found: $repo" >&2
  exit 1
fi
if ! git -C "$repo" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  echo "not a git repo: $repo" >&2
  exit 1
fi

before_commit="$(git -C "$repo" rev-parse --short HEAD)"
branch="$(git -C "$repo" rev-parse --abbrev-ref HEAD)"
pull_result="skipped"

if [[ "$skip_pull" -eq 0 ]]; then
  pull_output="$(git -C "$repo" pull --ff-only 2>&1)"
  pull_result="$pull_output"
fi

after_commit="$(git -C "$repo" rev-parse --short HEAD)"
last_commit_subject="$(git -C "$repo" log -1 --pretty=%s | tr -d '\r')"
updated_at=""

service_present="no"
service_active="unmanaged"
service_enabled="unmanaged"
restart_result="skipped"

restart_by_pid_file() {
  local path="$1"
  if [[ ! -f "$path" ]]; then
    echo "unmanaged (pid file missing; start manually)"
    return 0
  fi

  local pid
  pid="$(tr -d '[:space:]' <"$path" 2>/dev/null || true)"
  if [[ ! "$pid" =~ ^[0-9]+$ ]] || [[ "$pid" -le 1 ]]; then
    echo "pid-file-invalid ($path)"
    return 0
  fi

  if kill -0 "$pid" >/dev/null 2>&1; then
    if kill -TERM "$pid" >/dev/null 2>&1; then
      echo "pid-terminated:$pid (manual start required)"
      return 0
    fi
    echo "pid-stop-failed:$pid"
    return 0
  fi
  echo "pid-not-running:$pid"
}

write_sync_state() {
  updated_at="$(date -Iseconds)"
  mkdir -p "$(dirname "$sync_state_file")"
  cat >"$sync_state_file" <<STATE
# Skill Sync State

- updated_at: $updated_at
- repo_path: $repo
- branch: $branch
- before_commit: $before_commit
- after_commit: $after_commit
- last_commit_subject: $last_commit_subject
- install_bin: $install_bin
- pid_file: $pid_file
- service_name: $service_name
- service_present: $service_present
- service_active: $service_active
- service_enabled: $service_enabled
- skip_pull: $skip_pull
- skip_restart: $skip_restart

## Pull Result

\`\`\`
$pull_result
\`\`\`

## Restart Result

\`\`\`
$restart_result
\`\`\`
STATE
}

mkdir -p "$(dirname "$install_bin")"
(
  cd "$repo"
  go build -o "$install_bin" ./cmd/connector
)
chmod +x "$install_bin"

if command -v systemctl >/dev/null 2>&1; then
  if systemctl --user list-unit-files "$service_name" >/dev/null 2>&1; then
    service_present="yes"
    service_active="$(systemctl --user is-active "$service_name" 2>/dev/null || echo unknown)"
    service_enabled="$(systemctl --user is-enabled "$service_name" 2>/dev/null || echo unknown)"
  else
    service_present="no"
  fi
else
  service_present="systemctl-missing"
fi

if [[ "$skip_restart" -eq 0 ]]; then
  if [[ -n "${restart_cmd// }" ]]; then
    restart_result="restart-cmd-running"
    write_sync_state
    if restart_output="$(bash -lc "$restart_cmd" 2>&1)"; then
      if [[ -n "${restart_output// }" ]]; then
        restart_result="restart-cmd-ok: $restart_output"
      else
        restart_result="restart-cmd-ok"
      fi
    else
      restart_result="restart-cmd-failed: ${restart_output:-<no-output>}"
    fi
  elif [[ "$service_present" == "yes" ]]; then
    export XDG_RUNTIME_DIR="/run/user/$(id -u)"
    export DBUS_SESSION_BUS_ADDRESS="unix:path=$XDG_RUNTIME_DIR/bus"
    restart_result="systemd-restart-requested"
    write_sync_state
    if systemctl --user restart --no-block "$service_name"; then
      sleep 1
      service_active="$(systemctl --user is-active "$service_name" 2>/dev/null || echo unknown)"
      service_enabled="$(systemctl --user is-enabled "$service_name" 2>/dev/null || echo unknown)"
      restart_result="systemd:$service_active"
    else
      restart_result="systemd:restart-failed"
    fi
  else
    restart_result="$(restart_by_pid_file "$pid_file")"
  fi
fi

write_sync_state

echo "=== update-self-and-sync-skill done ==="
echo "repo: $repo"
echo "branch: $branch"
echo "before_commit: $before_commit"
echo "after_commit: $after_commit"
echo "install_bin: $install_bin"
echo "service_present: $service_present"
echo "service_active: $service_active"
echo "service_enabled: $service_enabled"
echo "restart_result: $restart_result"
echo "sync_state_file: $sync_state_file"
