# Install Alice

Three ways to install. Pick the one that fits your workflow.

## npm (Recommended)

```bash
npm install -g @alice_space/alice
```

After installation, run the setup wizard:

```bash
alice setup
```

This creates `~/.alice/`, writes a starter `config.yaml`, syncs bundled skills, registers a systemd user unit (Linux), and installs the OpenCode delegate plugin.

**Requirements:** Node.js 18+

## Installer Script

Single-command install from GitHub Releases:

```bash
# Install latest stable release
curl -fsSL https://cdn.jsdelivr.net/gh/Alice-space/alice@main/scripts/alice-installer.sh | bash -s -- install

# Install a specific version
curl -fsSL https://cdn.jsdelivr.net/gh/Alice-space/alice@main/scripts/alice-installer.sh | bash -s -- install --version v1.2.3

# Uninstall
curl -fsSL https://cdn.jsdelivr.net/gh/Alice-space/alice@main/scripts/alice-installer.sh | bash -s -- uninstall
```

The installer downloads the correct binary for your platform (darwin-amd64, darwin-arm64, linux-amd64, linux-arm64, win32-x64) and verifies checksums.

After installation, run `alice setup` to initialize the config and skills directory.

**Requirements:** `curl`, `tar`

## Build from Source

```bash
git clone https://github.com/Alice-space/alice.git
cd alice
go build -o bin/alice ./cmd/connector
```

Optionally install to your PATH:

```bash
cp bin/alice /usr/local/bin/alice
```

**Requirements:** Go 1.25+

## Verify Installation

```bash
alice --version
```

Should print the version string. If `alice setup` has been run, you can also check:

```bash
ls ~/.alice/
# config.yaml  skills/  log/  bots/
```

## Runtime Home

Alice uses different default home directories depending on the build channel:

| Build | Default Home |
|-------|-------------|
| Release (npm / installer) | `~/.alice` |
| Dev (source build) | `~/.alice-dev` |

Override with `--alice-home` or the `ALICE_HOME` environment variable.
