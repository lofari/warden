# Sandbox Hardening Design

## Problem

A prompt injection can trick an AI coding agent into exfiltrating secrets, modifying build pipelines, or writing backdoors through the files and network access the sandbox exposes. Warden isolates the agent at the OS/VM level, but the mounted project files and network access are the escape hatch. An agent compromised by injected instructions can read `.env` files, modify `.git/hooks`, or `curl` secrets to an attacker's server — all within its legitimate permissions.

## Goals

1. **Defense in depth** — make obvious attacks hard even when the agent is compromised by prompt injection
2. **Audit trail** — structured log of every file and network operation so incidents can be reviewed after the fact
3. **Secure by default** — ship sensible deny-lists and network presets; users opt-out rather than opt-in
4. **Both runtimes** — Docker (convenience) and Firecracker (secure) share the same config surface

## Non-Goals

- Per-file content inspection (detecting secrets inside file contents)
- Rate limiting on file operations
- TOCTOU fix for file server path resolution (separate effort, requires `openat(2)` rewrite)
- Persistent audit log aggregation across sessions
- Web UI for audit review

---

## Feature 1: File Access Controls

Two layers checked in order inside the file server.

### Deny-list

Files the agent cannot access at all — neither read nor write. Checked in `resolvePath` before any operation. Uses glob patterns matched against the path relative to the mount root.

**Built-in defaults** (always active unless overridden):

```
.env
.env.*
*.pem
*.key
*.p12
*.pfx
.npmrc
.pypirc
.git/credentials
.git/config
**/.ssh/*
**/.aws/*
**/.gnupg/*
**/.docker/config.json
```

**Config:**

```yaml
mounts:
  - path: .
    mode: rw
    deny_extra:          # added to built-in defaults
      - secrets/
      - "*.secret"
    deny_override:       # replaces built-in defaults entirely (escape hatch)
      - .env
```

- `deny_extra` extends the defaults. Most users use this.
- `deny_override` replaces the defaults completely. For projects where the defaults are wrong (e.g., the project IS a `.pem` generator).
- If neither is set, built-in defaults apply.

### Read-only overrides

Paths within an `rw` mount that the agent can read but not modify. Checked in `requireWrite` against glob patterns. Both rename source and destination are checked — an agent cannot rename a read-only file to bypass the restriction, nor rename another file into a read-only path.

```yaml
mounts:
  - path: .
    mode: rw
    read_only:
      - .git/hooks
      - .github/workflows
      - Makefile
      - Dockerfile
```

### Pattern matching

Uses the `github.com/bmatcuk/doublestar/v4` library for glob matching, which supports `**` recursive patterns (unlike `filepath.Match` which does not). All patterns are matched against the relative path from the mount root.

### Deny-list enforcement points

The deny-list is checked at two points:

1. **In `resolvePath`** — the resolved relative path is matched against deny patterns. This catches direct access attempts.
2. **After symlink resolution** — the deny check runs again on the symlink-resolved path. This prevents a symlink `foo.txt -> .env` from bypassing the deny-list. Both the requested path and the resolved real path must pass the deny check.
3. **In `readdir` results** — denied entries are filtered from directory listings so the agent cannot discover denied file names.

### Implementation

The file server constructor becomes: `NewServer(root, readOnly, denyPatterns, readOnlyPatterns, auditLogger)`.

- **Deny check** runs inside `resolvePath`. After resolving the path and evaluating symlinks, match the relative path against deny patterns at both stages. If either matches, return an error. This blocks all operations (stat, read, readdir, write, etc.).
- **Read-only check** is factored into a `requireWritePath(path)` helper called by all write handlers (handleCreate, handleWrite, handleMkdir, handleRemove, handleRename, handleTruncate, handleSymlink, handleChmod, and handleOpen with write flags). It checks both `s.readOnly` and the read-only override patterns.
- **Readdir filtering** — `handleReadDir` filters out entries whose names match deny patterns before returning results.
- Denied accesses are logged to the audit log if enabled.

---

## Feature 2: Network Allowlist with Presets

### Config type change

The `Network` field in `SandboxConfig` changes from `bool` to `string`:

```go
type SandboxConfig struct {
    // ...
    Network string `yaml:"network"` // "off", "all", or a preset name
    Allow   []string `yaml:"allow"` // additional domains (only with preset)
    // ...
}
```

**YAML backward compatibility:**
- `network: false` → parsed as `"off"` by YAML (Go YAML parses `false` as string `"false"` when target is `string`; add custom `UnmarshalYAML` to map `false` → `"off"`, `true` → `"all"`)
- `network: true` → `"all"`
- `network: claude-code` → `"claude-code"`

**CLI flags:**
- `--network <value>` replaces both `--network` and `--no-network`
- `--no-network` is kept as shorthand for `--network off`
- `--allow <domain>` appends to the allow list (repeatable)

