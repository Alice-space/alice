# Skill Sync State

Runtime self-update snapshots are written by default to:

- `${CODEX_HOME:-$HOME/.codex}/state/alice/sync-state.md`

Use this path to inspect latest pull/build/restart status after running:

- `$ALICE_REPO/scripts/update-self-and-sync-skill.sh`
- `$CODEX_HOME/skills/alice-codebase-onboarding/scripts/update-self-and-sync-skill.sh`

Current snapshot fields are written by the updater script and include:

- `updated_at`
- `repo_path`
- `branch`
- `before_commit`
- `after_commit`
- `last_commit_subject`
- `service_name`
- `service_active`
- `service_enabled`
- `skip_pull`
- `skip_restart`
- `Pull Result`
- `Restart Result`

Interpretation:

- `after_commit != before_commit` means a pull advanced the checkout.
- `service_active` / `service_enabled` reflect the post-check values the script could observe.
- `Restart Result` may stay in a pre-restart placeholder state if the updater restarts the current process before a later check can complete.
