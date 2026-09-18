# xtool Tart runner

Spins up isolated Linux GitHub Actions runners on a macOS host, for xtool.

(NB: very vibe coded)

## Prerequisites

- An Apple Silicon Mac running macOS 13 or newer
- Xcode installed
- Go 1.25.3 or newer when building from source

## Setup

### Install tartscaleset

Install the [latest continuous release](https://github.com/xtool-org/mac-actions-runner/releases/tag/continuous):

```bash
curl -fsSL https://github.com/xtool-org/mac-actions-runner/releases/download/continuous/tartscaleset-darwin-arm64.tar.gz \
| sudo tar -xz -C /usr/local/bin
```

### Install GitHub App

1. Create a GitHub App on the target organization with organization self-hosted runner read/write permission
2. Save the private key to `~/.config/tartscaleset/private-key.pem`
3. Protect the App private key: `chmod 600 ~/.config/tartscaleset/private-key.pem`

The scale-set controller discovers the App installation ID automatically from
`APP_ID`, `ORG_NAME`, and the private key.

## Run

Register as a LaunchAgent:

```bash
tartscaleset register # use unregister to disable
```

Or run in the foreground:

```bash
tartscaleset
```

## Updating the runner

```bash
# download latest binary
curl -fsSL https://github.com/xtool-org/mac-actions-runner/releases/download/continuous/tartscaleset-darwin-arm64.tar.gz \
| sudo tar -xz -C /usr/local/bin

# reload
tartscaleset register
```

## Configuration

Put custom configuration in `~/.config/tartscaleset/config.env`. See
[cmd/tartscaleset/config.go](/cmd/tartscaleset/config.go) for the list
of configuration options.

```dotenv
RUNNER_MAX_COUNT=4
TART_MEMORY_MB=4096
```

When running in the foreground, `tartscaleset` also respects configuration
from the process environment.

# More details

(The copy from hereon out is unreviewed / slop)

The controller downloads pinned Tart and Softnet releases into
`~/.config/tartscaleset/tools`;
no system or Homebrew installation is required. On first run, it also downloads
an Ubuntu ARM64 Tart image and provisions a reusable local base VM. The image has
a minimum size of 20 GB and is expanded to 50 GB by default. Its embedded image
version is recorded in `~/.config/tartscaleset`; bumping `baseImageVersion`
rebuilds an outdated base VM the next time `tartscaleset` starts.

Disposable guests have Ubuntu's automatic apt timers disabled: an unattended
upgrade can restart the Tart guest agent and terminate its runner. Rebuild the
base VM to pick up guest package updates. The controller probes each runner and
replaces its VM if the runner process disappears.

`tartscaleset` starts a GitHub Actions runner scale-set listener backed by Tart. It
keeps one clean runner waiting by default, generates a JIT configuration for
each runner, and deletes its disposable VM when the job completes. Set
`RUNNER_MIN_COUNT` and `RUNNER_MAX_COUNT` to change capacity.
The controller adapts GitHub's
[`dockerscaleset`](https://github.com/actions/scaleset/tree/main/examples/dockerscaleset)
example by replacing its Docker provider with Tart. It uses the public-preview
`github.com/actions/scaleset` Go module, pinned in `go.mod`.

The controller is built with `CGO_ENABLED=0`, and both scripts streamed into the
Linux guests are compiled into the executable with `go:embed`. The resulting
binary in `.tools/bin/tartscaleset` does not need the repository's scripts or Go
toolchain at runtime; it only needs its configuration and referenced host files.

To produce a distributable Apple Silicon binary:

```bash
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 \
  go build -trimpath -ldflags='-s -w' -o tartscaleset ./cmd/tartscaleset
```

Copy only `tartscaleset` to the destination Mac; the repository is not needed.
Run it from any directory. The first invocation performs the one-time network
setup if needed:

```bash
export APP_PRIVATE_KEY_FILE=/secure/path/private-key.pem
./tartscaleset
```

The binary uses `~/.config/tartscaleset` for state by default. Override the root
with `RUNNER_STATE_DIR`; `TART_BIN` and `SOFTNET_BIN` can still select externally
managed executables.

Press Ctrl+C to stop the listener. Shutdown deletes its runner VMs, message
session, and scale set. After an unclean host shutdown, the next start cleans
VMs recorded by the previous controller before accepting work. Per-VM Tart
output is discarded by default; set `TART_VM_LOGS=true` to write
`xtool-runner-*.vm.log` under `~/.config/tartscaleset/logs`. Controller and
runner output still goes to standard output and error.

Configuration comes from `config.env`, loaded with
[`godotenv`](https://github.com/joho/godotenv), and process environment overrides.
[`caarlos0/env`](https://github.com/caarlos0/env) applies the built-in defaults.
Export only the foreground overrides you need:

```bash
export RUNNER_MAX_COUNT=4
export XCODE_APP_PATH=/Applications/Xcode-beta.app
tartscaleset
```

Target it from a workflow with:

```yaml
jobs:
  build:
    runs-on: xtool-runner
    steps:
      - uses: actions/checkout@v6
      - run: uname -a
```

Docker and the Docker Compose v2 plugin are installed inside the Linux guest.
Workflow `docker` and `docker compose` commands talk to that disposable guest
daemon; we don't need or use Docker from the macOS host.

## Xcode share

Each runner receives a read-only VirtioFS mount of the host's Xcode bundle at
`/usr/local/share/Xcode.app`. The default `XCODE_APP_PATH=auto` derives the host path
from `xcode-select -p`. Override it through the environment when needed:

```bash
XCODE_APP_PATH=/Applications/Xcode-beta.app
```

Set `XCODE_APP_PATH=none` to disable the share. The Linux runner can read Xcode's
SDKs and other resources, but it cannot execute the bundle's macOS Mach-O
binaries.

## Network isolation

Softnet networking is the default and blocks guest access to private IPv4
networks. The controller pins Softnet and verifies its SHA-256 checksum.
Standard startup configures its narrowly scoped host privileges when needed. It
can also be done separately:

```bash
tartscaleset setup
```

This prompts for your macOS administrator password to make only the pinned
Softnet executable root-owned with its setuid bit enabled. Continue to run
`tartscaleset` as your normal user. The disposable VM boundary does not depend on
Softnet. Set `TART_NETWORK_MODE=shared` to opt out of Softnet
filtering, or use `TART_SOFTNET_ALLOW` to allow specific private CIDRs.