**`allow` field semantics:**
- Only meaningful when `network` is a preset name
- Ignored when `network` is `"off"` or `"all"`
- If `network` is `"all"` and `allow` is set, print a warning ("allow has no effect when network is 'all'")

### Built-in presets

Shipped with the binary, not user-editable. Support wildcard subdomains with `*.` prefix.

**`claude-code`** (recommended for Claude Code sessions):
```
api.anthropic.com
statsig.anthropic.com
sentry.io
registry.npmjs.org
pypi.org
files.pythonhosted.org
proxy.golang.org
sum.golang.org
github.com
*.githubusercontent.com
*.github.com
crates.io
static.crates.io
```

**`minimal`** (API access only):
```
api.anthropic.com
statsig.anthropic.com
```

Wildcard entries like `*.githubusercontent.com` match any subdomain. The DNS proxy resolves `objects.githubusercontent.com` and checks if it matches any wildcard pattern.

### DNS proxy

A lightweight DNS proxy runs on the sandbox's gateway IP (the host side of the TAP interface for Firecracker, or the bridge gateway for Docker). The sandbox's `/etc/resolv.conf` points to it.

- **Allowed domains**: proxy forwards to upstream DNS, returns real response
- **Blocked domains**: proxy returns NXDOMAIN and sends a notification over the vsock command channel (Firecracker) or control socket (Docker)

**Default iptables policy is DROP for outbound.** Only IPs resolved by the DNS proxy for allowed domains are permitted. This prevents IP-direct connections (e.g., `curl http://1.2.3.4`) from bypassing domain filtering. The iptables rules are:

```
# Default: drop all outbound
iptables -P OUTPUT DROP
# Allow loopback
iptables -A OUTPUT -o lo -j ACCEPT
# Allow DNS to proxy only
iptables -A OUTPUT -p udp --dport 53 -d <gateway-ip> -j ACCEPT
# Allow established connections (responses)
iptables -A OUTPUT -m state --state ESTABLISHED,RELATED -j ACCEPT
# Per-domain rules (added dynamically)
iptables -A OUTPUT -d <resolved-ip> -j ACCEPT
```

### Runtime domain approval

When the agent hits a blocked domain, two paths to approve it:

**Interactive prompt** (same terminal):
```
warden: agent wants to access docs.python.org — allow? [y/N]
```

Approval resolves the domain, adds iptables rules, and updates the DNS proxy's live allowlist.

**CLI command** (separate terminal):
```bash
warden allow docs.python.org
```

Communicates with the running warden process via a control socket at `$XDG_RUNTIME_DIR/warden-<pid>.sock` (or `/tmp/warden-<pid>.sock` fallback). Socket permissions are `0700` (owner-only) to prevent other users from modifying the allowlist.

Both paths update the same live state.

### Implementation per runtime

**Docker:**

The Docker runtime must become a long-lived process (no longer just `exec docker run`). Architecture:

1. **Pre-container setup:**
   - Create a custom Docker network: `docker network create --driver bridge warden-net-<id>`
   - Start DNS proxy goroutine bound to the bridge gateway IP
   - Install default-DROP iptables rules on the bridge

2. **Container launch:**
   - `docker run --network warden-net-<id> --dns <gateway-ip> ...`
   - The warden process uses `cmd.Start()` + `cmd.Wait()` (not `cmd.Run()`) to stay alive

3. **File access controls (when deny-list or read-only overrides are configured):**
   - Instead of bind mounts, warden starts the vsock file server on a Unix socket
   - The container runs a thin FUSE client that connects to the file server over the mounted Unix socket
   - This reuses the existing `internal/fileserver` and `internal/guest/fusefs` code
   - The FUSE client binary is baked into the warden Docker image (same as warden-init for Firecracker)
   - When NO access controls are configured, use plain bind mounts (zero overhead)

4. **Networking privileges:**
   - iptables rules are installed from the HOST side using `nsenter --net=/proc/<container-pid>/ns/net` — the container itself gets no extra capabilities
   - Requires warden to run with `CAP_NET_ADMIN` or sudo for the nsenter call, same as `warden-netsetup` for Firecracker
   - Alternative: pre-configure iptables on the custom bridge network before container starts (no nsenter needed, rules apply at bridge level)

   **Recommended approach:** Apply iptables rules at the bridge level, not inside the container namespace. This requires no extra container capabilities and uses the same `warden-netsetup` helper.

5. **Control socket:**
   - Same Unix socket as Firecracker for `warden allow` CLI
   - DNS notifications sent to the warden process which prompts in the terminal

