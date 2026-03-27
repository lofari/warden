# Auth Broker — Design Spec

Run AI assistants inside the sandbox with full functionality. Real credentials never enter the sandbox.

**Supersedes:** `2026-03-27-command-proxy-design.md` (command proxy approach). That design ran Claude on the host, which left Claude's built-in tools (bash, file read/write) unsandboxed. This design runs Claude inside the sandbox where all tools are contained, and proxies only the authentication layer.

## Problem

AI coding assistants (Claude CLI) need OAuth tokens to call the Anthropic API. Running them inside a sandbox requires credentials. Mounting the real credentials file exposes them to any process in the sandbox — a compromised dependency could steal OAuth tokens.

Running Claude on the host (command proxy approach) protects credentials but leaves Claude's built-in tools unsandboxed, defeating the purpose.

## Solution

Claude runs inside the sandbox with a **fake credentials file**. A host-side reverse proxy intercepts API requests, strips the fake auth, and injects the real OAuth token before forwarding to Anthropic. The real token never enters the sandbox in any form — not on disk, not in environment variables, not in memory of any sandboxed process.

## Decisions

- **Claude runs inside the sandbox** — all built-in tools (bash, file I/O) are contained
- **Fake credentials** — Claude reads a dummy `.credentials.json` and thinks it's authenticated
- **Reverse proxy on host** — intercepts API requests, replaces auth, forwards to Anthropic
- **`ANTHROPIC_BASE_URL`** — Claude CLI supports this env var to redirect API calls
- **Token refresh on host** — the proxy handles OAuth refresh; Claude never refreshes itself
- **Default runtime: Firecracker** — with auto-fallback to Docker if `/dev/kvm` unavailable

## Architecture

```
SANDBOX                                         HOST

~/.claude/.credentials.json                     Auth Broker
  (fake token: "warden-sandbox-token")            ├─ reads real ~/.claude/.credentials.json
                                                  ├─ holds real OAuth tokens in memory
Claude CLI (full interactive mode)                │
  ANTHROPIC_BASE_URL=http://proxy.sock            │
  ├─ built-in bash tool (CONTAINED)               │
  ├─ built-in file tool (CONTAINED)               │
  ├─ MCP tools (CONTAINED)                        │
  │                                               │
  ├─ API request ──────────────────────────────►  ├─ receives HTTP request
  │   Authorization: Bearer warden-sandbox-token  ├─ strips fake token
  │   (worthless outside sandbox)                 ├─ injects real OAuth token
  │                                               ├─ forwards HTTPS to api.anthropic.com
  ◄──────────────────────────────────────────────  └─ returns response
```

### What the Sandbox Sees

- Fake credentials file with a dummy token
- `ANTHROPIC_BASE_URL` pointing to a local socket (not the real API)
- Claude binary (read-only mount or in image)
- Project files (mounted per warden config)
- MCP servers running locally

### What the Sandbox Cannot Access

| Asset | Protection |
|-------|------------|
| Real OAuth tokens | Never enter sandbox — only in broker memory on host |
| `~/.claude/.credentials.json` (real) | Not mounted — fake version provided |
| `~/.ssh/` | Not mounted |
| `~/.gitconfig` | Not mounted |
| `~/.npmrc`, `~/.aws/`, `~/.gcloud/` | Not mounted |
| Host filesystem | Only explicitly mounted paths |
| Real Anthropic API | Only reachable through the broker |

## Configuration

New `auth_broker` field in `.warden.yaml`:

```yaml
default:
  runtime: firecracker
  auth_broker:
    enabled: true
    credentials: ~/.claude/.credentials.json    # host path to real credentials
    target: api.anthropic.com                   # API host to proxy
  tools: [node, go]
  network: true
```

When `auth_broker.enabled` is true, warden automatically:
1. Creates fake credentials inside the sandbox
2. Sets `ANTHROPIC_BASE_URL` in the sandbox environment
3. Starts the broker on the host
4. Sets up the transport (Unix socket or vsock)

CLI override: `warden run --auth-broker -- claude ...`

### Config Struct Change

Add to `SandboxConfig` in `internal/config/types.go`:

```go
type AuthBrokerConfig struct {
    Enabled     bool   `yaml:"enabled"`
    Credentials string `yaml:"credentials,omitempty"` // default: ~/.claude/.credentials.json
    Target      string `yaml:"target,omitempty"`      // default: api.anthropic.com
}

// In SandboxConfig:
AuthBroker *AuthBrokerConfig `yaml:"auth_broker,omitempty"`
```

