# Firecracker MicroVM Runtime Support

**Date:** 2026-03-11
**Status:** Draft
**Author:** Brainstorming session

## Problem Statement

Warden currently uses Docker containers as its sole isolation mechanism. This has two limitations:

1. **Security isolation** — Docker containers share the host kernel. A compromised agent could attempt container escapes via kernel exploits. For untrusted workloads, a stronger isolation boundary is needed.
2. **Environment constraints** — Some environments cannot run Docker (already inside a container, corporate policy restrictions, minimal server setups). These environments need an alternative runtime.

## Goals

- Provide Firecracker microVM as an alternative runtime alongside Docker
- Maintain the same user-facing `SandboxConfig` model — switching runtimes should be a one-field change
- Reuse existing feature scripts and configuration system
- Keep Docker as the default, zero breaking changes for existing users

## Non-Goals

- Replacing Docker — it remains the default and recommended runtime for most users
- Supporting other hypervisors (Cloud Hypervisor, QEMU) in this iteration
- Docker-free rootfs building (v1 uses Docker to assemble Firecracker rootfs images)
- Domain-level network filtering (on/off only, same as current Docker behavior)

## Architecture Overview

### Runtime Abstraction Layer

A `Runtime` interface abstracts the execution backend. Both Docker and Firecracker implement it. The rest of Warden (config parsing, CLI, feature scripts) remains unchanged.

```
SandboxConfig
     |
     v
Runtime interface
  |-- DockerRuntime       (existing logic, extracted into struct)
  |-- FirecrackerRuntime  (new)
       |-- manages kernel + rootfs images
       |-- translates mounts -> virtiofs shares
       |-- translates network on/off -> TAP + NAT or no-network
       |-- manages VM lifecycle via Firecracker API socket
```

### Runtime Interface

```go
// ImageInfo describes a cached image or rootfs.
type ImageInfo struct {
    Tag       string    // e.g. "warden:ubuntu-24.04_node" or "ubuntu-24.04_node.ext4"
    Size      int64     // bytes
    Runtime   string    // "docker" or "firecracker"
    CreatedAt time.Time
}

type Runtime interface {
    // Preflight checks if the runtime is available and ready.
    Preflight() error

    // EnsureImage ensures the image/rootfs exists, building if needed.
    // Returns an image identifier (Docker tag or rootfs path).
    EnsureImage(config SandboxConfig) (string, error)

    // Run executes a command in the sandbox.
    // Returns exit code and error. Error is non-nil for infrastructure failures
    // (failed to start, image build error, etc.). Exit code is meaningful only
    // when error is nil.
    Run(config SandboxConfig, command []string) (int, error)

    // DryRun prints what would be executed without running it.
    // For Docker: prints the docker run command.
    // For Firecracker: prints the VM configuration as structured JSON.
    DryRun(config SandboxConfig, command []string) error

    // ListImages returns cached images for this runtime.
    ListImages() ([]ImageInfo, error)

    // PruneImages removes all cached images for this runtime.
    PruneImages() error
}
```

**Call sequence from the CLI:**

1. `runtime := NewRuntime(config.Runtime)` — factory selects DockerRuntime or FirecrackerRuntime
2. `runtime.Preflight()` — verify the runtime is available
3. If `--dry-run`: call `runtime.DryRun(config, command)` and exit
4. `runtime.EnsureImage(config)` — build/cache images as needed
5. `runtime.Run(config, command)` — execute and return exit code

The CLI (`root.go`) owns this sequence. `Run()` does not call `EnsureImage()` internally — the CLI calls them in order. This keeps image building observable (progress messages) and testable independently.

### Runtime Selection

Resolution order (highest to lowest priority):

1. CLI flag: `--runtime docker|firecracker`
2. `.warden.yaml` profile field: `runtime: firecracker`
3. Default: `docker`

If `runtime: firecracker` is set but Firecracker is not configured, Warden exits with:
```
warden: firecracker runtime is not set up. Run "warden setup firecracker" first.
```