**Firecracker:**
- DNS proxy runs as a goroutine alongside the existing network setup
- Guest `/etc/resolv.conf` configured via `NetworkConfigMessage` (already sends DNS field)
- iptables rules on the TAP interface (already managed by `warden-netsetup`)
- Notifications sent over the existing vsock command connection
- Control socket for `warden allow` CLI

---

## Feature 3: Audit Logging

Off by default. Enabled via CLI or config.

### Config

```yaml
default:
  audit_log: ./audit.jsonl     # file path, or omit to disable
```

CLI:
```bash
warden run --audit-log ./audit.jsonl -- claude
warden run --audit-log - -- claude              # stderr
```

Relative paths are resolved relative to the working directory at warden launch time.

### Format

JSON Lines, one entry per event:

```json
{"ts":"2026-03-20T14:32:01.003Z","op":"read","path":"src/main.go","bytes":4096,"ok":true}
{"ts":"2026-03-20T14:32:01.015Z","op":"write","path":"src/main.go","bytes":312,"ok":true}
{"ts":"2026-03-20T14:32:01.020Z","op":"stat","path":".env","ok":false,"error":"denied"}
{"ts":"2026-03-20T14:32:05.100Z","op":"dns","domain":"evil.com","ok":false,"error":"blocked"}
{"ts":"2026-03-20T14:32:05.200Z","op":"dns","domain":"docs.python.org","ok":true,"note":"user-approved"}
```

**Logged events:**
- File operations: op, path, bytes (read/write only), ok/error
- Deny-list hits: path that was blocked
- Read-only violations: path + attempted operation
- DNS queries: domain, allowed/blocked, user-approved
- Network connection attempts (if blocked)

**Not logged:** file contents (too large, security risk in the log itself).

### Log rotation

No built-in rotation. The log file is opened with `O_APPEND` so external tools (logrotate, etc.) can rotate it. For long-running sessions, the log path can include a timestamp: `audit-2026-03-20.jsonl`.

### Exit summary

Printed to stderr when the sandbox exits:

```
warden: audit summary
  files read:     142
  files written:   23
  bytes read:     2.1 MB
  bytes written:  48 KB
  denied:          3 (.env, .env.local, .git/credentials)
  network blocked: 1 (pastebin.com)
```

The summary is always printed when audit logging is enabled. It gives a quick "did anything suspicious happen?" signal.

### Implementation

An `AuditLogger` interface injected into the file server and DNS proxy:

```go
type AuditLogger interface {
    Log(entry AuditEntry)
    Summary() AuditSummary
}
```

Both runtimes wire it the same way. The logger writes to the configured output (file or stderr). A no-op implementation is used when audit logging is disabled (zero overhead).

---

## Config Summary

Complete `.warden.yaml` with all new features:

```yaml
default:
  runtime: docker
  image: ubuntu:24.04
  tools: [node, python]
  network: claude-code
  allow:
    - registry.my-company.com
  memory: 8g
  timeout: 2h
  audit_log: ./audit.jsonl
  mounts:
    - path: .
      mode: rw
      deny_extra:
        - secrets/
      read_only:
        - .git/hooks
        - .github/workflows

profiles:
  secure:
    runtime: firecracker
    network: minimal
```

`warden init` generates a template with the new fields commented out and the defaults documented.

---

## Runtime Comparison

| Feature | Docker | Firecracker |
|---------|--------|-------------|
| File deny-list | File server (when configured) or bind mount (no controls) | File server deny check |
| Read-only overrides | File server (when configured) | File server write check |
| Network allowlist | Bridge-level iptables | TAP iptables |
| DNS proxy | Goroutine on bridge gateway | Goroutine on TAP gateway |
| Interactive domain approval | Control socket | vsock notification |
| `warden allow` CLI | Control socket | Control socket |
| Audit logging | File server logging | File server logging |
| Isolation level | Container (namespace) | MicroVM (kernel boundary) |

Docker uses plain bind mounts when no file access controls are configured (deny-list, read-only overrides). When access controls are needed, Docker switches to the same file-server-based approach as Firecracker, using a Unix socket instead of vsock. This keeps the security implementation identical across runtimes while preserving zero-overhead for the simple case.

### Docker file server architecture

When file access controls are configured, Docker mounts work as follows:

1. Warden starts a file server per mount, listening on a Unix socket in a temp directory
2. The temp directory containing the socket is bind-mounted into the container
3. A thin FUSE client binary (`warden-mount`) runs inside the container, connects to the Unix socket, and mounts the FUSE filesystem at the expected path
4. The container entrypoint is wrapped: `warden-mount` sets up mounts first, then exec's the user command

This reuses `internal/fileserver` (host side) and `internal/guest/fusefs` (container side) with a Unix socket transport instead of vsock. The `warden-mount` binary is built from the same `internal/guest` code as `warden-init` but without the VM-specific parts (no vsock listener, no filesystem mounting, no network config).
