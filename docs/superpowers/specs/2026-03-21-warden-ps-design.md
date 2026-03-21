# `warden ps` — List Running Sandboxes

## Goal

Add a `warden ps` command that lists all running warden sandboxes across both Docker and Firecracker runtimes, with resource usage (CPU%, memory), and supports both human-readable table and JSON output for programmatic consumption by saga.

## Architecture

A new `ListRunning() ([]RunningInstance, error)` method on the `Runtime` interface. Each runtime implements discovery differently:

- **Docker**: `docker ps --filter name=warden- --format ...` for instance list, `docker stats --no-stream --format ...` for resource usage.
- **Firecracker**: reads `~/.warden/firecracker/running.json` state file, validates PIDs are alive via `kill(pid, 0)`, reads RSS from `/proc/<pid>/statm` for memory.

The CLI command `warden ps` iterates all registered runtimes, collects instances, and renders as a table (default) or JSON (`--json` flag).

## Data Model

```go
// RunningInstance describes a running warden sandbox.
type RunningInstance struct {
    Name    string    // container/VM name (e.g. "warden-a1b2c3d4")
    Runtime string    // "docker" or "firecracker"
    Command string    // the command being executed (first arg, e.g. "bash")
    Started time.Time // when the sandbox started
    CPU     float64   // CPU usage percentage (-1 if unavailable)
    Memory  int64     // memory usage in bytes (-1 if unavailable)
}
```

CPU and Memory use `-1` as the sentinel for "unavailable" to distinguish from "actually zero". Table rendering shows `—` for `-1`, JSON renders `-1`.

This type lives in `internal/runtime/runtime.go` alongside the existing `ImageInfo` type.

## Runtime Interface Change

Add to the `Runtime` interface:

```go
// ListRunning returns currently running sandboxes for this runtime.
ListRunning() ([]RunningInstance, error)
```

Both `DockerRuntime` and `FirecrackerRuntime` must implement this simultaneously (interface change). If the runtime is not available (e.g. Docker not installed), `ListRunning()` should return `nil, nil` — not an error. Errors are reserved for unexpected failures (e.g. Docker is installed but `docker ps` returns garbage).

## Docker Implementation

In `internal/runtime/docker/docker.go`:

1. **List containers**: `docker ps --filter name=warden- --no-trunc --format '{{.Names}}\t{{.Command}}\t{{.CreatedAt}}'`
   - Parse `CreatedAt` using Go layout `"2006-01-02 15:04:05 -0700 MST"` (Docker's default format)
   - If `exec.LookPath("docker")` fails, return `nil, nil`
   - `Command` is the first word from the command column

2. **Get stats**: `docker stats --no-stream --no-trunc --format '{{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}'` with a 3-second context timeout
   - Parse CPU: strip `%` suffix, parse as float64
   - Parse Memory: take the left side of ` / ` split, parse value and unit suffix (`B`, `KiB`, `MiB`, `GiB`, `kB`, `MB`, `GB`). Handle both IEC (MiB) and SI (MB) units.

3. **Merge**: match by container name. If stats timed out or failed, CPU and Memory stay at `-1`.

## Firecracker Implementation

### VM Naming

Generate a name for each Firecracker VM using the same pattern as Docker: `"warden-fc-" + hex(random 4 bytes)`. Add a `name` field to `vmInstance` in `vm.go`. Generate the name in `startVM()` before boot.

### State File: `~/.warden/firecracker/running.json`

An array of entries, read/written under flock (same pattern as `net-alloc`):

```json
[
  {
    "name": "warden-fc-a1b2c3d4",
    "pid": 12345,
    "command": "bash",
    "started": "2026-03-21T10:00:00Z"
  }
]
```

- `pid` is `vm.cmd.Process.Pid` — the **firecracker child process PID**, not the parent warden process PID. This is the process whose resource usage we measure.
- `command` is `command[0]` — just the executable name, not the full argument list.
- `started` is `time.Now()` captured just before `vm.boot()` returns.

### Registration

In `startVM()` (in `vm.go`), after `vm.boot()` returns successfully, register an entry in `running.json` under flock. If registration fails (e.g. disk full), log a warning to stderr and continue — this is non-fatal.

In `cleanup()`, remove the entry from `running.json` under flock. Double-removal is safe: if the entry is already gone (reaped by a concurrent `warden ps`), this is a no-op.

### Discovery (`ListRunning`)

In `internal/runtime/firecracker/firecracker.go`:

1. Check if `running.json` exists. If not, return `nil, nil`.
2. Read `running.json` under flock.
3. For each entry, check if PID is alive via `syscall.Kill(pid, 0)`.
4. Dead entries are reaped (rewrite file without them, still under same flock).
5. For alive entries:
   - Read `/proc/<pid>/statm` for RSS memory: field index 1 (resident pages) * page size (4096).
   - CPU%: compute as **average CPU since start** — read utime+stime from `/proc/<pid>/stat` (fields 14+15 in clock ticks), divide by elapsed wall time since `started`. This avoids needing two-sample measurement.
6. Return `[]RunningInstance`.

## CLI Command

### File: `internal/cli/ps.go`

New file. Must import both `_ "github.com/winler/warden/internal/runtime/docker"` and `_ "github.com/winler/warden/internal/runtime/firecracker"` (following `root.go` pattern, not `images.go` which only imports docker).

### Usage

```
warden ps [--json]
```

### Table Output (default)

```
NAME                RUNTIME      COMMAND    CPU%    MEMORY    UPTIME
warden-a1b2c3d4     docker       bash       2.3%    128 MiB   5m32s
warden-fc-e5f6a7    firecracker  claude     8.1%    512 MiB   12m15s
```

- Uptime is computed from `Started` field as a human-readable duration
- Memory is formatted as MiB/GiB
- If CPU/memory is `-1`, show `—`

### JSON Output (`--json`)

```json
[
  {
    "name": "warden-a1b2c3d4",
    "runtime": "docker",
    "command": "bash",
    "cpu": 2.3,
    "memory": 134217728,
    "started": "2026-03-21T10:00:00Z",
    "uptime": "5m32s"
  }
]
```

- Includes both `started` (raw timestamp) and `uptime` (human-readable duration) for programmatic consumers.
- CPU/memory of `-1` means unavailable.
- Empty result prints `[]`.

### No Results

Table mode prints: `No running warden sandboxes.`

## Error Handling

- **Runtime not available** (Docker not installed, no /dev/kvm): `ListRunning()` returns `nil, nil` — the CLI skips it silently. No `Preflight()` call needed.
- **Stale Firecracker entries**: dead PIDs are reaped from `running.json` on every read. This is a write side-effect of a read operation, protected by flock. Concurrent `warden ps` calls will serialize on the flock briefly.
- **Docker stats timeout**: 3-second context deadline. If exceeded, CPU/memory stay at `-1`.
- **Concurrent access to running.json**: protected by flock, same pattern as `net-alloc`. Double-removal by cleanup + reap is safe (entry simply not found).
- **Registration failure in startVM**: non-fatal, log warning, continue execution.

## Testing

- **Unit**: Firecracker state file read/write/reap (dead PID removal)
- **Unit**: Table and JSON output formatting from `[]RunningInstance` (including `-1` sentinel rendering)
- **Unit**: Docker `ps` and `stats` output parsing (including various memory unit formats)
- **Unit**: Average CPU% computation from `/proc/stat` fields
- **Integration** (build tag): launch a Docker container via `warden run`, verify `warden ps` includes it