## Firecracker Runtime — VM Lifecycle

Each `warden run` with the Firecracker runtime follows this lifecycle:

1. **Preflight** — verify `/dev/kvm` access, Firecracker binary exists, virtiofsd available
2. **Start virtiofsd** — one instance per virtiofs mount (project directories), exposes host directories to the VM
3. **Configure VM** via Firecracker's HTTP API socket:
   - Attach kernel binary (`vmlinux`)
   - Attach rootfs as a block device (read-only base + writable overlay)
   - Attach virtiofs tags for user directory mounts
   - Configure vCPUs and memory from `SandboxConfig`
   - Configure network: no interface (`network: false`) or TAP device (`network: true`)
4. **Boot VM** — ~125ms to userspace
5. **Execute command** inside VM via guest init agent over vsock
6. **Signal forwarding** — SIGINT/SIGTERM forwarded to guest process via vsock
7. **Timeout** — watchdog sends shutdown command, grace period, then force-kills the VM process
8. **Cleanup** — stop VM, tear down TAP device (if created), stop virtiofsd instances, remove writable overlay

### Filesystem Mounts (virtiofs)

Each mount from `SandboxConfig` is exposed to the VM via virtiofs and mounted **at the same absolute path inside the guest as on the host**. This preserves Warden's same-path mount convention (matching the Docker path, where `-v /host/path:/host/path:mode` is used). This means `cfg.Workdir` — which is set to the first rw mount's host path — remains valid inside the guest without translation.

The guest init agent is responsible for creating the mount point directories and mounting each virtiofs tag at the correct path during boot initialization.

## Guest Init Agent

A lightweight Go binary that runs as PID 1 inside the microVM.

### Responsibilities

- Boot initialization: mount `/proc`, `/sys`, `/dev`, create mount points, mount virtiofs shares at their corresponding host paths, configure networking if TAP device is present
- Listen on vsock for commands from the host
- Spawn the requested command with correct working directory, environment variables, and UID/GID
- Stream stdout/stderr back to host over vsock
- Forward signals received from host to the child process
- Report exit code back to host on completion
- Shut down the VM cleanly after the command exits

### Communication Protocol (vsock)

All messages use a length-prefixed framing format over the vsock stream:

```
[4 bytes: little-endian uint32 payload length][JSON payload]
```

Each JSON payload has a `type` field as discriminator:

**Host to Guest:**
```json
{"type": "exec", "command": "node", "args": ["index.js"], "workdir": "/home/user/project", "env": ["NODE_ENV=dev"], "uid": 1000, "gid": 1000, "tty": true}
```
```json
{"type": "signal", "signal": "SIGINT"}
```

**Guest to Host:**
```json
{"type": "stdout", "data": "<base64-encoded bytes>"}
```
```json
{"type": "stderr", "data": "<base64-encoded bytes>"}
```
```json
{"type": "exit", "code": 0}
```

When `tty: true`, stdout and stderr are merged into a single PTY stream. The guest init allocates a PTY for the child process and sends all output as `stdout` type messages. The host writes stdin bytes to the vsock as raw (unframed) data on a separate vsock port to avoid framing overhead on interactive input.

### TTY Support

When the host detects a terminal (stdin is a TTY), it sets `tty: true` in the exec message. The guest init allocates a PTY for the child process and forwards the raw byte stream over vsock. This provides the same interactive experience as Docker's `-it` flags.

### Implementation

- Written in Go, compiled as a static binary (`CGO_ENABLED=0`)
- Bundled into every rootfs during image build
- Located at `/usr/local/bin/warden-init` inside the guest
- Source: `cmd/warden-init/` (separate build target)

## Image and Rootfs Management

### Storage Layout

