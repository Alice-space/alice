#!/usr/bin/env bash
set -euo pipefail

REPO_DEFAULT="Alice-space/alice"
ACTION="install"
if [[ $# -gt 0 && "$1" != -* ]]; then
  ACTION="$1"
  shift
fi

REPO="${ALICE_REPO:-$REPO_DEFAULT}"
ALICE_HOME="${ALICE_HOME:-}"
CHANNEL="release"
SERVICE_NAME="alice.service"
SERVICE_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
SERVICE_FILE=""
BIN_PATH=""
CONFIG_PATH=""
KEEP_DATA=0
VERSION=""

usage() {
  cat <<USAGE
Usage:
  alice-installer.sh install [--version vX.Y.Z] [--channel release|dev] [--home PATH] [--repo OWNER/REPO] [--service NAME]
  alice-installer.sh update  [--version vX.Y.Z] [--channel release|dev] [--home PATH] [--repo OWNER/REPO] [--service NAME]
  alice-installer.sh uninstall [--home PATH] [--service NAME] [--keep-data]

Examples:
  alice-installer.sh install
  alice-installer.sh install --channel dev
  alice-installer.sh update --version vX.Y.Z
  alice-installer.sh uninstall --keep-data
USAGE
}

log() {
  printf '[alice-installer] %s\n' "$*"
}

die() {
  printf '[alice-installer] ERROR: %s\n' "$*" >&2
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"
}

trim() {
  local text="${1:-}"
  text="${text#${text%%[![:space:]]*}}"
  text="${text%${text##*[![:space:]]}}"
  printf '%s' "$text"
}

normalize_alice_paths() {
  ALICE_HOME="$(trim "$ALICE_HOME")"
  [[ -n "$ALICE_HOME" ]] || die "ALICE_HOME is empty"
  case "$ALICE_HOME" in
    ~/*) ALICE_HOME="$HOME/${ALICE_HOME#~/}" ;;
    '~') ALICE_HOME="$HOME" ;;
  esac
  if [[ "$ALICE_HOME" != /* ]]; then
    ALICE_HOME="$(pwd)/$ALICE_HOME"
  fi

  local parent_dir base_name
  parent_dir="$(dirname "$ALICE_HOME")"
  base_name="$(basename "$ALICE_HOME")"
  [[ -n "$base_name" && "$base_name" != "." && "$base_name" != "/" ]] || die "invalid ALICE_HOME: $ALICE_HOME"

  mkdir -p "$parent_dir" || die "failed to create ALICE_HOME parent directory: $parent_dir"
  parent_dir="$(cd "$parent_dir" && pwd)" || die "failed to resolve ALICE_HOME parent directory: $parent_dir"
  ALICE_HOME="${parent_dir}/${base_name}"
  ALICE_HOME="${ALICE_HOME%/}"

  SERVICE_FILE="$SERVICE_DIR/$SERVICE_NAME"
  BIN_PATH="$ALICE_HOME/bin/alice"
  CONFIG_PATH="$ALICE_HOME/config.yaml"
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --home)
        [[ $# -ge 2 ]] || die "--home requires a value"
        ALICE_HOME="$2"
        shift 2
        ;;
      --repo)
        [[ $# -ge 2 ]] || die "--repo requires a value"
        REPO="$2"
        shift 2
        ;;
      --service)
        [[ $# -ge 2 ]] || die "--service requires a value"
        SERVICE_NAME="$2"
        shift 2
        ;;
      --version)
        [[ $# -ge 2 ]] || die "--version requires a value"
        VERSION="$2"
        shift 2
        ;;
      --channel)
        [[ $# -ge 2 ]] || die "--channel requires a value"
        CHANNEL="$2"
        shift 2
        ;;
      --keep-data)
        KEEP_DATA=1
        shift
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

systemctl_user() {
  local uid runtime_dir bus
  uid="$(id -u)"
  runtime_dir="${XDG_RUNTIME_DIR:-/run/user/$uid}"
  bus="${DBUS_SESSION_BUS_ADDRESS:-unix:path=$runtime_dir/bus}"
  XDG_RUNTIME_DIR="$runtime_dir" DBUS_SESSION_BUS_ADDRESS="$bus" systemctl --user "$@"
}

require_systemd_user() {
  require_cmd systemctl
  if ! systemctl_user --version >/dev/null 2>&1; then
    die "systemd --user is unavailable in current session"
  fi
}

has_systemd_user() {
  command -v systemctl >/dev/null 2>&1 || return 1
  systemctl_user --version >/dev/null 2>&1
}

detect_arch() {
  local arch
  arch="$(uname -m)"
  case "$arch" in
    x86_64|amd64) printf 'amd64' ;;
    aarch64|arm64) printf 'arm64' ;;
    *) die "unsupported architecture: $arch" ;;
  esac
}

detect_os() {
  local os
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "$os" in
    linux) printf 'linux' ;;
    *) die "this installer currently supports Linux only (detected: $os)" ;;
  esac
}

fetch_latest_version() {
  local api tag
  api="https://api.github.com/repos/$REPO/releases/latest"
  tag="$(curl -fsSL "$api" | grep -m1 '"tag_name"' | sed -E 's/.*"tag_name"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/')"
  tag="$(trim "$tag")"
  [[ -n "$tag" ]] || die "failed to fetch latest release tag from $api"
  printf '%s' "$tag"
}

verify_asset_checksum() {
  local version asset file_path tmpdir sums_url sums_file expected actual
  version="$1"
  asset="$2"
  file_path="$3"
  tmpdir="$4"
  sums_url="https://github.com/$REPO/releases/download/${version}/SHA256SUMS"
  sums_file="$tmpdir/SHA256SUMS"

  if ! curl -fsSL "$sums_url" -o "$sums_file"; then
    log "SHA256SUMS not found for $version; skip checksum verification"
    return
  fi

  expected="$(grep -E "[[:space:]]${asset}$" "$sums_file" | awk '{print $1}' | head -n1 || true)"
  [[ -n "$expected" ]] || die "SHA256SUMS missing checksum entry for $asset"

  if command -v sha256sum >/dev/null 2>&1; then
    actual="$(sha256sum "$file_path" | awk '{print $1}')"
  elif command -v shasum >/dev/null 2>&1; then
    actual="$(shasum -a 256 "$file_path" | awk '{print $1}')"
  else
    die "missing checksum tool: sha256sum or shasum"
  fi

  [[ "$actual" == "$expected" ]] || die "checksum verification failed for $asset"
  log "checksum verified for $asset"
}

download_and_install_binary() {
  local version channel os arch asset url tmpdir src src_name
  version="$1"
  channel="$2"
  os="$(detect_os)"
  arch="$(detect_arch)"
  case "$channel" in
    release)
      asset="alice_${version}_${os}_${arch}.tar.gz"
      src_name="alice_${version}_${os}_${arch}"
      ;;
    dev)
      asset="alice_dev_${os}_${arch}.tar.gz"
      src_name="alice_dev_${os}_${arch}"
      ;;
    *)
      die "unsupported channel: $channel"
      ;;
  esac
  url="https://github.com/$REPO/releases/download/${version}/${asset}"

  tmpdir="$(mktemp -d)"
  trap 'if [[ -n "${tmpdir:-}" ]]; then rm -rf "${tmpdir}"; fi' RETURN

  log "downloading $url"
  curl -fL "$url" -o "$tmpdir/$asset"
  verify_asset_checksum "$version" "$asset" "$tmpdir/$asset" "$tmpdir"
  tar -xzf "$tmpdir/$asset" -C "$tmpdir"

  src="$tmpdir/$src_name"
  [[ -f "$src" ]] || die "downloaded archive does not contain expected binary: $(basename "$src")"

  mkdir -p "$(dirname "$BIN_PATH")"
  install -m 0755 "$src" "$BIN_PATH"
  log "installed binary to $BIN_PATH"
}

enable_linger_if_possible() {
  if ! command -v loginctl >/dev/null 2>&1; then
    return
  fi

  local linger_state
  linger_state="$(loginctl show-user "$USER" -p Linger --value 2>/dev/null || true)"
  if [[ "$linger_state" == "yes" ]]; then
    return
  fi

  if loginctl enable-linger "$USER" >/dev/null 2>&1; then
    log "enabled linger for user $USER (service keeps running after logout)"
    return
  fi

  log "warning: failed to enable linger automatically; run: sudo loginctl enable-linger $USER"
}

write_systemd_unit() {
  mkdir -p "$SERVICE_DIR"
  cat > "$SERVICE_FILE" <<UNIT
[Unit]
Description=Alice Connector
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
Environment=ALICE_HOME=$ALICE_HOME
WorkingDirectory=$ALICE_HOME
ExecStart=$BIN_PATH --feishu-websocket
Restart=on-failure
RestartSec=3
NoNewPrivileges=yes

[Install]
WantedBy=default.target
UNIT
  log "wrote service unit: $SERVICE_FILE"
}

install_or_update() {
  require_cmd curl
  require_cmd tar
  require_cmd install
  require_systemd_user

  mkdir -p "$ALICE_HOME/bin" "$ALICE_HOME/log" "$ALICE_HOME/run"

  local version
  version="$VERSION"
  if [[ -z "$version" ]]; then
    if [[ "$CHANNEL" == "dev" ]]; then
      version="dev-latest"
    else
      version="$(fetch_latest_version)"
    fi
  fi
  log "target channel: $CHANNEL"
  log "target version: $version"

  download_and_install_binary "$version" "$CHANNEL"
  if HOME="$HOME" ALICE_HOME="$ALICE_HOME" "$BIN_PATH" --alice-home "$ALICE_HOME" skills sync >/dev/null 2>&1; then
    log "bundled skills synced into $HOME/.agents/skills (source: $ALICE_HOME/skills)"
  else
    log "warning: bundled skill sync failed during install; Alice will retry on startup"
  fi
  write_systemd_unit

  enable_linger_if_possible
  systemctl_user daemon-reload

  systemctl_user enable "$SERVICE_NAME" >/dev/null
  if systemctl_user is-active --quiet "$SERVICE_NAME"; then
    systemctl_user restart "$SERVICE_NAME"
    log "service restarted: $SERVICE_NAME"
  else
    systemctl_user start "$SERVICE_NAME"
    sleep 1
    if systemctl_user is-active --quiet "$SERVICE_NAME"; then
      log "service started: $SERVICE_NAME"
    else
      log "service exited after startup (likely first-run config bootstrap)"
      log "check/edit config: $CONFIG_PATH"
      log "start command: systemctl --user restart $SERVICE_NAME"
    fi
  fi
}

validate_alice_home_for_delete() {
  local target
  target="$(trim "$1")"
  [[ -n "$target" ]] || die "refusing to delete empty ALICE_HOME"

  case "$target" in
    /|/home|/root|/usr|/var|/etc|/opt|/tmp|/bin|/sbin|/lib|/lib64)
      die "refusing to delete high-risk path: $target"
      ;;
  esac

  if [[ "$target" == "$HOME" ]]; then
    die "refusing to delete HOME directory: $target"
  fi
}

uninstall() {
  if has_systemd_user; then
    if systemctl_user list-unit-files | grep -q "^$SERVICE_NAME"; then
      log "stopping and disabling service: $SERVICE_NAME"
      systemctl_user disable --now "$SERVICE_NAME" >/dev/null 2>&1 || true
    else
      log "service unit not found; stopping if running: $SERVICE_NAME"
      systemctl_user stop "$SERVICE_NAME" >/dev/null 2>&1 || true
    fi
  else
    log "systemd --user unavailable; skipping service stop/disable"
  fi

  if [[ -f "$SERVICE_FILE" ]]; then
    rm -f "$SERVICE_FILE"
    log "removed systemd unit file: $SERVICE_FILE"
  else
    log "systemd unit file not found: $SERVICE_FILE"
  fi
  if has_systemd_user; then
    systemctl_user daemon-reload >/dev/null 2>&1 || true
    systemctl_user reset-failed >/dev/null 2>&1 || true
    log "reloaded user systemd daemon and reset failed state"
  fi

  rm -f "$BIN_PATH"
  if [[ "$KEEP_DATA" -eq 0 ]]; then
    validate_alice_home_for_delete "$ALICE_HOME"
    rm -rf -- "$ALICE_HOME"
    log "removed $ALICE_HOME"
  else
    log "kept data directory: $ALICE_HOME"
  fi

  log "uninstall completed"
}

main() {
  case "$ACTION" in
    install|update|uninstall) ;;
    *)
      usage
      die "unsupported action: $ACTION"
      ;;
  esac

  parse_args "$@"
  CHANNEL="$(trim "$CHANNEL")"
  case "$CHANNEL" in
    release|dev) ;;
    *)
      die "unsupported channel: $CHANNEL"
      ;;
  esac
  if [[ -z "$(trim "$ALICE_HOME")" ]]; then
    if [[ "$CHANNEL" == "dev" ]]; then
      ALICE_HOME="$HOME/.alice-dev"
    else
      ALICE_HOME="$HOME/.alice"
    fi
  fi
  normalize_alice_paths

  case "$ACTION" in
    install|update)
      install_or_update
      ;;
    uninstall)
      uninstall
      ;;
  esac
}

main "$@"
