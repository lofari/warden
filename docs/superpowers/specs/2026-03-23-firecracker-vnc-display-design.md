# Firecracker VNC Display

## Goal

Add opt-in virtual display support to Firecracker VMs, enabling AI agents to visually test web apps and GUIs. When `--display` is passed, the VM runs Xvfb + x11vnc inside the guest, and warden exposes a local VNC endpoint on the host via vsock forwarding.

## Architecture

When `--display` is set, the Firecracker VM boots with Xvfb and x11vnc running inside the guest. The guest init agent starts a VNC listener on a dedicated vsock port (2048). The host-side warden process accepts the vsock connection and forwards it to a local TCP port, printing `vnc://localhost:<port>` to stderr. Saga or a VNC client connects to that port.

### Data Flow

```
Agent/Saga ──VNC──► localhost:<port> ──► warden (host) ──vsock──► VM guest
                                                                    │
                                                            x11vnc ◄─► Xvfb
                                                                        │
                                                                App under test
```

## Config

### CLI

```
warden run --display [--resolution 1920x1080x24] --runtime firecracker -- firefox http://localhost:3000
```

- `--display` — enables virtual display (opt-in, default off)
- `--resolution` — Xvfb screen resolution (default `1280x1024x24`)

### `.warden.yaml`

```yaml
display: true
resolution: "1920x1080x24"  # optional, default 1280x1024x24
```

### Runtime Restriction

`--display` is only supported with the `firecracker` runtime. This check happens in `root.go` **after** the runtime is resolved (step 8 in the current flow), NOT inside `config.Validate()`. This is because the runtime may come from CLI `--runtime` flag which is applied after validation.

```go
// After runtime is resolved (after rt := runtime.NewRuntime(rtName))
if cfg.Display && rtName == "docker" {
    return fmt.Errorf("--display is only supported with firecracker runtime")
}
```

### Resolution Default

When `display: true` but `resolution` is absent (empty string), the runtime applies the default `"1280x1024x24"` at the point where it builds the `DisplayConfigMessage`. The CLI flag also defaults to this value. `config.Validate()` only validates non-empty resolution strings — an empty resolution with `display: true` is valid (means "use default").

## Protocol Changes

Two new message types in `internal/protocol/protocol.go`:

```go
// Host -> Guest
type DisplayConfigMessage struct {
    Resolution string `json:"resolution"` // e.g. "1280x1024x24"
    VsockPort  uint32 `json:"vsock_port"` // 2048
}

// Guest -> Host
type DisplayReadyMessage struct {
    Port uint32 `json:"port"` // confirms VNC is listening on vsock
}
```

Protocol envelope type strings:
- `DisplayConfigMessage` → `"display_config"`
- `DisplayReadyMessage` → `"display_ready"`

Add both to the `WriteMessage` and `ReadMessage` switch statements, and update the protocol comment block (`Host -> Guest: ...`, `Guest -> Host: ...`).

### Reserved Vsock Port

Port 2048 is reserved for VNC display. Mount file servers use ports 1025+. To prevent collision, cap the mount port range: if `len(cfg.Mounts) > 1022` the port would reach 2047, which is fine. Port 2048 is always reserved for display regardless of whether `--display` is set — file server ports simply never reach it.

### Message Flow

1. Host sends `DisplayConfigMessage` after receiving `MountsReadyMessage` from guest (or after mount setup is skipped if no mounts), before `ExecMessage`
2. Guest starts Xvfb + x11vnc, waits for both to be ready
3. Guest sends `DisplayReadyMessage` back to host
4. Host starts TCP→vsock proxy goroutine, prints VNC URL to stderr
5. Host sends `ExecMessage` as usual

## Config Type Changes

Add to `SandboxConfig` in `internal/config/types.go`:

```go
type SandboxConfig struct {
    // ... existing fields ...
    Display    bool   `yaml:"display"`
    Resolution string `yaml:"resolution"`
}
```

## Guest Side Implementation

In `cmd/warden-init/main.go`, add `*protocol.DisplayConfigMessage` as a new case in the pre-exec message loop (alongside `NetworkConfigMessage` and `MountConfigMessage`):

### Display Setup Steps

1. Ensure `/tmp/.X11-unix/` directory exists (`os.MkdirAll`)
2. Start `Xvfb :99 -screen 0 <resolution> -ac` as a background process
3. Wait for `/tmp/.X11-unix/X99` socket to appear (poll every 50ms, timeout 3s)
4. Start `x11vnc -display :99 -forever -nopw -rfbport 5900` listening on TCP port 5900 inside the VM
5. Wait for x11vnc readiness: poll TCP connect to `localhost:5900` (every 50ms, timeout 3s)
6. Start a vsock listener on port 2048 that loops accepting connections — for each accepted connection, dial `localhost:5900` and proxy bytes bidirectionally. The listener runs in a goroutine and loops to support VNC client reconnects.
7. Send `DisplayReadyMessage{Port: 2048}` back to host via the command connection
8. Append `DISPLAY=:99` to the environment — done in `handleConnection` after the display setup, before building `cmd.Env`. Specifically: if a `DisplayConfigMessage` was received, append `"DISPLAY=:99"` to `execMsg.Env` (or to the default env if `execMsg.Env` is empty) before constructing `exec.Cmd`.

### Internal Port Choice

x11vnc listens on TCP port 15900 inside the VM (not 5900) to avoid conflicts if the user's command also uses VNC. This is an internal detail — the port is never exposed outside the VM.

