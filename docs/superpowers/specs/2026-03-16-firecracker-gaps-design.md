# Firecracker Runtime: Closing Implementation Gaps

**Date:** 2026-03-16
**Status:** Draft
**Author:** Brainstorming session
**Builds on:** [2026-03-11-microvm-support-design.md](2026-03-11-microvm-support-design.md)

## Problem Statement

The Firecracker runtime infrastructure is ~70% complete — VM boot, networking, rootfs building, and virtiofs mounts all work. But `warden run --runtime firecracker` cannot execute commands because the vsock communication layer between host and guest is unimplemented. Several secondary gaps (kernel checksum, memory parsing, automated setup, IP reclamation, global config) also remain.

## Goals

- Make `warden run --runtime firecracker -- <cmd>` execute a command inside the microVM and return its output + exit code
- Pin to real Firecracker v1.15.0 and kernel 6.1.155 with verified checksums
- Automate `warden setup firecracker` to download/build all dependencies
- Parse memory config strings into MiB for VM configuration
- Reclaim IP allocations from dead VMs using PID-based stale detection
- Add global config file (`~/.warden/config.yaml`) for kernel path override

## Non-Goals

- Stdin forwarding or TTY/interactive mode (future work, separate vsock port)
- aarch64 support (x86_64 only for now)
- Multiple concurrent commands per VM
- Domain-level network filtering

## Design

### 1. vsock Communication Protocol

The existing `internal/protocol` package defines the message types. This design uses them over a single vsock connection on port 1024.

#### Message Flow

```
Host                                    Guest (warden-init)
  |                                         |
  |  [VM boots, guest starts listening]     |
  |                                         |
  |--- connect to CID:1024 --------------->|
  |                                         |
  |--- ExecMessage ----------------------->|
  |    {command, args, workdir, env}        |
  |                                         | [spawns command]
  |                                         |
  |<--- OutputMessage {stdout, data} ------|
  |<--- OutputMessage {stderr, data} ------|
  |<--- OutputMessage {stdout, data} ------|
  |    ...interleaved...                    |
  |                                         |
  |--- SignalMessage {SIGTERM} ----------->| [on timeout or Ctrl+C]
  |                                         | [forwards to process]
  |                                         |
  |<--- ExitMessage {code: 0} ------------|
  |                                         |
  | [cleanup VM]                            | [init exits, VM halts]
```

#### Write Synchronization

Multiple goroutines write to the vsock connection (stdout stream, stderr stream, exit message). The protocol uses length-prefixed framing, so individual messages are atomic at the wire level, but concurrent `Write()` calls on the same `net.Conn` can interleave bytes. A `sync.Mutex` guards all writes to the connection on the guest side.

### 2. Guest Init Agent (warden-init)

**File:** `cmd/warden-init/main.go`

The guest init agent is PID 1 inside the microVM. It mounts filesystems (already implemented), then enters the vsock event loop.

#### Lifecycle

1. Mount `/proc`, `/sys`, `/dev` (existing)
2. Open vsock listener on port 1024 using `github.com/mdlayher/vsock`
3. Accept one connection from host
4. Read `ExecMessage` — extract command, args, workdir, env
5. Configure environment: set `$PATH`, `$HOME`, apply env from message
6. Spawn command via `os/exec` with stdout/stderr pipes
7. Start goroutines for output streaming and signal handling
8. Wait for command to exit
9. Send `ExitMessage` with exit code
10. Close connection, exit (VM halts when init exits)

#### Goroutine Model

```
main goroutine:   accept → read ExecMessage → cmd.Start() → cmd.Wait() → send ExitMessage → os.Exit()
stdout goroutine: read cmd.Stdout in 4KB chunks → write OutputMessage{type:"stdout"} → repeat until EOF
stderr goroutine: read cmd.Stderr in 4KB chunks → write OutputMessage{type:"stderr"} → repeat until EOF
signal goroutine: read conn for SignalMessage → syscall.Kill(cmd.Process.Pid, sig) → repeat
```

The signal goroutine reads from the vsock connection concurrently with the main goroutine's initial `ReadMessage` for `ExecMessage`. After `ExecMessage` is received, the signal goroutine takes over reading. This is safe because the protocol is sequential: host sends `ExecMessage` first, then only `SignalMessage` afterwards.

#### Output Chunk Size

