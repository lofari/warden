# Warden

Secure sandbox CLI for running AI coding agents in isolated Docker containers.

## Install

```sh
go install github.com/lofari/warden/cmd/warden@latest
```

## Quick Start

```sh
# Run a command in a sandboxed container
warden run -- echo "hello from the sandbox"

# Run with network access
warden run --network -- npm install

# Run with a timeout
warden run --timeout 30m -- python train.py

# Dry-run to see the generated docker command
warden run --dry-run -- bash
```

## Configuration

Generate a starter config:

```sh
warden init
```

This creates `.warden.yaml` in the current directory:

```yaml
default:
  image: ubuntu:24.04
  tools: []
  mounts:
    - path: .
      mode: rw
  network: false
  memory: 8g
```

### Profiles

Define named profiles that extend the default:

```yaml
default:
  image: ubuntu:24.04
  network: false
  memory: 8g

profiles:
  web:
    tools: [node]
    network: true
  ml:
    tools: [python]
    memory: 16g
    timeout: 2h
```

```sh
warden run --profile web -- npm start
```

## Features

- **Same-path mounts** -- host paths map to identical container paths so agents see real file locations
- **Network isolation** -- networking disabled by default, opt-in per run or profile
- **Dev tool installation** -- built-in feature scripts for node, python, go, rust, java
- **Graceful timeout** -- SIGTERM with 10s grace period before SIGKILL
- **Signal forwarding** -- Ctrl+C forwarded to container, double-press force-kills
- **TTY detection** -- interactive mode when attached to a terminal
- **Exit code passthrough** -- container exit codes propagated to the host

## CLI Reference

```
warden run [flags] -- <command...>

Flags:
  --mount path:mode   Mount host path (mode: ro or rw)
  --network           Enable networking
  --no-network        Disable networking
  --timeout duration  Max execution time (e.g. 30m, 2h)
  --memory size       Memory limit (e.g. 4g)
  --cpus n            CPU limit
  --tools list        Dev tools to install (comma-separated: node,python,go,rust,java)
  --image name        Base image (default: ubuntu:24.04)
  --profile name      Profile from .warden.yaml
  --workdir path      Working directory inside container
  --dry-run           Print docker command without executing

warden init           Generate a starter .warden.yaml
warden images         List cached warden images
warden images prune   Remove cached warden images
```