## Host Side Implementation

In `internal/runtime/firecracker/firecracker.go`, in `Run()`:

1. After mount setup (after receiving `MountsReadyMessage` or skipping mounts), if `cfg.Display` is true:
   - Resolve resolution: if `cfg.Resolution == ""`, use `"1280x1024x24"`
   - Send `DisplayConfigMessage{Resolution: resolution, VsockPort: 2048}`
   - Wait for `DisplayReadyMessage` from guest (with 10s timeout — Xvfb + x11vnc startup)
   - Pick a free local TCP port: `listener, _ := net.Listen("tcp", "localhost:0")`
   - Store the listener on `vmInstance` so it is closed in `cleanup()`
   - Start `proxyVNC(listener, vm.vsockPath)` goroutine
   - Extract port from `listener.Addr().(*net.TCPAddr).Port`
   - Print `warden: VNC available at vnc://localhost:<port>` to stderr
2. Proceed with `ExecMessage` as usual

### TCP→vsock Proxy

The proxy loops on `Accept()` to support VNC client reconnection. Each accepted connection dials a new vsock connection (port 2048), which the guest-side listener accepts and proxies to x11vnc.

```go
func proxyVNC(listener net.Listener, vsockPath string) {
    for {
        tcpConn, err := listener.Accept()
        if err != nil {
            return // listener closed (VM cleanup)
        }
        go func() {
            defer tcpConn.Close()
            vsockConn, err := dialGuest(vsockPath, 2048, 10*time.Second)
            if err != nil {
                return
            }
            defer vsockConn.Close()
            go io.Copy(vsockConn, tcpConn)
            io.Copy(tcpConn, vsockConn)
        }()
    }
}
```

### Proxy Lifecycle

Add a `vncListener net.Listener` field to `vmInstance`. In `cleanup()`, if `vm.vncListener != nil`, close it before killing the Firecracker process. This causes `proxyVNC`'s `Accept()` to return an error and the goroutine to exit cleanly. Errors during proxy teardown (disconnected VNC client) are silently swallowed — no stderr noise.

## CLI Wiring

In `internal/cli/root.go`:

- Add `--display` bool flag (default false)
- Add `--resolution` string flag (default `"1280x1024x24"`)
- Wire: `if cmd.Flags().Changed("display") { cfg.Display = display }`
- Wire: `if cmd.Flags().Changed("resolution") { cfg.Resolution = resolution }`
- After runtime is resolved (after `rt, err := runtime.NewRuntime(rtName)`), add:
  ```go
  if cfg.Display && rtName == "docker" {
      return fmt.Errorf("--display is only supported with firecracker runtime")
  }
  ```

## Firecracker Base Image

Bake `xvfb` and `x11vnc` into the Firecracker base rootfs image. They are small (~15MB total) and this avoids conditional image building. The packages are:

- `xvfb` (or `xorg-server` with Xvfb)
- `x11vnc`
- `x11-xserver-utils` (for xdpyinfo, useful for debugging)

These are only installed in the Firecracker rootfs, not the Docker base image.

## Error Handling

- **Xvfb fails to start**: log warning to stderr, do NOT send `DisplayReadyMessage`, continue to exec without `DISPLAY` set. The host times out waiting for `DisplayReadyMessage` and logs a warning, then proceeds.
- **x11vnc fails to start**: same — log, continue without VNC.
- **No VNC client connects**: no overhead — x11vnc and the proxy goroutine idle.
- **VNC proxy goroutine dies or VNC client disconnects mid-session**: silently swallowed, no stderr. VM keeps running.
- **VM exits while VNC client is connected**: `cleanup()` closes `vncListener`, proxy goroutine exits, vsock/TCP connections get EOF. Silently handled.
- **`--display` on Docker**: return error after runtime resolution (not in `Validate()`).
- **Resolution parse**: validate format matches `WxHxD` pattern in `config.Validate()`.
- **`--display` with no mounts**: works fine — display setup happens independently of mount setup.

## Validation

Add to `config.Validate()` (resolution format only — runtime check is NOT here):

```go
if c.Resolution != "" {
    parts := strings.Split(c.Resolution, "x")
    if len(parts) != 3 {
        return fmt.Errorf("invalid resolution %q: must be WxHxD (e.g. 1280x1024x24)", c.Resolution)
    }
    for _, p := range parts {
        n, err := strconv.Atoi(p)
        if err != nil || n <= 0 {
            return fmt.Errorf("invalid resolution %q: components must be positive integers", c.Resolution)
        }
    }
}
```

## Testing

- **Unit**: `DisplayConfigMessage` / `DisplayReadyMessage` serialization roundtrip
- **Unit**: TCP→vsock proxy (bidirectional byte forwarding with `net.Pipe`)
- **Unit**: Free port picker
- **Unit**: Resolution validation (valid, invalid, zero components)
- **Unit**: `--display` with Docker runtime returns error
- **Integration**: requires a real Firecracker VM with display packages — defer to manual testing

## DryRun

`warden run --display --dry-run` should include `"display": true` and `"resolution": "1280x1024x24"` in the JSON output. Update `DryRun()` in `firecracker.go` to add these fields to `vmConfig`:

```go
if cfg.Display {
    vmConfig["display"] = true
    resolution := cfg.Resolution
    if resolution == "" {
        resolution = "1280x1024x24"
    }
    vmConfig["resolution"] = resolution
}
```