```
~/.warden/firecracker/
|-- kernel/
|   |-- vmlinux-5.10.217              # default kernel (auto-downloaded on first use)
|   |-- vmlinux-5.10.217.sha256       # checksum file
|-- rootfs/
|   |-- base-ubuntu-24.04.ext4        # base rootfs
|   |-- ubuntu-24.04_node.ext4        # base + node tools
|   |-- ubuntu-24.04_node_python.ext4 # base + node + python tools
|-- overlays/                          # ephemeral per-run writable layers (cleaned up after run)
|-- bin/
|   |-- firecracker                    # Firecracker binary
|   |-- virtiofsd                      # virtiofsd binary
|-- net-alloc                          # IP allocation counter file (file-locked)
```

### Building a Rootfs

1. Start a temporary Docker container from the base image (e.g., `ubuntu:24.04`)
2. Run `BaseDockerfile` steps (install curl, git, ripgrep, build-essential, etc.)
3. Run requested feature scripts (`node.sh`, `python.sh`, etc.) — same scripts used by Docker path
4. Bundle the guest init agent binary into the filesystem
5. Export the container filesystem to an ext4 image
6. Cache the image at `~/.warden/firecracker/rootfs/`

### Rootfs Naming

Rootfs filenames are derived from Docker tag names by stripping the `warden:` prefix and appending `.ext4`. Examples:

| Docker tag | Rootfs filename |
|---|---|
| `warden:base-ubuntu-24.04` | `base-ubuntu-24.04.ext4` |
| `warden:ubuntu-24.04_node` | `ubuntu-24.04_node.ext4` |
| `warden:ubuntu-24.04_node_python` | `ubuntu-24.04_node_python.ext4` |

The same deterministic naming logic (base image + sorted tool list) is used. A shared `ImageTag()` function computes the canonical name, and each runtime maps it to its storage format.

### Kernel Management

- **Default:** Warden downloads kernel `vmlinux-5.10.217` from the Firecracker GitHub releases on first use (`https://github.com/firecracker-microvm/firecracker/releases/download/v1.x.x/vmlinux-5.10.217`). The binary's SHA-256 checksum is embedded in the Warden source and verified after download. A first-run progress message matches the existing `BuildBaseImage` UX.
- **Custom kernel:** Users can override via global config at `~/.warden/config.yaml`:
  ```yaml
  firecracker:
    kernel: /path/to/custom/vmlinux
  ```
- Kernel config is machine-level (global config), not project-level (`.warden.yaml`)
- If checksum verification fails, Warden exits with an error and removes the partial download

### Image Commands

`warden images` lists both Docker and Firecracker images, grouped by runtime. It calls `ListImages()` on each registered runtime and combines the output.

`warden images prune` removes cached images for both runtimes, or `--runtime firecracker` to prune selectively.

## Networking

### network: false (default)

No TAP device attached to the VM. The guest has zero network interfaces — stronger isolation than Docker's `--network none` (which still creates a loopback and network namespace).

### network: true

1. Host creates a TAP device named `warden-fc-{short-uuid}` (unique per VM for concurrent runs)
2. TAP device attached to the VM's virtio-net configuration
3. IP pair allocated from `172.16.0.0/12` range using a sequential counter with file locking (see IP Allocation below)
4. Host configures the TAP endpoint (gateway IP) and NAT:
   - Enable IP forwarding
   - iptables/nftables MASQUERADE rule on the host's outbound interface
5. Guest init configures the interface with the allocated guest IP
6. On VM exit: tear down TAP device, remove iptables rules, release IP allocation

### IP Allocation

To support concurrent VMs, IP addresses are allocated from `/30` subnets within `172.16.0.0/12`:

- A counter file at `~/.warden/firecracker/net-alloc` tracks the next available subnet index
- File locking (`flock`) prevents concurrent allocation races
- Each VM gets a `/30` subnet: index N maps to `172.16.N*4.0/30` (gateway `.1`, guest `.2`)
- On VM cleanup, the allocation is released
- The counter wraps at the range limit (~262K subnets available, more than sufficient)

### TAP Device Naming

`warden-fc-{8-char-uuid}` — avoids collisions when multiple VMs run concurrently.

## Privilege Model

