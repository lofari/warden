# Command Proxy — Design Spec

Run designated commands on the host while the sandbox handles everything else. Auth credentials never enter the sandbox.

## Problem

AI coding assistants (Claude CLI, Cursor) need API keys and session tokens to function. Running them inside a sandbox requires either (a) mounting credentials into the sandbox, where malicious code can steal them, or (b) re-authenticating inside the sandbox, which is friction-heavy and still exposes credentials.

Neither option is acceptable. The sandbox should protect credentials completely.

## Solution

Proxied commands execute on the host with full auth. The sandbox gets a shim binary that relays stdio through a transport. From inside the sandbox, the command appears to run normally — same args, same stdin/stdout, same exit codes — but the real process and its credentials stay on the host.

## Decisions

- **Transport:** runtime-specific. Unix socket for Docker, vsock for Firecracker. Matches existing warden patterns.
- **Shim delivery:** mount at runtime (not baked into images). Follows `warden-init` precedent.
- **Lifecycle:** sandbox exits when proxied command exits. Standard `warden run` behavior.
- **Config:** explicit `proxy` list in `.warden.yaml`. No magic detection.
- **Default runtime:** Firecracker, with auto-fallback to Docker if `/dev/kvm` unavailable.

## Configuration

New `proxy` field in `.warden.yaml` profile:

```yaml
default:
  runtime: firecracker
  proxy:
    - claude
  tools: [node, go]
  network: true

profiles:
  ai-dev:
    proxy: [claude, cursor]
    tools: [node, python]
```

CLI override for one-off use:

```bash
warden run --proxy claude -- saga plan
```

The `proxy` field is a string array. Each entry names a command to run on the host. Multiple commands can be proxied in the same session.

### Config Struct Change

Add to `SandboxConfig` in `internal/config/types.go`:

```go
Proxy []string `yaml:"proxy"`
```

This follows the same pattern as `Tools []string`, `Env []string`, etc.

## Architecture

### Startup Flow

1. `warden run -- saga plan` starts
2. For each command in `proxy` list:
   - Resolve the real binary path on host via `exec.LookPath`
   - Allocate transport (Unix socket or vsock port)
   - Start proxy listener on host
3. Start sandbox with shim binaries on PATH
4. Sandbox runs the user's command (`saga plan`)
5. When code inside the sandbox invokes a proxied command (e.g., `claude`), it finds the shim

### Shim → Host Flow

```
SANDBOX                           HOST

saga plan
  └─ spawns "claude --args"
       └─ shim binary
            │
            ├─ connect transport ──── proxy listener
            ├─ send header ────────── { command, args, tty, env }
            ├─ relay stdin ─────────► real claude process
            ├─ relay stdout ◄──────── (with host auth, ~/.claude/)
            └─ receive exit code ◄─── process exits
            └─ shim exits(code)
```

### Multiple Invocations

The proxy listener accepts multiple sequential connections. If `saga plan` spawns Claude, Claude exits, and saga spawns Claude again (e.g., handoff), the second invocation gets a fresh connection and a fresh host-side process. The listener stays alive for the duration of the sandbox.

## Transport Layer

### Docker: Unix Socket

Host creates a temporary directory with one socket per proxied command:

```
/tmp/warden-proxy-<id>/
  claude.sock
  cursor.sock
```

Mounted read-only into the container:

```
-v /tmp/warden-proxy-<id>:/run/warden-proxy:ro
```

The shim connects to `/run/warden-proxy/<command>.sock`.

The socket directory is created with mode `0700` and cleaned up when the container exits.

### Firecracker: Vsock

Each proxied command gets a vsock port, starting at 3000 (after display port 2048):

| Port | Use |
|------|-----|
| 1024 | Guest init (existing) |
| 1025+ | FUSE mounts (existing, allocated as `1025 + mountIndex`) |
| 2048 | VNC display (existing) |
| 3000+ | Command proxy (new, allocated as `3000 + proxyIndex`) |