## Fake Credentials

The broker generates a fake `~/.claude/.credentials.json` inside the sandbox:

```json
{
  "accessToken": "warden-sandbox-token",
  "refreshToken": "warden-sandbox-refresh",
  "expiresAt": 9999999999999,
  "scopes": ["user:inference"],
  "subscriptionType": "max"
}
```

Key properties:
- **`expiresAt` set far in the future** — Claude won't attempt token refresh (the broker handles this if needed)
- **`subscriptionType` matches the real subscription** — prevents Claude from showing upgrade prompts or restricting features. The broker reads the real credentials and mirrors the subscription type.
- **Token value is fixed and known** — the broker validates that incoming requests carry exactly `warden-sandbox-token`. Requests with any other token are rejected (defense against leaked real tokens somehow reaching the broker).
- **Full structure mirrored** — the fake credentials file copies the complete structure of the real credentials (all fields), substituting only `accessToken`, `refreshToken`, and `expiresAt`. This prevents validation failures if Claude expects fields like `organizationId`, `accountId`, or `tokenType`.

### Delivery

**Docker:** The fake credentials file is written to a tmpfs and bind-mounted:

```bash
# Host creates fake creds in a private temp directory (0700)
dir=$(mktemp -d /tmp/warden-auth-XXXXXXXX)
chmod 700 "$dir"
echo '{"accessToken":"warden-sandbox-token",...}' > "$dir/credentials.json"
chmod 600 "$dir/credentials.json"

# Mount into container (read-only)
docker run \
  -v /tmp/warden-auth-<id>/credentials.json:/root/.claude/.credentials.json:ro \
  ...
```

**Firecracker:** The guest init agent receives an `AuthBrokerConfigMessage` and writes the fake credentials to `/root/.claude/.credentials.json` inside the VM.

Cleanup: tmpfs directory removed when container/VM exits.

## Transport Layer

### Docker: Unix Socket with TCP Bridge

The broker listens on a Unix socket on the host. A tiny TCP-to-UDS bridge runs inside the container so Claude can connect via `localhost`.

**Why not direct Unix socket URL?** Node.js `http.Agent` supports `socketPath`, but `ANTHROPIC_BASE_URL` is parsed as a URL by Claude's SDK, which may not handle `unix:` socket paths. A localhost TCP bridge is reliable and works with `--network none` (loopback is always available even without network).

**Why not Docker port forwarding?** `-p` port mapping doesn't work with `--network none`, which is the secure default. The bridge runs inside the container, so no network config changes are needed.

```
HOST                                    CONTAINER

Auth Broker                             TCP-to-UDS bridge (tiny binary)
  ├─ listens on UDS ◄───────────────── ├─ listens on localhost:19280
  │  /tmp/warden-auth-<id>/proxy.sock  ├─ connects to /run/warden/auth.sock
  │                                    │   (bind-mounted from host)
  │                                    │
  │                                    Claude CLI
  │                                      ANTHROPIC_BASE_URL=http://localhost:19280
```

The bridge binary is the same `warden-shim` from the command proxy design, repurposed for TCP-to-UDS bridging. Or a dedicated `warden-bridge` binary — either way, it's statically compiled and bind-mounted.

Port 19280 is chosen as an unlikely-to-collide ephemeral port for the loopback listener. It's internal to the container and never exposed to the host network.

### Firecracker: Vsock with TCP Bridge

The broker listens on vsock port 2900 (reserved for auth broker — below the command proxy range at 3000+).

Port map:
| Port | Use |
|------|-----|
| 1024 | Guest init |
| 1025+ | FUSE mounts |
| 2048 | VNC display |
| 2900 | Auth broker (new) |
| 3000+ | Command proxy (if used) |

Inside the VM, a TCP-to-vsock bridge listens on `localhost:19280` and connects to vsock port 2900. `ANTHROPIC_BASE_URL=http://localhost:19280`.

Port assignment sent via protocol message (see Protocol section).

## Auth Broker Implementation

New package: `internal/authbroker/`

```go
type Broker struct {
    Credentials  CredentialStore  // reads/refreshes real tokens on host
    Target       string           // "api.anthropic.com"
    FakeToken    string           // "warden-sandbox-token"
    Listener     net.Listener     // UDS or TCP
}

func (b *Broker) Serve(ctx context.Context) error
func (b *Broker) Close() error
```