Firecracker requires `/dev/kvm` access and TAP device management requires `CAP_NET_ADMIN`. Rather than running Warden as root, the privilege model uses targeted capabilities.

### `warden setup firecracker`

A one-time setup command that requires `sudo`:

1. Adds the current user to the `kvm` group (for `/dev/kvm` access)
2. Downloads the Firecracker binary to `~/.warden/firecracker/bin/firecracker`
3. Downloads virtiofsd to `~/.warden/firecracker/bin/virtiofsd`
4. Builds `warden-netsetup` from `cmd/warden-netsetup/` and installs it to `/usr/local/bin/warden-netsetup` with `CAP_NET_ADMIN` set via `setcap cap_net_admin+ep`. Installation to `/usr/local/bin/` (rather than a user-writable path) is required because `setcap` on a user-writable binary is a security risk — the owner could replace the binary.
5. Enables `net.ipv4.ip_forward` in sysctl (with user confirmation)
6. Runs a verification check (can access `/dev/kvm`, can create a TAP device, can start a minimal VM)

### `warden-netsetup` Helper

Source: `cmd/warden-netsetup/main.go`

A minimal binary that accepts exactly three subcommands via CLI args (no stdin, no network input):

```
warden-netsetup create --tap <name> --host-ip <ip/mask> --guest-ip <ip/mask> --outbound-iface <iface>
warden-netsetup destroy --tap <name>
warden-netsetup verify
```

- `create`: creates TAP device, assigns host IP, adds iptables MASQUERADE rule
- `destroy`: removes TAP device and associated iptables rule
- `verify`: checks that capabilities are set correctly

The binary validates all inputs strictly (interface name format, IP ranges within `172.16.0.0/12`). Warden invokes it via `exec.Command("/usr/local/bin/warden-netsetup", ...)`.

### Runtime Privilege Separation

- **`warden` binary** — runs unprivileged, always
- **`warden-netsetup` helper** — has `CAP_NET_ADMIN` only, invoked by Warden only when `network: true` with the Firecracker runtime. Handles TAP create/destroy and iptables rules.

This keeps the attack surface minimal. The main CLI never runs with elevated privileges.

## Configuration Changes

### `SandboxConfig` Struct Update

```go
// In config/types.go
type SandboxConfig struct {
    Runtime string   // "docker" or "firecracker" (default: "docker")
    Image   string
    Tools   []string
    Mounts  []Mount
    Network bool
    Memory  string
    CPUs    int
    Timeout string
    Workdir string
    Env     []string
}
```

### `ProfileEntry` Update

```go
// In config/parse.go — follows existing pointer pattern for optional fields
type ProfileEntry struct {
    Runtime *string  // new field
    Image   *string
    Tools   []string
    // ... existing fields
}
```

`ApplyProfile` gains a nil-check for `Runtime`, matching the existing pattern for other optional fields:
```go
if p.Runtime != nil {
    cfg.Runtime = *p.Runtime
}
```

`DefaultConfig()` sets `Runtime: "docker"`.

### `.warden.yaml` Additions

```yaml
default:
  runtime: docker            # new field: docker | firecracker
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
  secure:
    extends: default
    runtime: firecracker

  untrusted:
    extends: secure
    network: false
    mounts:
      - path: .
        mode: ro
```

### `warden init` Template Update

The `initTemplate` constant in `init.go` gains the `runtime` field:
```yaml
runtime: docker  # or: firecracker
```

### Global Config (`~/.warden/config.yaml`)

Machine-level settings not tied to a project:

```yaml
firecracker:
  kernel: /path/to/custom/vmlinux    # optional, overrides auto-downloaded default
```

### New CLI Flags

- `--runtime docker|firecracker` — override runtime for this invocation

### New Commands

- `warden setup firecracker` — one-time Firecracker environment setup

## Package Structure

### Refactoring

Current `container/` package logic is extracted into runtime-specific implementations:

```
cmd/
|-- warden/main.go                # existing entrypoint (unchanged)
|-- warden-init/main.go           # guest init agent (new, separate build target)
|-- warden-netsetup/main.go       # network helper (new, setcap'd binary)

internal/
|-- cli/
|   |-- root.go                   # updated: runtime selection, --runtime flag, DryRun dispatch
|   |-- init.go                   # updated: initTemplate gains runtime field
|   |-- images.go                 # updated: iterates all runtimes, grouped output
|   |-- setup.go                  # new: warden setup firecracker command
|-- config/
|   |-- types.go                  # updated: Runtime field added to SandboxConfig
|   |-- defaults.go               # updated: DefaultConfig() sets Runtime: "docker"
|   |-- parse.go                  # updated: ProfileEntry gains Runtime *string
|   |-- merge.go                  # updated: ApplyProfile handles Runtime nil-check
|-- runtime/
|   |-- runtime.go                # Runtime interface, ImageInfo struct, NewRuntime() factory
|   |-- mounts.go                 # ResolveMounts() — shared by both runtimes
|   |-- docker/
|   |   |-- docker.go             # DockerRuntime struct, Preflight(), Run(), DryRun()
|   |   |-- image.go              # EnsureImage(), ImageExists(), BuildImage(), BuildBaseImage()
|   |   |-- args.go               # buildArgs() (private to package)
|   |-- firecracker/
|   |   |-- firecracker.go        # FirecrackerRuntime struct, Preflight(), Run(), DryRun()
|   |   |-- image.go              # EnsureImage(), BuildRootfs()
|   |   |-- vm.go                 # VM lifecycle: configure, boot, execute, cleanup
|   |   |-- network.go            # TAP setup/teardown via warden-netsetup, IP allocation
|   |   |-- kernel.go             # Kernel download, checksum verification, path resolution
|   |   |-- virtiofs.go           # virtiofsd process management
|   |-- shared/
|       |-- signals.go            # Signal forwarding (used by both runtimes)
|       |-- timeout.go            # Timeout watchdog (used by both runtimes)
|       |-- tty.go                # TTY detection (used by both runtimes)
|       |-- exitcode.go           # Exit code translation
|-- features/                     # unchanged
|-- guest/
|   |-- exec.go                   # Command execution inside VM
|   |-- vsock.go                  # vsock listener and protocol handling
|   |-- net.go                    # Guest-side network configuration
```

### What Moves Where

| Current location | New location | Notes |
|---|---|---|
| `container/run.go` Run() | `runtime/docker/docker.go` DockerRuntime.Run() | Core logic preserved |
| `container/run.go` RunConfig.DryRun | `runtime/docker/docker.go` DockerRuntime.DryRun() | Separate method on interface |
| `container/image.go` BuildImage() | `runtime/docker/image.go` | Unchanged |
| `container/base.go` BuildBaseImage() | `runtime/docker/image.go` | Unchanged |
| `container/args.go` BuildDockerArgs() | `runtime/docker/args.go` buildArgs() | Made private to package |
| `container/args.go` ResolveMounts() | `runtime/mounts.go` | Shared by both runtimes |
| `container/timeout.go` | `runtime/shared/timeout.go` | Used by both runtimes |
| `container/docker.go` CheckDockerReady() | `runtime/docker/docker.go` Preflight() | Renamed to match interface |

### Shared Utilities

Signal forwarding, timeout watchdog, TTY detection, exit code translation, and mount resolution are extracted into `runtime/shared/` and `runtime/mounts.go`, used by both `DockerRuntime` and `FirecrackerRuntime`. No duplication.

### Feature Scripts

Unchanged. The same embedded shell scripts are used by both runtimes — Docker runs them during `docker build`, Firecracker runs them inside a temporary container during rootfs assembly.

## Error Handling