FUSE mount ports are bounded by the number of configured mounts (typically 1-3). The 3000 base provides a safe gap of ~1975 unused ports. Proxy ports are allocated as `3000 + j` where `j` is the index into the proxy list.

Port assignments are sent to the guest via a new protocol message before command execution.

## Protocol Messages (Firecracker)

New message types in `internal/protocol/`:

```go
// Host → Guest: configure proxied commands
type ProxyConfigMessage struct {
    Proxies []ProxyEntry `json:"proxies"`
}

type ProxyEntry struct {
    Command string `json:"command"`
    Port    uint32 `json:"port"`
}
```

Sent after `MountConfigMessage`, before `ExecMessage`. The guest init agent installs shim binaries at `/usr/local/bin/<command>` for each entry.

### Shim-to-Host Wire Protocol

Once the shim connects (Unix socket or vsock), it sends a JSON header followed by raw stdio:

```go
// Shim → Host: initial handshake
type ProxyHandshake struct {
    Command string   `json:"command"`
    Args    []string `json:"args"`
    TTY     bool     `json:"tty"`
    Env     []string `json:"env,omitempty"` // only sandbox-specific env to merge
}

// Host → Shim: ready acknowledgment
type ProxyReady struct {
    OK    bool   `json:"ok"`
    Error string `json:"error,omitempty"`
}

// Host → Shim: process exited
type ProxyExit struct {
    Code int `json:"code"`
}
```

### Wire Format After Handshake

After the handshake, the connection carries length-prefixed frames:

```
[1 byte: type] [4 bytes: length (little-endian)] [payload]
```

Frame types:
- `0x01` — stdin (shim → host)
- `0x02` — stdout (host → shim)
- `0x03` — stderr (host → shim)
- `0x04` — exit code (host → shim): payload is 4-byte little-endian int32
- `0x05` — resize (shim → host): payload is JSON `{"cols": N, "rows": N}`
- `0x06` — signal (shim → host): payload is 4-byte signal number

This framing lets the host send stdout data and exit code on the same connection without ambiguity. The shim reads frames in a loop, dispatching stdout/stderr to the appropriate file descriptor, and exits when it receives an exit frame.

For TTY mode: the host spawns the real command with a PTY. The shim sets its own terminal to raw mode. Stdout and stderr merge into a single stream (frame type `0x02`), matching normal PTY behavior. Window size changes are forwarded via resize frames.

## Shim Binary

Source: `cmd/warden-shim/main.go`

Compiled as a static binary (CGO_ENABLED=0) for portability across container images.

### Behavior

1. Determine transport:
   - Check `/run/warden-proxy/<basename>.sock` (Docker path)
   - If not found, read vsock port from `/run/warden-proxy/<basename>.port` (Firecracker — written by guest init)
2. Connect to transport
3. Send `ProxyHandshake` with command name, args, TTY detection, and any sandbox-specific environment variables
4. Wait for `ProxyReady`
5. If TTY: set terminal to raw mode
6. Relay stdin → transport, transport → stdout/stderr
7. On transport close, read `ProxyExit`, exit with that code

### Build and Installation

The shim is built alongside warden's other binaries. The `Makefile` (or `go build` commands) produces three binaries:

```bash
go build -o bin/warden ./cmd/warden/
go build -o bin/warden-init ./cmd/warden-init/          # existing
CGO_ENABLED=0 go build -o bin/warden-shim ./cmd/warden-shim/  # new, static
```

The shim must be statically compiled (`CGO_ENABLED=0`) since it runs inside containers with potentially different glibc versions.

`warden setup` (or first `warden run`) installs the shim to `~/.warden/bin/warden-shim`, alongside the existing Firecracker binaries. The main `warden` binary checks for the shim at startup when `proxy` is configured and prints an error if missing.

### Delivery

**Docker:** At `docker run` time, one bind-mount per proxied command, each pointing to the same shim binary:

```
-v ~/.warden/bin/warden-shim:/usr/local/bin/claude:ro
-v ~/.warden/bin/warden-shim:/usr/local/bin/cursor:ro
```

The shim detects which command it is via `os.Args[0]` (argv[0]).

**Firecracker:** The shim binary is copied into the rootfs during image build (same process that installs `warden-init`). The image build step in `internal/runtime/firecracker/image.go` adds the shim to `/usr/local/bin/warden-shim`. When the guest init agent receives `ProxyConfigMessage`, it creates symlinks from `/usr/local/bin/<command>` to `/usr/local/bin/warden-shim` for each proxied command.

## Host Proxy Handler

New package: `internal/proxy/`

```go
type Proxy struct {
    Command  string        // "claude"
    HostPath string        // resolved via exec.LookPath
    Listener net.Listener  // UDS or vsock listener
}

func (p *Proxy) Serve(ctx context.Context) error
func (p *Proxy) Close() error
```

### Connection Handling

Each connection from the shim:

1. Read `ProxyHandshake`
2. Resolve host binary path (already cached from startup)
3. Build `exec.Cmd` with:
   - The real binary path and shim-provided args
   - Host environment (full PATH, HOME, auth credentials)
   - Merged sandbox-specific env vars from handshake (if any)
   - Working directory: not set (inherits host cwd, which is the project dir)
4. If TTY: create PTY, attach to command
5. If not TTY: pipe stdin/stdout/stderr
6. Send `ProxyReady{OK: true}`
7. Relay stdio bidirectionally (goroutines with `io.Copy`)
8. On process exit: send `ProxyExit{Code: exitCode}`, close connection

### Signal Forwarding

The shim forwards signals from the sandbox side to the host-side process via signal frames (type `0x06`). When the shim receives SIGINT or SIGTERM, it sends the signal number to the host proxy handler, which delivers it to the real process via `syscall.Kill`. If the process has already exited (`syscall.Kill` returns `ESRCH`), the host handler ignores the error silently — the exit frame is already in flight or queued. This mirrors how the main Firecracker runtime handles signals in its `Run` loop.

### Concurrency

The listener accepts connections sequentially (one active proxied process at a time per command). If a second connection arrives while one is active, it queues until the first completes. This prevents credential races and matches how Claude CLI works (one session at a time).

**Known constraint:** saga's orchestrator mode can spawn multiple Claude instances for different personas. With the sequential model, these would serialize rather than run in parallel. This is acceptable for now — orchestrator dispatches agents sequentially in practice, and parallel agent execution through the proxy would require per-connection process isolation that adds significant complexity.

## Runtime Default Change

### New Default: Firecracker with Auto-Fallback

When no runtime is specified in config or CLI:

1. Check `/dev/kvm` exists and is accessible
2. Check Firecracker binary exists at `~/.warden/firecracker/bin/firecracker`
3. If both present: use Firecracker
4. If either missing: fall back to Docker, print warning to stderr:
   ```
   warden: firecracker unavailable, falling back to docker (less isolation)
   ```
5. If user explicitly sets `runtime: firecracker` (in config or CLI): fail hard, no fallback

### Implementation

`DefaultConfig()` in `internal/config/defaults.go` currently sets `Runtime: "docker"`. Change it to `Runtime: ""` (empty string), meaning "auto-detect."

In `internal/cli/root.go`, the runtime resolution logic changes from:

```go
rt, err := runtime.NewRuntime(cfg.Runtime) // was always "docker" from defaults
```

To:

```go
rt, err := runtime.ResolveRuntime(cfg.Runtime) // auto-detects if empty
```

New function `runtime.ResolveRuntime(preferred string) (Runtime, error)`:
- If `preferred` is non-empty: use it exactly, error if unavailable (no fallback)
- If empty: check `/dev/kvm` and Firecracker binary → use `firecracker` if both present, fall back to `docker` with stderr warning
- User config `runtime: docker` or `runtime: firecracker` always takes precedence (no auto-detection)

