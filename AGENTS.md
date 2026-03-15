# AGENTS Collaboration Guide (Alice)

This file is the quick-entry guide for collaborators.  
Canonical execution standard remains: [AGENT.md](./AGENT.md).

## Branch And Release Workflow (Mandatory)

- Default development branch: `dev`.
- Do not push directly to `main`.
- Only merge `dev -> main`.
- PRs to `main` must come from `dev`.
- Use merge-commit for `dev -> main` (do not squash/rebase for release path).

## CI Behavior Summary

- `dev` push:
  - run quality gate
  - build dev binaries
  - update prerelease `dev-latest`
- `main` merge from `dev`:
  - run quality gate
  - auto-create next `vX.Y.Z` tag
  - build and publish GitHub Release
- manual `v*` tags:
  - still trigger release workflow

## Runtime Home Defaults By Build Channel

- release build default: `~/.alice`
- dev build default: `~/.alice-dev`
- explicit `ALICE_HOME` / `--alice-home` overrides both defaults