Read stdout/stderr in 4KB chunks. Each chunk becomes one `OutputMessage`. This balances latency (small enough for responsive output) with overhead (not one message per byte).

#### Error Handling

- If `ExecMessage` specifies a command that doesn't exist: send `ExitMessage{code: 127}` (standard "command not found")
- If vsock accept fails: log to stderr, exit with code 1 (VM will be cleaned up by host timeout)
- If command is killed by signal: send `ExitMessage{code: 128 + signal_number}`

### 3. Host-side vsock Client

**File:** `internal/runtime/firecracker/firecracker.go` — `Run()` method

#### Lifecycle

1. `startVM()` (existing) — boots VM, returns `vmInstance`
2. Poll-connect to guest vsock: `vsock.Dial(guestCID, 1024)` every 10ms, timeout after 5s
3. Send `ExecMessage` with command, args, workdir, env from `SandboxConfig`
4. Enter read loop:
   - `OutputMessage{type:"stdout"}` → `os.Stdout.Write(data)`
   - `OutputMessage{type:"stderr"}` → `os.Stderr.Write(data)`
   - `ExitMessage` → capture exit code, break
5. `cleanup()` (existing) — stops VM, tears down networking
6. Return exit code

#### Goroutine Model

```
main goroutine:    connect → send ExecMessage → read loop (dispatch Output/Exit) → return exitCode
signal goroutine:  <-sigChan → write SignalMessage{sig} to conn
                   second signal → kill Firecracker process directly
timeout goroutine: <-ctx.Done() → write SignalMessage{SIGTERM} → sleep 10s → kill Firecracker process
```

#### Readiness Detection

The guest agent needs time to boot and start listening. The host polls with `vsock.Dial()`:

- Retry interval: 10ms
- Maximum wait: 5s
- On timeout: `cleanup()` + return error `"guest agent did not start within 5s"`

This is simpler than a readiness signal protocol and matches the VM boot time (~125ms typical).

#### Guest CID

The guest CID is assigned during `configureVM()`. Firecracker assigns CID 3 by default for the guest. The host uses this CID to dial. If multiple VMs run concurrently, each gets its own Firecracker process with its own vsock device — the CID is scoped to the VM, not global.

#### Signal Handling

Integrates with `shared.SignalHandler()` which returns a channel:

- First SIGINT/SIGTERM: send `SignalMessage` to guest via vsock. Guest forwards to the running process.
- Second signal: kill the Firecracker process directly (immediate VM death). This matches the Docker runtime's two-signal pattern.

#### Timeout Handling

Uses the same graceful pattern as the Docker runtime:

1. Context deadline fires
2. Send `SignalMessage{SIGTERM}` to guest
3. Start 10s grace timer
4. If grace timer expires before `ExitMessage` received: kill Firecracker process
5. Return exit code 124 (timeout)

### 4. vsock Dependency

**Package:** `github.com/mdlayher/vsock`

Provides `net.Listener` and `net.Conn` compatible types for `AF_VSOCK` sockets. Used by both guest (listen) and host (dial).

- Guest: `vsock.Listen(1024, nil)` → `listener.Accept()` → `net.Conn`
- Host: `vsock.Dial(cid, 1024, nil)` → `net.Conn`

The existing `protocol.WriteMessage()`/`ReadMessage()` work with `io.Writer`/`io.Reader`, so `net.Conn` plugs in directly.

### 5. Version Pinning and Checksums

#### Kernel

| Field | Old Value | New Value |
|-------|-----------|-----------|
| Version | 5.10.217 | 6.1.155 |
| URL | `spec.ccfc.min/.../vmlinux-5.10.217` | `https://s3.amazonaws.com/spec.ccfc.min/firecracker-ci/v1.15/x86_64/vmlinux-6.1.155` |
| SHA256 | `placeholder-sha256-checksum` | `e20e46d0c36c55c0d1014eb20576171b3f3d922260d9f792017aeff53af3d4f2` |

Rationale: 5.10 guest kernel support ends 2024-01-31 (already EOL). 6.1 is supported until 2026-09-02 and is the recommended version for Firecracker v1.15.

#### Firecracker Binary