### Request Flow

1. Claude inside sandbox sends HTTP request to `ANTHROPIC_BASE_URL`
2. Broker receives the request
3. **Validate:** check `Authorization` header equals `Bearer warden-sandbox-token`. Reject if not.
4. **Strip:** remove the fake `Authorization` header
5. **Inject:** add `Authorization: Bearer <real-oauth-token>` from host credential store
6. **Forward:** make HTTPS request to `https://api.anthropic.com` with the real token
7. **Return:** relay the response back to Claude

### Request Filtering

The broker restricts both the target host and the API paths:

**Host filtering:**
- Requests to `api.anthropic.com` → check path allowlist
- Requests to any other host → rejected with 403

**Path allowlisting:**
- `/v1/messages` — chat completions (Claude's primary API)
- `/v1/complete` — legacy completions
- `/api/oauth/file_upload` — file attachment uploads
- All other paths → rejected with 403

This prevents a compromised sandbox from using the broker to hit Anthropic account management endpoints (API key creation, organization settings, billing) with the real user's credentials. The allowlist covers only the endpoints Claude CLI uses during normal operation.

If Claude CLI adds new API endpoints in future versions, the broker logs rejected paths to stderr so the user can update the allowlist.

**Telemetry and feature flags:** Claude CLI may contact `statsig.anthropic.com` or other non-API endpoints for telemetry and feature flags. These do not go through `ANTHROPIC_BASE_URL` and will fail with `--network none`. This may cause startup delays or missing feature flags. Test Claude CLI startup with `--network none` during implementation and document any observed side effects.

### Token Refresh

The broker manages OAuth token lifecycle on the host:

1. On startup, read `~/.claude/.credentials.json` (real credentials)
2. Store access token and refresh token in memory
3. Before each forwarded request, check if access token is expired
4. If expired, use the refresh token to get a new access token (standard OAuth refresh flow to `api.anthropic.com/oauth/token`)
5. Update the stored tokens and write back to `~/.claude/.credentials.json` on host
6. Never expose refresh flow to the sandbox — Claude inside sees no 401s

**Concurrent refresh safety:** Claude can make parallel API requests (e.g., multiple tool calls). Multiple goroutines could detect the token as expired simultaneously. The credential store uses a `refreshOnce` pattern — the first goroutine to detect expiry acquires a mutex and refreshes; others block on the mutex and reuse the refreshed token when it completes. This prevents multiple simultaneous refresh attempts that would invalidate each other's refresh tokens.

### Credential Store

```go
type CredentialStore struct {
    path         string          // ~/.claude/.credentials.json
    mu           sync.Mutex      // protects refresh
    accessToken  string
    refreshToken string
    expiresAt    time.Time
    scopes       []string
    subType      string          // "max", "pro", etc.
}

func NewCredentialStore(path string) (*CredentialStore, error)
func (c *CredentialStore) GetAccessToken() (string, error)  // refreshes if expired, serialized
func (c *CredentialStore) SubscriptionType() string
```

`GetAccessToken()` flow:
1. Lock mutex
2. If token is not expired, unlock and return it
3. If expired, perform refresh (HTTP call to Anthropic OAuth endpoint)
4. Update stored tokens, write to disk, unlock, return new token
5. If refresh fails, return error (broker returns 502 to Claude)

## Protocol Messages (Firecracker)

New message type in `internal/protocol/`:

```go
type AuthBrokerConfigMessage struct {
    Port            uint32 `json:"port"`             // vsock port (3000)
    FakeCredentials string `json:"fake_credentials"` // JSON string for fake .credentials.json
    BaseURL         string `json:"base_url"`         // "http://localhost:<port>"
}
```

Sent after `MountConfigMessage`, before `ExecMessage`. Guest init:
1. Writes fake credentials to `/root/.claude/.credentials.json`
2. Starts TCP-to-vsock bridge on `localhost:<port>` → vsock port 3000
3. Responds with `AuthBrokerReadyMessage`

## Claude Binary Delivery

Claude CLI must be available inside the sandbox. Options:

**Docker:** Bind-mount the host's Claude binary read-only:
```
-v /home/user/.local/share/claude/versions/2.1.86:/usr/local/bin/claude:ro
```

The exact path is resolved via `readlink -f $(which claude)` at runtime.

**Firecracker:** Claude binary is copied into the rootfs during image build, or mounted via FUSE (using warden's existing file server infrastructure). FUSE mount is preferred — it avoids baking a specific Claude version into the image.

## Runtime Default Change

Same as the command proxy spec: default runtime changes from `docker` to `firecracker` with auto-fallback.

`DefaultConfig()` in `internal/config/defaults.go` returns `Runtime: ""` (empty string = auto-detect).

`runtime.ResolveRuntime(preferred string)`:
- Non-empty → use exactly, error if unavailable
- Empty → try Firecracker (check `/dev/kvm` + binary), fall back to Docker with stderr warning

## Saga Integration

Changes needed in saga's `cmd/sandbox.go`:

1. **Remove `--proxy claude`** from `buildWardenArgs()` (replaces the command proxy approach)
2. **Add `--auth-broker`** flag to warden args when sandbox is enabled
3. **Mount Claude binary** — add the claude binary path to warden's mount list
4. The MCP config already uses bare commands for sandboxed mode — no change needed there

```go
func buildWardenArgs(opts SandboxOpts) []string {
    args := []string{"run"}
    // ... existing profile, display, resolution logic ...
    args = append(args, "--auth-broker")
    args = append(args, "--")
    return args
}
```

## Security Model

### What This Protects

- **Credential theft:** Real OAuth tokens never enter the sandbox. The fake token is worthless — it only works with the local broker, which validates the token value and only forwards to `api.anthropic.com`.
- **Built-in tool containment:** Claude's bash and file tools run inside the sandbox. They can only access mounted project files, not the host filesystem.
- **Exfiltration via proxy:** The broker only forwards to `api.anthropic.com`. It cannot be used as an open relay to send data to attacker-controlled servers.
- **Token from memory dump:** Even if malicious code dumps all process memory inside the sandbox, it finds only the fake token. The real token exists only in the broker process on the host.

### What This Does Not Protect

1. **Prompt injection → harmful MCP tool calls.** If project content manipulates Claude, it can still execute harmful tool calls inside the sandbox. The sandbox limits blast radius (project files only, no host access), but the damage is real within that scope.

2. **Project file exfiltration via API.** Claude makes API calls to Anthropic through the broker. If prompt injection convinces Claude to include project file contents in its API requests (e.g., as part of conversation context), that data reaches Anthropic's servers. The broker cannot distinguish legitimate API usage from exfiltration attempts — it forwards all requests to the target host.

3. **Network exfiltration (if enabled).** If `network: true`, processes inside the sandbox can make outbound connections to any host. The broker doesn't restrict general networking — it only handles requests routed through `ANTHROPIC_BASE_URL`. Disable networking when not needed.

4. **Fake token replay.** The fake token `warden-sandbox-token` is deterministic. If an attacker knows the value, they could craft requests to the broker from inside the sandbox. This is not an escalation — any process in the sandbox can already reach the broker. The token validation prevents accidental use of leaked real tokens, not intentional broker access.

5. **Claude version mismatch.** If Claude CLI updates its credential file format, the fake credentials may fail validation. The broker should mirror the format of the real credentials exactly, substituting only the token values.

### Compared to Alternatives

| Threat | No sandbox | Command proxy (old) | Auth broker (this) |
|--------|------------|--------------------|--------------------|
| Steal OAuth tokens | Possible | Prevented | **Prevented** |
| Claude bash tool on host | Full access | **Full access** | Prevented |
| Claude file tool on host | Full access | **Full access** | Prevented |
| Access host filesystem | Full access | Prevented | **Prevented** |
| Project file exfiltration | Via network | Via network | Via API or network |
| Prompt injection scope | Entire machine | **Entire machine** | Sandbox only |

The auth broker is strictly stronger than the command proxy for the AI assistant use case.

## What This Design Does Not Include

- **Multi-tool proxying.** The command proxy spec supported proxying arbitrary commands (cursor, etc.). This design is specific to Claude CLI's OAuth flow. Other tools would need their own auth mechanisms.
- **Certificate pinning bypass.** If a future Claude version pins the Anthropic API certificate, the broker approach still works — it's a reverse proxy, not a MITM. Claude connects to the broker via plain HTTP on a local socket, and the broker makes the real HTTPS connection.
- **Credential encryption at rest.** The real credentials on the host are stored as they always were (`~/.claude/.credentials.json` with mode 600). The broker reads them — no additional encryption layer.
- **Audit logging.** The broker does not log API requests. This could be added later for security monitoring.
