# Alice Deploy And Self-Update Runbook

Repository default: `${ALICE_REPO:-$HOME/alice}`  
Default runtime home: `${ALICE_HOME:-$HOME/.alice}`  
Recommended runtime model: direct binary run (systemd optional).

## Canonical commands

- Runtime health check:
  - `$CODEX_HOME/skills/alice-codebase-onboarding/scripts/check_alice_runtime.sh`
- Canonical updater:
  - `$ALICE_REPO/scripts/update-self-and-sync-skill.sh`
- Skill wrapper:
  - `$CODEX_HOME/skills/alice-codebase-onboarding/scripts/update-self-and-sync-skill.sh`

The wrapper should only dispatch to the repo script. The repo script is the source of truth.

## Feature delivery sequence

1. Commit and push repo changes first.
- `git -C "$ALICE_REPO" status`
- `git -C "$ALICE_REPO" add <intended-files>`
- `git -C "$ALICE_REPO" commit -m "<clear-message>"`
- `git -C "$ALICE_REPO" push`

2. Then run the canonical updater.
- preferred: `$ALICE_REPO/scripts/update-self-and-sync-skill.sh`
- equivalent wrapper: `$CODEX_HOME/skills/alice-codebase-onboarding/scripts/update-self-and-sync-skill.sh`

3. Then inspect the sync snapshot.
- `${CODEX_HOME:-${ALICE_HOME:-$HOME/.alice}/.codex}/state/alice/sync-state.md`

## What the updater actually does

`scripts/update-self-and-sync-skill.sh` handles:

- `git pull --ff-only` unless `--skip-pull`
- `go build -o ${ALICE_HOME:-$HOME/.alice}/bin/alice ./cmd/connector`
- restart attempt (priority: `--restart-cmd` > systemd service > pid file stop/manual start) unless `--skip-restart`
- sync snapshot write before/after restart attempt

Supported flags:

- `--repo PATH`
- `--service NAME`
- `--install-bin PATH`
- `--pid-file PATH`
- `--restart-cmd CMD`
- `--sync-state-file PATH`
- `--skip-pull`
- `--skip-restart`

## Host requirements

For self-update to succeed on the target host, expect:

- `git`
- `go`
- valid repo checkout at `$ALICE_REPO`

If `go` is missing, the updater cannot rebuild the runtime binary; report that as a hard blocker.

Runtime skill execution is a different path:

- bundled skills first honor `ALICE_RUNTIME_BIN`
- then fall back to `${ALICE_HOME:-$HOME/.alice}/bin/alice`
- then `alice` from `PATH`

That means normal runtime skill usage does not imply a Go toolchain is present.

## Local run checklist

1. Prerequisites
- `go version`
- `codex` CLI installed
- login state valid for the runtime user:
  - `HOME=$HOME CODEX_HOME=${CODEX_HOME:-${ALICE_HOME:-$HOME/.alice}/.codex} codex login status`

2. Config
- `mkdir -p "${ALICE_HOME:-$HOME/.alice}"`
- `cp config.example.yaml "${ALICE_HOME:-$HOME/.alice}/config.yaml"`
- verify key fields:
  - `feishu_app_id`
  - `feishu_app_secret`
  - `codex_command`
  - `workspace_dir`
  - `memory_dir`
  - `runtime_http_addr`

3. Build and test
- `go test ./...`
- `go build -o "${ALICE_HOME:-$HOME/.alice}/bin/alice" ./cmd/connector`

4. Foreground run
- `./bin/alice`

## User-level systemd deployment

Create service file:

- `~/.config/systemd/user/alice-codex-connector.service`

Core fields:

- `WorkingDirectory=%h`
- `Environment=ALICE_HOME=%h/.alice`
- `Environment=HOME=%h`
- `Environment=CODEX_HOME=%h/.alice/.codex`
- `ExecStart=%h/.alice/bin/alice -c %h/.alice/config.yaml`
- `Restart=always`

Enable and start:

- `systemctl --user daemon-reload`
- `systemctl --user enable --now alice-codex-connector.service`

Inspect:

- `systemctl --user status --no-pager alice-codex-connector.service`
- `journalctl --user-unit alice-codex-connector.service -f`
- fallback: `journalctl --user -u alice-codex-connector.service -f`

Restart after code/config update:

- preferred: `$ALICE_REPO/scripts/update-self-and-sync-skill.sh`
- fallback: `systemctl --user restart --no-block alice-codex-connector.service`

## Quick troubleshooting matrix

1. Service inactive
- `ls -l "${ALICE_HOME:-$HOME/.alice}/bin/alice"`
- verify `ExecStart` path and working directory.

2. Codex call fails
- `HOME=$HOME CODEX_HOME=${CODEX_HOME:-${ALICE_HOME:-$HOME/.alice}/.codex} codex login status`
- verify `codex_command` in `${ALICE_HOME:-$HOME/.alice}/config.yaml`.

3. Feishu events not received
- re-check app credentials and event subscription.
- verify long connection mode and required permissions.

4. Memory/state not updating
- check `memory_dir` path and permissions.
- verify `${ALICE_HOME:-$HOME/.alice}/memory/session_state.json` and `${ALICE_HOME:-$HOME/.alice}/memory/runtime_state.json` write access.

5. Skill/repo drift or uncertain rollout state
- run `$ALICE_REPO/scripts/update-self-and-sync-skill.sh`
- inspect `${CODEX_HOME:-${ALICE_HOME:-$HOME/.alice}/.codex}/state/alice/sync-state.md`
