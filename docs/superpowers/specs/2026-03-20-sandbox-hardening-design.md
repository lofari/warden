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
.git/credentials
.git/config
**/.ssh/*
**/.aws/*
**/.gnupg/*
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

Paths within an `rw` mount that the agent can read but not modify. Checked in `requireWrite` against glob patterns.

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

### Implementation

The file server constructor becomes: `NewServer(root, readOnly, denyPatterns, readOnlyPatterns)`.

- **Deny check** runs inside `resolvePath`. After resolving the path, match the relative path against deny patterns. If matched, return an error. This blocks all operations (stat, read, readdir, write, etc.).
- **Read-only check** runs inside `requireWrite`. After the existing `s.readOnly` check, match the relative path against read-only patterns. If matched, return a "read-only path" error.
- Pattern matching uses `filepath.Match` for simple globs and prefix matching for directory patterns (trailing `/`). `**` patterns require recursive matching.
- Denied accesses are logged to the audit log if enabled.

---

## Feature 2: Network Allowlist with Presets

### Config surface

The `network` field changes from boolean to a union type:

```yaml
# Legacy (still supported)
network: false           # no network
network: true            # allow all (no filtering)

# New: preset name
network: claude-code     # built-in allowlist preset

# Extension
network: claude-code
allow:                   # additional domains
  - registry.my-company.com
  - artifactory.internal
```

CLI:
```bash
warden run --network claude-code -- claude
warden run --network claude-code --allow docker.io -- claude
```

### Built-in presets

Shipped with the binary, not user-editable:

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
crates.io
```

**`minimal`** (API access only):
```
api.anthropic.com
statsig.anthropic.com
```

### DNS proxy

A lightweight DNS proxy runs on the sandbox's gateway IP (the host side of the TAP interface for Firecracker, or the bridge gateway for Docker). The sandbox's `/etc/resolv.conf` points to it.

- **Allowed domains**: proxy forwards to upstream DNS, returns real response
- **Blocked domains**: proxy returns NXDOMAIN and sends a notification over the vsock command channel (Firecracker) or a control socket (Docker)

At startup, all allowed domains are pre-resolved and iptables rules installed for their IPs. The DNS proxy handles ongoing resolution as IPs change.

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

Communicates with the running warden process via a control socket (`/tmp/warden-<pid>.sock`).

Both paths update the same live state.

### Implementation per runtime

**Docker:**
- Create a custom bridge network
- Run DNS proxy as a goroutine in the warden process
- Set `--dns <gateway-ip>` on the container
- Install iptables OUTPUT rules on the container's network namespace
- The warden process must stay alive (no longer just `exec docker run`)

**Firecracker:**
- DNS proxy runs as a goroutine alongside the existing network setup
- Guest `/etc/resolv.conf` configured via `NetworkConfigMessage` (already sends DNS field)
- iptables rules on the TAP interface (already managed by `warden-netsetup`)
- Notifications sent over the existing vsock command connection

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
| File deny-list | Bind mount filtering | File server deny check |
| Read-only overrides | Bind mount filtering | File server write check |
| Network allowlist | Custom bridge + iptables | TAP iptables |
| DNS proxy | Goroutine on bridge gateway | Goroutine on TAP gateway |
| Interactive domain approval | Control socket | vsock notification |
| `warden allow` CLI | Control socket | Control socket |
| Audit logging | Mount filter logging | File server logging |
| Isolation level | Container (namespace) | MicroVM (kernel boundary) |

For Docker, the file access controls need a different implementation since Docker uses bind mounts, not the vsock file server. Two approaches:

1. **FUSE overlay on Docker too** — run the same file server for Docker mounts. Consistent but adds FUSE overhead to the simpler runtime.
2. **Bind mount + inotify filter** — Docker-specific filtering that intercepts at the mount level. Complex and incomplete.
3. **Docker-side deny-list** — mount the directory but run a helper process inside the container that enforces deny-list via LD_PRELOAD or seccomp-bpf. Fragile.

**Recommendation:** Use approach 1 — run the file server for Docker mounts too when deny-list or read-only overrides are configured. If no access controls are configured, use plain bind mounts (zero overhead for the simple case). This keeps the security implementation identical across runtimes.