| Condition | Behavior |
|---|---|
| `/dev/kvm` not accessible | Exit 1: `"warden: /dev/kvm not accessible. Run 'warden setup firecracker'"` |
| Firecracker binary missing | Exit 1: `"warden: firecracker not found. Run 'warden setup firecracker'"` |
| virtiofsd missing | Exit 1: `"warden: virtiofsd not found. Run 'warden setup firecracker'"` |
| Kernel not found | Exit 1: `"warden: kernel not found at <path>"` |
| Kernel checksum mismatch | Exit 1: `"warden: kernel checksum verification failed"` |
| VM boot failure | Exit 1: show Firecracker stderr |
| Guest init not responding | Exit 1: `"warden: guest agent not responding (vsock timeout)"` |
| TAP creation fails | Exit 1: `"warden: failed to create network interface. Check warden-netsetup capabilities"` |
| IP allocation exhausted | Exit 1: `"warden: no available network addresses"` |
| OOM kill (VM) | Exit 137: `"warden: killed (out of memory, limit was 8g)"` |
| Timeout | Exit 124: `"warden: killed (timeout after 1h)"` |
| SIGINT (first) | Forward to guest process via vsock |
| SIGINT (second) | Force-kill the Firecracker process |

## Testing Strategy

### Unit Tests (no KVM required)

- Runtime interface compliance (both implementations satisfy the interface)
- Firecracker VM configuration generation
- TAP device naming and IP allocation logic
- Rootfs tag computation (mirrors existing Docker tag tests)
- Guest init protocol serialization/deserialization (framing, all message types)
- Config parsing with `runtime` field
- Mount translation to virtiofs share configs
- `warden-netsetup` argument validation
- IP allocation counter wraparound and edge cases

### Integration Tests (require `/dev/kvm`)

- Full VM boot and command execution
- virtiofs mount verification (file changes visible both directions)
- Network isolation (no network = no connectivity)
- Network enabled (can reach external hosts)
- Signal forwarding (SIGINT kills guest process)
- Timeout behavior
- Exit code propagation
- TTY passthrough
- Concurrent VMs with independent network allocations

Integration tests gated behind a `//go:build firecracker` build tag, separate from existing Docker integration tests.

## Security Considerations

### Isolation Improvement Over Docker

- **Kernel boundary:** Each microVM runs its own kernel. A guest kernel exploit does not compromise the host — it only affects the minimal Firecracker VMM.
- **Reduced attack surface:** Firecracker exposes a minimal device model (virtio-net, virtio-block, virtio-vsock). No USB, no PCI passthrough, no graphics.
- **No shared kernel:** Unlike containers, there is no shared syscall interface. The guest kernel mediates all syscalls independently.
- **Seccomp on Firecracker:** The Firecracker process itself runs under a strict seccomp profile, limiting its host syscall surface to ~30 calls.

### Remaining Trust Boundaries

- **virtiofsd** runs on the host and serves filesystem requests from the guest. A malicious guest could attempt to exploit virtiofsd. Mitigation: virtiofsd is sandboxed and runs unprivileged.
- **warden-netsetup** has `CAP_NET_ADMIN`. Installed to `/usr/local/bin/` (not user-writable) to prevent binary replacement. Its scope is limited to TAP device and iptables rule management with strict input validation.
- **Kernel integrity** — the default kernel is verified against a SHA-256 checksum embedded in the Warden binary. Custom kernels are the user's responsibility.
- **Rootfs builds** use Docker. A compromised Docker daemon could produce a malicious rootfs. This is the same trust model as the current Docker-only path.

## Migration and Compatibility

- **Zero breaking changes** for existing users. Docker is the default runtime. No config migration needed.
- **Existing `.warden.yaml` files** work unchanged — the `runtime` field defaults to `docker` when absent.
- **CLI behavior** is identical unless `--runtime firecracker` is specified.
- **Feature scripts** are reused without modification.
- **`RunConfig` is removed** — its fields are absorbed into `SandboxConfig` (config fields) and the `Runtime` interface methods (`DryRun` becomes a separate method). The CLI layer handles `--dry-run` by calling `runtime.DryRun()` instead of `runtime.Run()`.
