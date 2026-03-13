# Alice Deploy And Runbook

Repository default: `${ALICE_REPO:-$HOME/alice}`  
Recommended service model: user-level systemd service.

## Feature delivery sequence (required)

When adding new capability:

1. Commit and push repo changes first.
- `git -C "$ALICE_REPO" status`
- `git -C "$ALICE_REPO" add <intended-files>`
- `git -C "$ALICE_REPO" commit -m "<clear-message>"`
- `git -C "$ALICE_REPO" push`

2. Then run unified self-update command.
- Canonical: `$ALICE_REPO/scripts/update-self-and-sync-skill.sh`
- Wrapper: `$CODEX_HOME/skills/alice-codebase-onboarding/scripts/update-self-and-sync-skill.sh`

## Unified self-update command (required)

Use one command for pull/build/restart/sync snapshot:

- `$ALICE_REPO/scripts/update-self-and-sync-skill.sh`

This command handles:

- repository update (`git pull --ff-only`)
- binary build (`bin/alice-connector`, including `runtime` subcommands for bundled skills)
- user service restart (`systemctl --user restart --no-block alice-codex-connector.service`)
- sync snapshot write (default: `$CODEX_HOME/state/alice/sync-state.md`)

## Local run checklist

1. Prerequisites
- `go version`
- `codex` CLI installed
- login state valid for service user:
  - `HOME=$HOME CODEX_HOME=${CODEX_HOME:-$HOME/.codex} codex login status`

2. Config
- `cp config.example.yaml config.yaml`
- verify key fields:
  - `feishu_app_id`
  - `feishu_app_secret`
  - `codex_command`
  - `workspace_dir`
  - `memory_dir`

3. Build and test
- `go mod tidy`
- `go test ./...`
- `go build -o bin/alice-connector ./cmd/connector`

4. Foreground run
- `./bin/alice-connector -c config.yaml`

## User-level systemd deployment

Create service file:
- `~/.config/systemd/user/alice-codex-connector.service`

Core fields:
- `WorkingDirectory=%h/alice`
- `Environment=HOME=%h`
- `Environment=CODEX_HOME=%h/.codex`
- `ExecStart=%h/alice/bin/alice-connector -c %h/alice/config.yaml`
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
- `ls -l "$ALICE_REPO/bin/alice-connector"`
- verify `ExecStart` path and working directory.

2. Codex call fails
- `HOME=$HOME CODEX_HOME=${CODEX_HOME:-$HOME/.codex} codex login status`
- verify `codex_command` in `config.yaml`.

3. Feishu events not received
- re-check app credentials and event subscription.
- verify long connection mode and required permissions.

4. Memory/state not updating
- check `memory_dir` path and permissions.
- verify `.memory/session_state.json` and `.memory/runtime_state.json` write access.

5. Skill/repo drift
- run `$ALICE_REPO/scripts/update-self-and-sync-skill.sh`
- inspect `$CODEX_HOME/state/alice/sync-state.md`.
