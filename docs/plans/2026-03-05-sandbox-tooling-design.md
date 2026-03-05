# Sandbox Tooling Design: Agent-Ready Base Images

**Date:** 2026-03-05
**Status:** Approved

## Problem

Warden sandboxes start from bare `ubuntu:24.04`. AI coding agents (Claude Code primarily) hit immediate friction: no `ripgrep`, no build tools, no ecosystem package managers. Agents waste time installing basics on every run.

## Constraints

- Primary consumer: Claude Code (other agents secondary)
- Container init time must stay under ~10 seconds (cached images)
- First-build time acceptable (30-60s), subsequent runs instant via Docker cache
- Image building always has network; `--network` flag controls runtime only
- Version pinning: major version channels (e.g., Node 22.x, Go 1.23.x)
- Only Debian/Ubuntu base images supported for v1

## Approach: Warden Base Image Layer

Always build and use a `warden:base-<image>` image instead of raw ubuntu. Feature scripts layer on top.

### Image flow

```
no tools  →  warden:base-ubuntu-24.04  (built once, cached)
tools     →  warden:base-ubuntu-24.04 + feature scripts  (layered on top)
```

### Previous flow (replaced)

```
no tools  →  raw ubuntu:24.04
tools     →  ubuntu:24.04 + curl,git,ca-certs + feature scripts
```

## Base Image Contents

Installed in every sandbox regardless of tool selection:

```
# Network & download
curl, wget, ca-certificates, openssh-client

# Version control
git

# Search & navigation (Claude Code essentials)
ripgrep, fd-find, tree, less

# Build toolchain (needed by npm/pip native extensions)
build-essential, pkg-config

# Data processing
jq

# Archive tools
unzip, zip, tar, gzip

# System
sudo, locales (UTF-8 configured)
```

Not included (YAGNI): vim/nano, man pages, docker-in-docker, databases, GUI tools.

Estimated size: ~350MB.

## Enhanced Feature Scripts

### node.sh
- nodejs 22.x (via nodesource)
- yarn, pnpm (package managers)
- tsx, typescript (TS execution/compilation)

### python.sh
- python3, python3-pip, python3-venv, python3-dev (dev headers for C extensions)
- uv (fast pip replacement from Astral)
- symlink python -> python3

### go.sh
- go 1.23.x (pinned minor, latest patch)
- gopls (language server)
- PATH includes /root/go/bin

### rust.sh
- stable toolchain via rustup
- cargo env sourced in profile

### java.sh
- openjdk-21-jdk-headless
- maven
- gradle 8.x

### claude.sh
- node (if not already present)
- @anthropic-ai/claude-code (unchanged)

## Implementation Changes

### image.go

- New `BaseImageTag(base string) string` — returns `warden:base-<safe-name>`
- New `BuildBaseImage(base string) (string, error)` — builds base if not cached
- `BuildImage` changes `FROM` line to use base image instead of raw ubuntu
- Inline `apt-get install curl git ca-certificates` removed from tool Dockerfile

### run.go

- Before running, always call `BuildBaseImage` to ensure base exists
- Replace "only build if tools requested" with "always ensure base, optionally layer tools"

### Dockerfile generation

Base image:
```dockerfile
FROM ubuntu:24.04
RUN apt-get update && apt-get install -y --no-install-recommends \
    curl wget ca-certificates openssh-client \
    git ripgrep fd-find tree less \
    build-essential pkg-config \
    jq unzip zip tar gzip \
    sudo locales \
  && sed -i 's/# en_US.UTF-8/en_US.UTF-8/' /etc/locale.gen \
  && locale-gen \
  && rm -rf /var/lib/apt/lists/*
```

Tool image:
```dockerfile
FROM warden:base-ubuntu-24.04
COPY features/ /tmp/warden-features/
RUN /tmp/warden-features/node.sh
RUN rm -rf /tmp/warden-features/
```

## Error Handling

- **First run**: prints `warden: building base image (first run only)...`
- **No auto-invalidation**: users run `warden images prune` to force rebuild
- **Non-debian base**: print warning, fail gracefully
- **Offline + no cache**: fail with `warden: base image not found, network required for first build`

## Existing Commands

`warden images` and `warden images prune` already operate on `warden:` prefix — base images are automatically covered.
