# Warden Design

A secure sandbox CLI for running AI coding agents in isolated Docker containers.

## Problem

AI coding agents (Claude Code, Codex, Aider, etc.) run with full access to the host machine. Warden wraps any command in a sandboxed container so the agent only sees what you allow.

## CLI Interface

```
warden run [OPTIONS] -- <command...>
warden init
warden images [prune]
```

### Flags for `warden run`

| Flag | Default | Description |
|---|---|---|
| `--mount <path>:<mode>` | none | Mount host path (ro or rw). Repeatable. |
| `--network` / `--no-network` | off | Enable/disable networking |
| `--timeout <duration>` | none | Max execution time (e.g. 30m, 2h) |
| `--memory <limit>` | 8g | Memory cap |
| `--cpus <n>` | host count | CPU limit |
| `--tools <tool,...>` | none | Dev tools to install (node, python, go, rust, java) |
| `--image <name>` | ubuntu:24.04 | Base image |
| `--profile <name>` | default | Profile from .warden.yaml |
| `--workdir <path>` | first rw mount | Working directory inside container |
| `--dry-run` | false | Print docker command without executing |

## Architecture

Shell out to the `docker` CLI. Warden translates config into `docker run` arguments. No Docker SDK, no daemon, no background processes.

### Why not Docker Engine API?

More code, manual TTY stream multiplexing, API versioning complexity. Shelling out to `docker run` gives us everything we need with dramatically less code.

### Why not Dev Containers?

Dev containers are designed for long-lived development environments. Warden needs ephemeral fire-and-forget runs. The Node.js dependency and slower startup (build + create + start vs just run) aren't worth it. We steal the good idea (feature scripts for tool installation) without the spec.

## Profile System

`.warden.yaml` in the project root:

```yaml
default:
  image: ubuntu:24.04
  tools: [node, python]
  mounts:
    - path: .
      mode: rw
  network: false
  timeout: 1h
  memory: 8g
  cpus: 4

profiles:
  untrusted:
    extends: default
    network: false
    mounts:
      - path: .
        mode: ro
    timeout: 30m

  web:
    extends: default
    network: true
    tools: [node, python, go]
```

### Merging order

built-in defaults < `.warden.yaml` default profile < named profile (via extends) < CLI flags.

### No config behavior

If no `.warden.yaml` and no flags: mount cwd as rw, no network, ubuntu:24.04, no tools.

## Image Building & Tool Features

When tools are requested, Warden builds and caches images.

1. Compute image tag from base + sorted tools: `warden:ubuntu-24.04_go_node`
2. If image exists locally, use it. If not, build it.
3. Generate a Dockerfile, run `docker build`, tag it.

### Generated Dockerfile

```dockerfile
FROM ubuntu:24.04
RUN apt-get update && apt-get install -y curl git
COPY features/ /tmp/warden-features/
RUN /tmp/warden-features/node.sh
RUN /tmp/warden-features/go.sh
RUN rm -rf /tmp/warden-features/ /var/lib/apt/lists/*
```

### Feature scripts

Simple shell scripts bundled with the binary via `go:embed`. Each installs one tool. Built-in features for v1: node, python, go, rust, java.

### Image management

- `warden images` — list cached warden images with sizes
- `warden images prune` — remove all cached warden images
- Images are normal Docker images, manageable with `docker` directly

## Container Execution

Example: `warden run --mount ./project:rw --no-network --tools node -- claude -p "do the thing"`

Translates to:

```bash
docker run \
  --rm -it \
  -v /home/user/project:/home/user/project:rw \
  --network none \
  --memory 8g --cpus 4 \
  -w /home/user/project \
  warden:ubuntu-24.04_node \
  claude -p "do the thing"
```

### Key behaviors

- **Mount mapping:** host paths map to the same absolute path inside the container. The agent sees real paths, not /workspace.
- **TTY:** allocate PTY when stdin is a terminal. Pass through signals.
- **User mapping:** run as host UID/GID. No permission issues on mounted files.
- **Timeout:** watchdog sends SIGTERM, waits 10s, then SIGKILL.
- **Exit codes:** 0 = success, non-zero = agent's exit code, 124 = timeout.
- **Cleanup:** `--rm` ensures container removal. No accumulation.
- **Ctrl+C:** first SIGINT forwarded, second force-kills.

## Error Handling

| Condition | Behavior |
|---|---|
| Docker not installed | `"warden: docker is not installed"` |
| Docker daemon not running | `"warden: docker daemon is not running"` |
| Image build failure | Show build output, exit with error |
| Mount path doesn't exist | Error before starting: `"warden: mount path ./foo does not exist"` |
| OOM kill | Exit 137, `"warden: killed (out of memory, limit was 8g)"` |
| Timeout | Exit 124, `"warden: killed (timeout after 1h)"` |
| Dry run | Print docker command and exit |

No retry logic. No daemon mode. Warden runs, container runs, both exit.

## Project Structure

```
warden/
├── cmd/warden/main.go
├── internal/
│   ├── cli/root.go
│   ├── config/profile.go
│   ├── config/defaults.go
│   ├── container/run.go
│   ├── container/image.go
│   ├── container/timeout.go
│   └── features/features.go
├── features/
│   ├── node.sh
│   ├── python.sh
│   ├── go.sh
│   ├── rust.sh
│   └── java.sh
├── go.mod
└── go.sum
```

### Dependencies

- `cobra` — CLI framework
- `yaml.v3` — YAML parsing
- Standard library for everything else (os/exec, embed, filepath)

## Testing

**Unit tests** (no Docker required):
- Config parsing and merging
- Image tag computation
- Docker argument generation
- Mount path resolution
- Timeout exit code handling

**Integration tests** (require Docker, build tag `integration`):
- Full run flow with real containers
- Mount verification, network isolation, exit code propagation
- Image build and cache behavior

```bash
go test ./...                          # unit tests
go test -tags integration ./...        # unit + integration
```

## Golem Integration

External. Golem calls `warden run ...` to wrap its iteration commands. No coupling — Warden has no knowledge of golem, state.yaml, or iterations. Golem is responsible for translating its locked paths into `--mount` flags.

## Future (not v1)

- Domain-based network allowlisting
- `--allow-domain` flag (reserved, not implemented)
- GPU passthrough
- Container reuse across runs (warm containers)
- Custom feature scripts from user config

## Decisions

| Decision | Rationale |
|---|---|
| Go, not Kotlin | Consistent with golem, native binaries trivially, simpler toolchain |
| Shell out to docker CLI, not Engine API | Dramatically simpler, predictable, debuggable |
| Feature scripts, not devcontainers | Ephemeral runs don't fit devcontainer model, avoids Node.js dep |
| Same-path mounts, not /workspace | Agent sees real paths, sandbox is invisible |
| On/off network, not domain filtering | Simple v1, domain filtering can be added without API changes |
| .warden.yaml in project root | Convention over configuration, no central config needed |