## What the Sandbox Never Sees

| Asset | Why |
|-------|-----|
| `~/.claude/` | API keys, session tokens, OAuth state |
| `~/.ssh/` | SSH private keys |
| `~/.gitconfig` | Git credential helpers, tokens |
| `~/.npmrc`, `~/.yarnrc` | Package registry tokens |
| `~/.docker/config.json` | Docker registry credentials |
| `~/.aws/`, `~/.gcloud/` | Cloud provider credentials |
| Host PATH binaries | Only proxied commands are accessible via shim |
| Host environment variables | Only explicitly declared `env:` entries |

The proxy socket itself is the only bridge. It carries stdio bytes, not credentials.

## Security Model

### What This Protects

- **Credential theft:** API keys, SSH keys, cloud credentials never enter the sandbox. A compromised dependency or supply chain attack cannot access them.
- **Host filesystem:** only explicitly mounted paths are visible. `~/.ssh`, `~/.claude/`, and other dotfiles are inaccessible.
- **Blast radius:** the worst case is damage to the mounted project files, not the entire development machine.

### What This Does Not Protect

1. **Prompt injection via project files.** If malicious content in the project manipulates the AI assistant, the assistant executes harmful MCP tool calls inside the sandbox. The proxy doesn't help — the AI controls the tools. The sandbox limits what those tools can do (can't access host filesystem), but within the sandbox, the AI has full access to project files.

2. **Project file exfiltration.** Project files are mounted by design. Code inside the sandbox can read all mounted files. If networking is enabled, it can send them externally.

3. **Network-based exfiltration.** If `network: true`, any process inside the sandbox can make outbound connections. Disable networking when not needed for dependency installation or API calls.

4. **Proxy socket access.** Any process inside the sandbox can connect to the proxy socket and interact with the host-side command. This is equivalent to what the AI agent already does — it's not an escalation of privilege, but it means the proxy isn't a privilege boundary within the sandbox.

5. **MCP server manipulation.** MCP servers run inside the sandbox. Compromised code could alter MCP server behavior to influence future AI sessions (e.g., poisoning task state, decisions, or persona prompts).

### Compared to No Sandbox

| Threat | No sandbox | Proxy sandbox |
|--------|------------|---------------|
| Steal API keys | Possible | Prevented |
| Access host filesystem | Full access | Mounted paths only |
| Steal SSH/cloud credentials | Possible | Prevented |
| Read/modify project files | Yes | Yes (by design) |
| Network exfiltration | Unrestricted | Configurable |
| Prompt injection blast radius | Entire machine | Container/VM only |

## Integration with Saga

Saga's `cmd/sandbox.go` already calls `warden run`. Two changes needed on the saga side:

1. **Add `--proxy claude` to warden args** when sandbox is enabled. In `buildWardenArgs()`, append `--proxy claude` to the argument list.

2. **Remove the MCP config path adjustment.** Currently saga switches MCP server commands to bare names (`saga` instead of absolute path) when sandboxed. This stays the same — `saga` runs inside the sandbox. Only `claude` is proxied.

No changes to saga's MCP server configuration, persona system, or orchestrator.

## What This Design Does Not Include

- **Multiple simultaneous proxy sessions.** One active connection per proxied command at a time. Sequential reuse is supported.
- **Credential-specific mount blocking.** Warden already denies common dotfile mounts via its access control system. This spec doesn't add new deny rules — it removes the need for credentials inside the sandbox entirely.
- **Proxy authentication.** The socket/vsock transport is not authenticated. Any process inside the sandbox can connect. This is acceptable because the sandbox is already a trust boundary — everything inside it runs as the same user.
- **Windows support.** Firecracker requires Linux. Docker proxy works on any platform Docker supports, but the default-to-Firecracker fallback is Linux-specific.
