# Warden

Secure sandbox CLI for running AI coding agents in isolated environments. Supports three runtimes: Docker containers, Docker Desktop Sandboxes (microVMs), and Firecracker microVMs.

## Install

```sh
go install github.com/lofari/warden/cmd/warden@latest
```

## Quick Start

```sh
# Drop into a shell
warden shell

# Run a command
warden exec npm test

# Run with full control
warden run --network --timeout 30m -- python train.py

# Check sandbox status
warden info
```

## Runtimes

Warden auto-detects the best available runtime:

| Runtime | Isolation | Requirements |
|---------|-----------|-------------|
| **Firecracker** | Separate kernel (microVM) | Linux, `/dev/kvm`, `warden setup` |
| **Docker Sandbox** | Hypervisor-backed microVM | Docker Desktop with sandbox support |
| **Docker** | Container (cgroups/namespaces) | Docker Engine |

Override with `--runtime`:

```sh
warden shell --runtime sandbox
warden exec --runtime docker npm test
```

### Docker Sandbox

Sandboxes are persistent by default — installed packages and config survive between runs. Use `--ephemeral` for one-shot execution.

```sh
warden shell                    # creates or reuses sandbox for this workspace
warden exec pip install pandas  # packages persist
warden exec python train.py    # pandas still installed

warden stop                     # stop sandbox
warden rm                       # remove sandbox
warden rm -a                    # remove all warden sandboxes
```

### Firecracker

Requires one-time setup:

```sh
warden setup
```

This downloads the kernel, Firecracker binary, and builds virtiofsd.

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

### Runtime and Ephemeral

```yaml
runtime: sandbox
ephemeral: false
```

## Features

- **Three runtimes** -- Docker containers, Docker Desktop Sandboxes, Firecracker microVMs
- **Auto-detection** -- picks the strongest available isolation automatically
- **Same-path mounts** -- host paths map to identical container paths so agents see real file locations
- **Network isolation** -- networking disabled by default, opt-in per run or profile
- **Dev tool installation** -- built-in feature scripts for node, python, go, rust, java
- **Auth broker** -- secure Claude API credential proxying without exposing keys to the sandbox
- **Command proxy** -- forward host commands (git, npm, etc.) into the sandbox
- **Persistent sandboxes** -- Docker Sandbox and Firecracker VMs persist between runs
- **Graceful timeout** -- SIGTERM with 10s grace period before SIGKILL
- **Signal forwarding** -- Ctrl+C forwarded to sandbox, double-press force-kills
- **TTY detection** -- interactive mode when attached to a terminal
- **Exit code passthrough** -- sandbox exit codes propagated to the host

## CLI Reference

### Running commands

```
warden shell                      Drop into an interactive bash session
warden exec <command> [args...]   Run a command (no -- needed)
warden run [flags] -- <command>   Run with full flag control
```

### Sandbox lifecycle

```
warden info [--json]              Show sandbox status for current workspace
warden ps [--json]                List all running warden sandboxes
warden stop [name]                Stop a sandbox (derives name from cwd if omitted)
warden rm [name] [-a]             Remove a sandbox (-a removes all)
```

### Image management

```
warden init                       Generate a starter .warden.yaml
warden images                     List cached warden images
warden images prune               Remove cached warden images
warden setup                      Set up Firecracker runtime (download kernel, binaries)
```

### Flags for run, shell, exec

```
--runtime string    Runtime backend (docker, sandbox, or firecracker)
--mount path:mode   Mount host path (mode: ro or rw)
--network           Enable networking
--no-network        Disable networking
--timeout duration  Max execution time (e.g. 30m, 2h)
--memory size       Memory limit (e.g. 4g)
--cpus n            CPU limit
--tools list        Dev tools (comma-separated: node,python,go,rust,java)
--image name        Base image (default: ubuntu:24.04)
--profile name      Profile from .warden.yaml
--workdir path      Working directory inside sandbox
--env KEY=VALUE     Environment variable (repeatable)
--proxy command     Proxy a host command into sandbox (repeatable)
--auth-broker       Enable auth broker for Claude API proxying
--ephemeral         Remove sandbox after execution (sandbox runtime only)
--display           Enable virtual display (Firecracker only)
--dry-run           Print command without executing (run only)
```
