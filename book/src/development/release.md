# Release Process

How Alice releases are built and published.

## Branch Strategy

- Day-to-day development happens on **`dev`**
- Releases go through **`dev → main`** merge commits only
- Never push directly to `main`

## CI Pipeline

### On `dev` Push
1. Run quality gate (`make check`)
2. Build dev binaries
3. Update prerelease `dev-latest`

### On `main` Merge from `dev`
1. Run quality gate (`make check`)
2. Auto-create next `vX.Y.Z` tag
3. Build release binaries for all platforms
4. Publish GitHub Release

### Manual `v*` Tags
- Pushing a `v*` tag triggers the release workflow directly

## Release Artifacts

Each release publishes:
- Binary builds for: linux-amd64, linux-arm64, darwin-amd64, darwin-arm64, win32-x64
- npm package: `@alice_space/alice`
- Installer script: `scripts/alice-installer.sh`

## Making a Release

1. Ensure `dev` passes all checks and is ready
2. Create a PR from `dev` to `main`
3. Merge with **merge commit** (do NOT squash or rebase)
4. CI auto-creates the tag and publishes the release
5. Verify the GitHub Release shows all artifacts

## Version Numbers

Tags follow semver: `vX.Y.Z`. The CI auto-increments the patch version from the previous release tag.

## Post-Release

- The installer script (`alice-installer.sh`) automatically picks up the latest release
- npm users get the update via `npm update -g @alice_space/alice`

## CI Workflow Files

- `.github/workflows/ci.yml` — Dev branch quality gate and dev binaries
- `.github/workflows/main-release.yml` — Main branch release build
- `.github/workflows/release-on-tag.yml` — Manual tag release
