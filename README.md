# xtool Tart runner

Spins up isolated Linux GitHub Actions runners on a macOS host, for xtool.

(NB: very vibe coded)

## Prerequisites

- An Apple Silicon Mac running macOS 13 or newer
- Xcode installed

## Setup

### Install GitHub App

1. Create a GitHub App on the target organization with organization self-hosted runner read/write permission
2. Save the private key to `./runner-private-key.pem`
3. Protect the App private key: `chmod 600 runner-private-key.pem`

### Set up networking (requires sudo)

Run `./scripts/setup-softnet.sh` to configure VM network isolation.

> [!TIP]
> If you don't have `sudo` access, you can set `TART_NETWORK_MODE=shared` in `runner.env`.

## Run

```bash
./run.sh
```

# More details

(The copy from hereon out is unreviewed / slop)

On first run, we download an Ubuntu ARM64 Tart image and provision a reusable
local base VM. The image has a minimum size of 20 GB and is expanded to 50 GB by
default.

`run.sh` keeps one clean runner waiting for a job. After the job, it stops and
deletes the VM, then clones another one from the base.

Press Ctrl+C to stop the loop.

Target it from a workflow with:

```yaml
jobs:
  build:
    runs-on: [self-hosted, linux, ARM64, xtool-runner]
    steps:
      - uses: actions/checkout@v6
      - run: uname -a
```

Docker is installed inside the Linux guest. Workflow Docker commands talk to
that disposable guest daemon; we don't need or use Docker from the macOS host.

## Xcode share

Each runner receives a read-only VirtioFS mount of the host's Xcode bundle at
`/usr/share/Xcode.app`. The default `XCODE_APP_PATH=auto` derives the host path
from `xcode-select -p`. Override it in `runner.env` when needed:

```bash
XCODE_APP_PATH=/Applications/Xcode-beta.app
```

Set `XCODE_APP_PATH=` to disable the share. The Linux runner can read Xcode's
SDKs and other resources, but it cannot execute the bundle's macOS Mach-O
binaries.

## Network isolation

Softnet networking is the default and blocks guest access to private IPv4
networks. The scripts pin openai/softnet and verify its published SHA-256
checksum. Configure its narrowly scoped host privileges once:

```bash
./scripts/setup-softnet.sh
```

This prompts for your macOS administrator password to make only the pinned
Softnet executable root-owned with its setuid bit enabled. Continue to run
`./run.sh` as your normal user. The disposable VM boundary does not depend on
Softnet. Set `TART_NETWORK_MODE=shared` in `runner.env` to opt out of Softnet
filtering, or use `TART_SOFTNET_ALLOW` to allow specific private CIDRs.