| Field | Value |
|-------|-------|
| Version | v1.15.0 |
| URL pattern | `https://github.com/firecracker-microvm/firecracker/releases/download/v1.15.0/firecracker-v1.15.0-x86_64.tgz` |
| SHA256 (tarball) | `00cadf7f21e709e939dc0c8d16e2d2ce7b975a62bec6c50f74b421cc8ab3cab4` |
| Binary inside tarball | `release-v1.15.0-x86_64/firecracker-v1.15.0-x86_64` |

#### virtiofsd

| Field | Value |
|-------|-------|
| Version | v1.13.3 |
| Source | `https://gitlab.com/virtio-fs/virtiofsd/-/archive/v1.13.3/virtiofsd-v1.13.3.tar.gz` |
| Build method | Compile inside Docker container with Rust toolchain |

No prebuilt binary is published for virtiofsd. The setup command builds it from source inside a Docker container (Docker is already a required dependency for rootfs building).

### 6. Automated `warden setup firecracker`

**File:** `internal/cli/setup.go`

Replaces the current manual instructions with actual download and install.

#### Flow

1. **Check prerequisites**
   - `/dev/kvm` exists and is readable
   - Docker is available (needed for virtiofsd build and rootfs building)

2. **Create directory structure**
   ```
   ~/.warden/firecracker/
   ├── kernel/
   ├── rootfs/
   ├── bin/
   └── net-alloc
   ```

3. **Download kernel**
   - Fetch `vmlinux-6.1.155` from S3
   - Verify SHA256
   - Place at `~/.warden/firecracker/kernel/vmlinux-6.1.155`

4. **Download Firecracker**
   - Fetch tarball from GitHub releases
   - Verify SHA256
   - Extract `firecracker` binary to `~/.warden/firecracker/bin/firecracker`
   - `chmod +x`

5. **Build virtiofsd**
   - Download source tarball from GitLab
   - Build inside Docker container: `docker run --rm -v ... rust:latest cargo build --release`
   - Copy binary to `~/.warden/firecracker/bin/virtiofsd`
   - `chmod +x`

6. **Build and install warden-netsetup**
   - `go build -o ~/.warden/firecracker/bin/warden-netsetup ./cmd/warden-netsetup`
   - Print instruction: `sudo setcap cap_net_admin+ep ~/.warden/firecracker/bin/warden-netsetup`

7. **System configuration prompts**
   - Check `net.ipv4.ip_forward` — if disabled, print: `sudo sysctl -w net.ipv4.ip_forward=1`
   - Check `setcap` on warden-netsetup — if missing, print the command

8. **Verification**
   - Run `Preflight()` to confirm everything works
   - Print success summary

#### Privilege Model

The setup command runs unprivileged. Steps requiring root privileges print the exact command for the user to run manually. No automatic `sudo`.

#### Idempotency

Each step checks whether the artifact exists with the correct checksum before downloading. Re-running `warden setup firecracker` skips completed steps and prints "already installed" messages.

### 7. Memory Parsing

**File:** `internal/runtime/firecracker/vm.go`

Replace `mem := 1024` with `parseMemoryMiB(cfg.Memory)`.

#### Parsing Rules

| Input | Output (MiB) |
|-------|-------------|
| `""` (empty/unset) | 1024 (default) |
| `"512m"` or `"512M"` | 512 |
| `"2g"` or `"2G"` | 2048 |
| `"1024"` (bare number) | 1024 (assume MiB) |
| `"0"` or negative | Error |
| `"abc"` | Error |

Function signature: `func parseMemoryMiB(s string) (int, error)`

Error returned before VM boot so the user gets a clear message.

### 8. IP Reclamation with PID-based Stale Detection

**File:** `internal/runtime/firecracker/network.go`

Replace the monotonic counter with a PID-tracked allocation file.

#### Allocation File Format

File: `~/.warden/firecracker/net-alloc`

```
0:12345
3:12389
7:12401
```

Each line is `subnetIndex:PID`. The file is locked during read-modify-write (same `flock` pattern as the current counter).

#### Allocate Flow

1. Acquire file lock on `net-alloc`
2. Read file, parse `index:PID` entries
3. For each entry, check if PID is alive: `syscall.Kill(pid, 0)`
   - If error (process gone): mark index as reclaimable
4. If reclaimable indices exist: take the lowest one
5. Otherwise: take `max(existing indices) + 1` (or 0 if file is empty)
6. Write our `index:PID` entry, remove any reclaimed entries
7. Release lock
8. Return gateway IP, guest IP, release function

#### Release Function

Called by `vmInstance.cleanup()`:

1. Acquire file lock
2. Read file, remove entry matching `os.Getpid()`
3. Write file
4. Release lock

#### Edge Cases

- **PID reuse:** The window between VM exit and PID reuse by another warden process is negligible. Worst case: a leaked IP reclaimed on the next allocation scan.
- **Crash without cleanup:** Handled — next allocation scans for dead PIDs.
- **Empty file:** First allocation gets index 0.
- **Concurrent allocations:** File lock serializes access. Each allocation is atomic.

### 9. Global Config File

**File:** `internal/config/global.go`

Add support for `~/.warden/config.yaml`.

#### Format

```yaml
firecracker:
  kernel: /path/to/custom/vmlinux
```

#### Implementation

```go
type GlobalConfig struct {
    Firecracker FirecrackerGlobalConfig `yaml:"firecracker"`
}

type FirecrackerGlobalConfig struct {
    Kernel string `yaml:"kernel"`
}

func LoadGlobalConfig() (GlobalConfig, error)
```

- If `~/.warden/config.yaml` doesn't exist, return zero-value struct (no error)
- If it exists but is malformed, return error
- Wire into `resolveKernelPath()`: pass `globalCfg.Firecracker.Kernel` as the `customPath` parameter

The struct is extensible — future settings (virtiofsd path, default memory, etc.) can be added without changing the file format or existing consumers.

## File Changes Summary

| File | Change |
|------|--------|
| `cmd/warden-init/main.go` | Replace `select{}` with vsock event loop |
| `internal/runtime/firecracker/firecracker.go` | Implement `Run()` with vsock client |
| `internal/runtime/firecracker/kernel.go` | Update version, URL, checksum constants |
| `internal/runtime/firecracker/vm.go` | Add `parseMemoryMiB()`, wire into `configureVM()` |
| `internal/runtime/firecracker/network.go` | Replace counter with PID-tracked allocation |
| `internal/cli/setup.go` | Replace manual instructions with download/build flow |
| `internal/config/global.go` | New file: `LoadGlobalConfig()` |
| `go.mod` | Add `github.com/mdlayher/vsock` dependency |

## Testing Strategy

| Component | Test Type | Notes |
|-----------|-----------|-------|
| Guest agent | Unit test | Mock vsock connection with `net.Pipe()`, verify message exchange |
| Host vsock client | Unit test | Same `net.Pipe()` approach, verify ExecMessage sent and Output/Exit handled |
| Memory parsing | Unit test | Table-driven: all input formats, edge cases, errors |
| IP reclamation | Unit test | Create allocation file, simulate dead PIDs, verify reclamation |
| Global config | Unit test | Parse valid/missing/malformed YAML |
| Kernel constants | Unit test | Verify URL format, checksum length |
| Setup command | Integration test | Behind build tag, requires Docker |
| Full VM lifecycle | Integration test | Behind build tag, requires `/dev/kvm` |

Unit tests use `net.Pipe()` to simulate vsock connections — no actual VM needed. The protocol package already uses `io.Reader`/`io.Writer`, so this works transparently.

## Error Handling

| Scenario | Behavior |
|----------|----------|
| Guest agent doesn't start within 5s | Cleanup VM, return error |
| Command not found in guest | `ExitMessage{code: 127}` |
| vsock connection drops mid-execution | Host detects read error, kills Firecracker, returns exit code 1 |
| Timeout | Send SIGTERM via vsock, 10s grace, then kill Firecracker, return code 124 |
| Second Ctrl+C | Kill Firecracker process directly |
| Memory parse error | Error before VM boot with descriptive message |
| Kernel download checksum mismatch | Error, remove partial download |
| Setup: Docker not available | Error with message to install Docker first |

## Security Considerations

- **vsock is VM-local.** No network exposure. The vsock connection exists only between the host and its own guest VM, scoped by the Firecracker process.
- **Guest agent runs as root inside the VM.** This is expected — PID 1 (init) must be root. The VM boundary is the security boundary, not user separation within the guest.
- **virtiofsd built from source.** The setup command builds from a pinned release tag, not `HEAD`. The source tarball URL is deterministic.
- **Checksum verification on all downloads.** Kernel, Firecracker binary, and virtiofsd source tarball are all verified against hardcoded SHA256 checksums.
- **No automatic sudo.** Setup prints privileged commands for the user to review and run manually.
