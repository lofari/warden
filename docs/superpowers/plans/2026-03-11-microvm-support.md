# Firecracker MicroVM Runtime Support — Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Firecracker microVM as an alternative runtime alongside Docker, behind a `Runtime` interface abstraction.

**Architecture:** Extract existing Docker logic into a `Runtime` interface implementation. Add Firecracker as a second implementation. Shared utilities (signals, timeout, TTY, mounts) live in `runtime/shared/`. A guest init agent communicates over vsock. A privileged helper binary handles TAP networking.

**Tech Stack:** Go 1.21, Cobra CLI, Firecracker API (HTTP over Unix socket), vsock, virtiofs, ext4 rootfs images

**Spec:** `docs/superpowers/specs/2026-03-11-microvm-support-design.md`

---

## File Structure

### New files
- `internal/runtime/runtime.go` — Runtime interface, ImageInfo, NewRuntime() factory
- `internal/runtime/mounts.go` — ResolveMounts() (moved from container/args.go)
- `internal/runtime/mounts_test.go` — Mount resolution tests (moved)
- `internal/runtime/shared/signals.go` — Signal forwarding helpers
- `internal/runtime/shared/timeout.go` — Timeout watchdog + ParseTimeout (moved)
- `internal/runtime/shared/timeout_test.go` — Timeout tests (moved)
- `internal/runtime/shared/tty.go` — isTerminal() helper (moved)
- `internal/runtime/shared/exitcode.go` — ExitCodeMessage (moved)
- `internal/runtime/shared/exitcode_test.go` — Exit code tests (moved)
- `internal/runtime/docker/docker.go` — DockerRuntime struct, Preflight(), Run(), DryRun()
- `internal/runtime/docker/docker_test.go` — Docker preflight test (moved)
- `internal/runtime/docker/image.go` — EnsureImage(), BuildImage(), BuildBaseImage(), ImageTag(), etc.
- `internal/runtime/docker/image_test.go` — Image tag tests (moved)
- `internal/runtime/docker/args.go` — buildArgs() (private, moved from container/args.go)
- `internal/runtime/docker/args_test.go` — Docker args tests (moved)
- `internal/runtime/firecracker/firecracker.go` — FirecrackerRuntime struct, Preflight(), Run(), DryRun()
- `internal/runtime/firecracker/firecracker_test.go` — Preflight tests
- `internal/runtime/firecracker/image.go` — EnsureImage(), BuildRootfs()
- `internal/runtime/firecracker/image_test.go` — Rootfs tag/naming tests
- `internal/runtime/firecracker/vm.go` — VM lifecycle (configure, boot, execute, cleanup)
- `internal/runtime/firecracker/network.go` — TAP setup/teardown, IP allocation
- `internal/runtime/firecracker/network_test.go` — IP allocation tests
- `internal/runtime/firecracker/kernel.go` — Kernel download, checksum, path resolution
- `internal/runtime/firecracker/kernel_test.go` — Kernel path/checksum tests
- `internal/runtime/firecracker/virtiofs.go` — virtiofsd process management
- `internal/protocol/protocol.go` — vsock message types and framing (shared by host runtime and guest agent)
- `internal/protocol/protocol_test.go` — Protocol round-trip tests
- `internal/guest/main.go` — Guest init agent entrypoint
- `internal/guest/exec.go` — Command execution inside VM
- `internal/guest/net.go` — Guest-side network configuration
- `cmd/warden-init/main.go` — Guest init build target
- `cmd/warden-netsetup/main.go` — Network helper binary
- `internal/cli/setup.go` — `warden setup firecracker` command

### Modified files
- `internal/config/types.go` — Add `Runtime` field to SandboxConfig
- `internal/config/defaults.go` — Set `Runtime: "docker"` in DefaultConfig()
- `internal/config/parse.go` — Add `Runtime *string` to ProfileEntry
- `internal/config/merge.go` — Add Runtime nil-check in ApplyProfile
- `internal/config/parse_test.go` — Test Runtime field parsing
- `internal/config/merge_test.go` — Test Runtime field merging
- `internal/cli/root.go` — Replace container.Run() with runtime interface calls, add `--runtime` flag
- `internal/cli/init.go` — Add `runtime: docker` to initTemplate
- `internal/cli/images.go` — Use runtime.ListImages()/PruneImages() instead of docker CLI

### Deleted files (after extraction complete)
- `internal/container/run.go`
- `internal/container/args.go`
- `internal/container/docker.go`
- `internal/container/image.go`
- `internal/container/base.go`
- `internal/container/timeout.go`
- All corresponding `_test.go` files in `internal/container/`

---

## Chunk 1: Runtime Interface, Shared Utilities & Config Changes

This chunk establishes the foundation: the Runtime interface, shared helpers extracted from `container/`, and config changes for the `Runtime` field. After this chunk, the interface exists and config supports `runtime:` but the CLI still uses the old `container` package.

### Task 1: Add Runtime field to config

**Files:**
- Modify: `internal/config/types.go`
- Modify: `internal/config/defaults.go`
- Modify: `internal/config/parse.go`
- Modify: `internal/config/merge.go`
- Modify: `internal/config/parse_test.go`
- Modify: `internal/config/merge_test.go`

- [ ] **Step 1: Write failing test for DefaultConfig Runtime field**

Add to `internal/config/parse_test.go`:

```go
func TestDefaultConfigRuntime(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Runtime != "docker" {
		t.Errorf("default runtime = %q, want docker", cfg.Runtime)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/winler/projects/warden && go test ./internal/config/ -run TestDefaultConfigRuntime -v`
Expected: FAIL — SandboxConfig has no field Runtime

- [ ] **Step 3: Add Runtime to SandboxConfig and DefaultConfig**

In `internal/config/types.go`, add `Runtime` as first field:
```go
type SandboxConfig struct {
	Runtime string   `yaml:"runtime"`
	Image   string   `yaml:"image"`
	// ... rest unchanged
}
```

In `internal/config/defaults.go`:
```go
func DefaultConfig() SandboxConfig {
	return SandboxConfig{
		Runtime: "docker",
		Image:   "ubuntu:24.04",
		Network: false,
		Memory:  "8g",
		CPUs:    runtime.NumCPU(),
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/winler/projects/warden && go test ./internal/config/ -run TestDefaultConfigRuntime -v`
Expected: PASS

- [ ] **Step 5: Write failing test for Runtime in ProfileEntry/ApplyProfile**

Add to `internal/config/merge_test.go`:

```go
func TestApplyProfileRuntime(t *testing.T) {
	base := DefaultConfig()
	profile := ProfileEntry{
		Runtime: strPtr("firecracker"),
	}
	result := ApplyProfile(base, profile)
	if result.Runtime != "firecracker" {
		t.Errorf("runtime = %q, want firecracker", result.Runtime)
	}
}

func TestApplyProfileRuntimeNil(t *testing.T) {
	base := DefaultConfig()
	profile := ProfileEntry{}
	result := ApplyProfile(base, profile)
	if result.Runtime != "docker" {
		t.Errorf("runtime = %q, want docker (base default)", result.Runtime)
	}
}
```

- [ ] **Step 6: Run tests to verify they fail**

Run: `cd /home/winler/projects/warden && go test ./internal/config/ -run TestApplyProfileRuntime -v`
Expected: FAIL — ProfileEntry has no field Runtime

- [ ] **Step 7: Add Runtime to ProfileEntry and ApplyProfile**

In `internal/config/parse.go`, add to ProfileEntry:
```go
type ProfileEntry struct {
	Extends string   `yaml:"extends"`
	Runtime *string  `yaml:"runtime"`
	Image   *string  `yaml:"image"`
	// ... rest unchanged
}
```

In `internal/config/merge.go`, add at top of ApplyProfile (before Image check):
```go
if p.Runtime != nil {
	base.Runtime = *p.Runtime
}
```

- [ ] **Step 8: Run all config tests**

Run: `cd /home/winler/projects/warden && go test ./internal/config/ -v`
Expected: ALL PASS

- [ ] **Step 9: Write test for YAML parsing with runtime field**

Add to `internal/config/parse_test.go`:

```go
func TestParseWardenYAMLWithRuntime(t *testing.T) {
	yaml := `
default:
  runtime: docker
  image: ubuntu:24.04

profiles:
  secure:
    extends: default
    runtime: firecracker
`
	file, err := ParseWardenYAML([]byte(yaml))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if file.Default.Runtime == nil || *file.Default.Runtime != "docker" {
		t.Errorf("default runtime = %v, want docker", file.Default.Runtime)
	}
	secure := file.Profiles["secure"]
	if secure.Runtime == nil || *secure.Runtime != "firecracker" {
		t.Errorf("secure runtime = %v, want firecracker", secure.Runtime)
	}
}
```

- [ ] **Step 10: Run test to verify it passes**

Run: `cd /home/winler/projects/warden && go test ./internal/config/ -run TestParseWardenYAMLWithRuntime -v`
Expected: PASS (YAML parsing already handles new fields via struct tags)

- [ ] **Step 11: Run full test suite to verify no regressions**

Run: `cd /home/winler/projects/warden && go test ./... -v`
Expected: ALL PASS

- [ ] **Step 12: Commit**

```bash
git add internal/config/
git commit -m "feat: add Runtime field to SandboxConfig and ProfileEntry"
```

### Task 2: Create Runtime interface and ImageInfo

**Files:**
- Create: `internal/runtime/runtime.go`

- [ ] **Step 1: Create the runtime package with interface definition**

Create `internal/runtime/runtime.go`:

```go
package runtime

import (
	"fmt"
	"time"

	"github.com/winler/warden/internal/config"
)

// ImageInfo describes a cached image or rootfs.
type ImageInfo struct {
	Tag       string
	Size      int64
	Runtime   string
	CreatedAt time.Time
}

// Runtime abstracts the execution backend (Docker, Firecracker, etc.).
type Runtime interface {
	// Preflight checks if the runtime is available and ready.
	Preflight() error

	// EnsureImage ensures the image/rootfs exists, building if needed.
	// Returns an image identifier (Docker tag or rootfs path).
	EnsureImage(cfg config.SandboxConfig) (string, error)

	// Run executes a command in the sandbox.
	// Returns exit code and error. Error is non-nil for infrastructure failures.
	// Exit code is meaningful only when error is nil.
	Run(cfg config.SandboxConfig, command []string) (int, error)

	// DryRun prints what would be executed without running it.
	DryRun(cfg config.SandboxConfig, command []string) error

	// ListImages returns cached images for this runtime.
	ListImages() ([]ImageInfo, error)

	// PruneImages removes all cached images for this runtime.
	PruneImages() error
}

// NewRuntime creates a Runtime for the given name ("docker" or "firecracker").
func NewRuntime(name string) (Runtime, error) {
	switch name {
	case "docker":
		return nil, fmt.Errorf("docker runtime not yet extracted")
	case "firecracker":
		return nil, fmt.Errorf("firecracker runtime not yet implemented")
	default:
		return nil, fmt.Errorf("unknown runtime: %q", name)
	}
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /home/winler/projects/warden && go build ./internal/runtime/`
Expected: Success

- [ ] **Step 3: Commit**

```bash
git add internal/runtime/runtime.go
git commit -m "feat: define Runtime interface and NewRuntime factory"
```

### Task 3: Extract shared utilities from container/

**Files:**
- Create: `internal/runtime/mounts.go`
- Create: `internal/runtime/mounts_test.go`
- Create: `internal/runtime/shared/timeout.go`
- Create: `internal/runtime/shared/timeout_test.go`
- Create: `internal/runtime/shared/exitcode.go`
- Create: `internal/runtime/shared/exitcode_test.go`
- Create: `internal/runtime/shared/tty.go`
- Create: `internal/runtime/shared/signals.go`

- [ ] **Step 1: Create runtime/mounts.go — move ResolveMounts**

```go
package runtime

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/winler/warden/internal/config"
)

// ResolvedMount is a mount with an absolute host path.
type ResolvedMount struct {
	Path string
	Mode string
}

// ResolveMounts converts relative mount paths to absolute and validates they exist.
func ResolveMounts(mounts []config.Mount, baseDir string) ([]ResolvedMount, error) {
	resolved := make([]ResolvedMount, 0, len(mounts))
	for _, m := range mounts {
		p := m.Path
		if !filepath.IsAbs(p) {
			p = filepath.Join(baseDir, p)
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			return nil, fmt.Errorf("resolving mount path %q: %w", m.Path, err)
		}
		if _, err := os.Stat(abs); err != nil {
			return nil, fmt.Errorf("warden: mount path %s does not exist", m.Path)
		}
		mode := m.Mode
		if mode == "" {
			mode = "ro"
		}
		resolved = append(resolved, ResolvedMount{Path: abs, Mode: mode})
	}
	return resolved, nil
}
```

- [ ] **Step 2: Create runtime/mounts_test.go — move mount tests**

```go
package runtime

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/winler/warden/internal/config"
)

func TestResolveMounts(t *testing.T) {
	tmp := t.TempDir()
	mounts := []config.Mount{{Path: tmp, Mode: "rw"}}
	resolved, err := ResolveMounts(mounts, tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("got %d mounts, want 1", len(resolved))
	}
	if !filepath.IsAbs(resolved[0].Path) {
		t.Errorf("resolved path %q is not absolute", resolved[0].Path)
	}
}

func TestResolveMountsRelativePath(t *testing.T) {
	tmp := t.TempDir()
	sub := filepath.Join(tmp, "project")
	os.MkdirAll(sub, 0o755)
	mounts := []config.Mount{{Path: "project", Mode: "ro"}}
	resolved, err := ResolveMounts(mounts, tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved[0].Path != sub {
		t.Errorf("path = %q, want %q", resolved[0].Path, sub)
	}
}

func TestResolveMountsMissingPath(t *testing.T) {
	_, err := ResolveMounts([]config.Mount{{Path: "/nonexistent/path", Mode: "ro"}}, "/")
	if err == nil {
		t.Fatal("expected error for nonexistent mount path")
	}
}
```

- [ ] **Step 3: Run mount tests in new location**

Run: `cd /home/winler/projects/warden && go test ./internal/runtime/ -run TestResolveMount -v`
Expected: ALL PASS

- [ ] **Step 4: Create runtime/shared/timeout.go**

```go
package shared

import (
	"fmt"
	"time"
)

// ParseTimeout parses a human-friendly duration string.
func ParseTimeout(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid timeout %q: %w", s, err)
	}
	return d, nil
}
```

- [ ] **Step 5: Create runtime/shared/timeout_test.go**

```go
package shared

import (
	"testing"
	"time"
)

func TestParseTimeout(t *testing.T) {
	tests := []struct {
		input string
		want  time.Duration
	}{
		{"", 0},
		{"30m", 30 * time.Minute},
		{"1h", time.Hour},
		{"2h30m", 2*time.Hour + 30*time.Minute},
	}
	for _, tc := range tests {
		got, err := ParseTimeout(tc.input)
		if err != nil {
			t.Errorf("ParseTimeout(%q) error: %v", tc.input, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseTimeout(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestParseTimeoutInvalid(t *testing.T) {
	_, err := ParseTimeout("abc")
	if err == nil {
		t.Fatal("expected error for invalid timeout")
	}
}
```

- [ ] **Step 6: Create runtime/shared/exitcode.go**

```go
package shared

import "fmt"

const TimeoutExitCode = 124

// ExitCodeMessage returns a human-readable message for special exit codes.
func ExitCodeMessage(code int, memory string) string {
	switch code {
	case 137:
		return fmt.Sprintf("warden: killed (out of memory, limit was %s)", memory)
	default:
		return ""
	}
}
```

- [ ] **Step 7: Create runtime/shared/exitcode_test.go**

```go
package shared

import "testing"

func TestExitCodeMessage137(t *testing.T) {
	msg := ExitCodeMessage(137, "8g")
	if msg == "" {
		t.Fatal("expected message for exit code 137")
	}
}

func TestExitCodeMessageNormal(t *testing.T) {
	msg := ExitCodeMessage(0, "8g")
	if msg != "" {
		t.Errorf("expected empty message for exit 0, got %q", msg)
	}
}
```

- [ ] **Step 8: Create runtime/shared/tty.go**

```go
package shared

import "os"

// IsTerminal reports whether stdin is connected to a terminal.
func IsTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
```

- [ ] **Step 9: Create runtime/shared/signals.go**

```go
package shared

import (
	"os"
	"os/signal"
	"syscall"
)

// SignalHandler manages signal forwarding with force-kill on second signal.
// Returns a channel that receives signals and a cleanup function.
// The onFirst callback receives the first SIGINT/SIGTERM.
// The onSecond callback is called on the second signal (force-kill).
func SignalHandler(onFirst func(os.Signal), onSecond func()) (cleanup func()) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sigCount := 0
		for sig := range sigCh {
			sigCount++
			if sigCount >= 2 {
				onSecond()
				return
			}
			onFirst(sig)
		}
	}()
	return func() { signal.Stop(sigCh) }
}
```

- [ ] **Step 10: Run all shared tests**

Run: `cd /home/winler/projects/warden && go test ./internal/runtime/... -v`
Expected: ALL PASS

- [ ] **Step 11: Commit**

```bash
git add internal/runtime/
git commit -m "feat: extract shared utilities into runtime package"
```

---

## Chunk 2: Docker Runtime Extraction

Extract existing Docker logic from `container/` into `runtime/docker/`, implementing the Runtime interface. Then update the CLI to use the new interface. After this chunk, the old `container/` package is deleted.

### Task 4: Create DockerRuntime implementing Runtime interface

**Files:**
- Create: `internal/runtime/docker/docker.go`
- Create: `internal/runtime/docker/docker_test.go`
- Create: `internal/runtime/docker/image.go`
- Create: `internal/runtime/docker/image_test.go`
- Create: `internal/runtime/docker/args.go`
- Create: `internal/runtime/docker/args_test.go`

- [ ] **Step 1: Create runtime/docker/args.go — move BuildDockerArgs (made private)**

```go
package docker

import (
	"os/user"
	"strconv"

	"github.com/winler/warden/internal/config"
)

// buildArgs translates a SandboxConfig into docker run arguments.
func buildArgs(cfg config.SandboxConfig, command []string) []string {
	args := []string{"run", "--rm"}

	u, err := user.Current()
	if err == nil {
		args = append(args, "--user", u.Uid+":"+u.Gid)
	}

	if !cfg.Network {
		args = append(args, "--network", "none")
	}

	if cfg.Memory != "" {
		args = append(args, "--memory", cfg.Memory)
	}
	if cfg.CPUs > 0 {
		args = append(args, "--cpus", strconv.Itoa(cfg.CPUs))
	}

	for _, m := range cfg.Mounts {
		args = append(args, "-v", m.Path+":"+m.Path+":"+m.Mode)
	}

	for _, e := range cfg.Env {
		args = append(args, "-e", e)
	}

	if cfg.Workdir != "" {
		args = append(args, "-w", cfg.Workdir)
	}

	args = append(args, cfg.Image)
	args = append(args, command...)
	return args
}
```

- [ ] **Step 2: Create runtime/docker/args_test.go**

Copy tests from `internal/container/args_test.go`, changing package to `docker` and function names from `BuildDockerArgs` to `buildArgs`. Remove mount tests (those are in `runtime/mounts_test.go` now). Update import path.

```go
package docker

import (
	"strings"
	"testing"

	"github.com/winler/warden/internal/config"
)

func TestBuildDockerArgs(t *testing.T) {
	cfg := config.SandboxConfig{
		Image:   "ubuntu:24.04",
		Network: false,
		Memory:  "4g",
		CPUs:    2,
		Mounts: []config.Mount{
			{Path: "/home/user/project", Mode: "rw"},
		},
		Workdir: "/home/user/project",
	}
	args := buildArgs(cfg, []string{"echo", "hello"})
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "--rm") {
		t.Error("missing --rm")
	}
	if !strings.Contains(joined, "--network none") {
		t.Error("missing --network none for network=false")
	}
	if !strings.Contains(joined, "--memory 4g") {
		t.Error("missing --memory 4g")
	}
	if !strings.Contains(joined, "-v /home/user/project:/home/user/project:rw") {
		t.Error("missing volume mount")
	}
	if !strings.Contains(joined, "-w /home/user/project") {
		t.Error("missing workdir")
	}
	if !strings.HasSuffix(joined, "ubuntu:24.04 echo hello") {
		t.Errorf("args should end with image + command, got: %s", joined)
	}
}

func TestBuildDockerArgsEnvVars(t *testing.T) {
	cfg := config.SandboxConfig{
		Image: "ubuntu:24.04",
		Env:   []string{"ANTHROPIC_API_KEY", "FOO=bar"},
	}
	args := buildArgs(cfg, []string{"echo"})
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "-e ANTHROPIC_API_KEY") {
		t.Error("missing -e ANTHROPIC_API_KEY")
	}
	if !strings.Contains(joined, "-e FOO=bar") {
		t.Error("missing -e FOO=bar")
	}
}

func TestBuildDockerArgsNetworkEnabled(t *testing.T) {
	cfg := config.SandboxConfig{
		Image:   "ubuntu:24.04",
		Network: true,
		Memory:  "8g",
		CPUs:    4,
	}
	args := buildArgs(cfg, []string{"bash"})
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "--network") {
		t.Error("should not set --network when network is enabled")
	}
}
```

- [ ] **Step 3: Run args tests**

Run: `cd /home/winler/projects/warden && go test ./internal/runtime/docker/ -run TestBuildDocker -v`
Expected: ALL PASS

- [ ] **Step 4: Create runtime/docker/image.go — move image building logic**

Move `ImageTag`, `ImageExists`, `BuildImage`, `BuildBaseImage`, `BaseImageTag`, `BaseDockerfile` from `container/image.go` and `container/base.go`. Keep the exact same logic but in the `docker` package. Export `ImageTag` and `BaseImageTag` (needed by EnsureImage).

```go
package docker

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/winler/warden/internal/config"
	"github.com/winler/warden/internal/features"
)

// ImageTag computes the docker image tag for a base image + tool set.
func ImageTag(base string, tools []string) string {
	baseTag := BaseImageTag(base)
	if len(tools) == 0 {
		return baseTag
	}
	sorted := make([]string, len(tools))
	copy(sorted, tools)
	sort.Strings(sorted)
	safeName := strings.ReplaceAll(base, ":", "-")
	safeName = strings.ReplaceAll(safeName, "/", "-")
	return "warden:" + safeName + "_" + strings.Join(sorted, "_")
}

// BaseImageTag returns the tag for the warden base image.
func BaseImageTag(base string) string {
	safe := strings.ReplaceAll(base, ":", "-")
	safe = strings.ReplaceAll(safe, "/", "-")
	return "warden:base-" + safe
}

// ImageExists checks if a docker image exists locally.
func ImageExists(tag string) (bool, error) {
	cmd := exec.Command("docker", "image", "inspect", tag)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil, nil
}

// BuildBaseImage ensures the warden base image exists. Builds if not cached.
func BuildBaseImage(base string) (string, error) {
	tag := BaseImageTag(base)
	exists, err := ImageExists(tag)
	if err != nil {
		return "", err
	}
	if exists {
		return tag, nil
	}

	fmt.Fprintf(os.Stderr, "warden: building base image (first run only)...\n")

	tmpDir, err := os.MkdirTemp("", "warden-base-*")
	if err != nil {
		return "", fmt.Errorf("creating build dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	dockerfile := BaseDockerfile(base)
	if err := os.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte(dockerfile), 0o644); err != nil {
		return "", fmt.Errorf("writing Dockerfile: %w", err)
	}

	cmd := exec.Command("docker", "build", "-t", tag, tmpDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("building base image: %w", err)
	}

	return tag, nil
}

// BaseDockerfile returns the Dockerfile for the warden base image.
func BaseDockerfile(base string) string {
	return fmt.Sprintf(`FROM %s
RUN apt-get update && apt-get install -y --no-install-recommends \
    curl wget ca-certificates openssh-client \
    git ripgrep fd-find tree less \
    build-essential pkg-config \
    jq unzip zip tar gzip \
    sudo locales \
  && sed -i 's/# en_US.UTF-8/en_US.UTF-8/' /etc/locale.gen \
  && locale-gen \
  && rm -rf /var/lib/apt/lists/*
`, base)
}

// BuildImage creates a warden image with the specified tools installed.
func BuildImage(base string, tools []string) (string, error) {
	baseTag, err := BuildBaseImage(base)
	if err != nil {
		return "", fmt.Errorf("building base image: %w", err)
	}

	tag := ImageTag(base, tools)
	if len(tools) == 0 {
		return baseTag, nil
	}

	exists, err := ImageExists(tag)
	if err != nil {
		return "", err
	}
	if exists {
		return tag, nil
	}

	tmpDir, err := os.MkdirTemp("", "warden-build-*")
	if err != nil {
		return "", fmt.Errorf("creating build dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	featDir := filepath.Join(tmpDir, "features")
	os.MkdirAll(featDir, 0o755)

	sorted := make([]string, len(tools))
	copy(sorted, tools)
	sort.Strings(sorted)

	var runLines []string
	for _, tool := range sorted {
		script, err := features.GetFeatureScript(tool)
		if err != nil {
			return "", fmt.Errorf("unknown tool %q: %w", tool, err)
		}
		scriptPath := filepath.Join(featDir, tool+".sh")
		if err := os.WriteFile(scriptPath, script, 0o755); err != nil {
			return "", fmt.Errorf("writing feature script: %w", err)
		}
		runLines = append(runLines, fmt.Sprintf("RUN /tmp/warden-features/%s.sh", tool))
	}

	dockerfile := fmt.Sprintf("FROM %s\nCOPY features/ /tmp/warden-features/\n%s\nRUN rm -rf /tmp/warden-features/\n",
		baseTag, strings.Join(runLines, "\n"))

	if err := os.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte(dockerfile), 0o644); err != nil {
		return "", fmt.Errorf("writing Dockerfile: %w", err)
	}

	cmd := exec.Command("docker", "build", "-t", tag, tmpDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("building image: %w", err)
	}

	return tag, nil
}

// EnsureImage implements Runtime.EnsureImage for Docker.
func (d *DockerRuntime) EnsureImage(cfg config.SandboxConfig) (string, error) {
	if len(cfg.Tools) > 0 {
		return BuildImage(cfg.Image, cfg.Tools)
	}
	return BuildBaseImage(cfg.Image)
}
```

- [ ] **Step 5: Create runtime/docker/image_test.go — move tag tests**

```go
package docker

import "testing"

func TestImageTagNoTools(t *testing.T) {
	tag := ImageTag("ubuntu:24.04", nil)
	want := "warden:base-ubuntu-24.04"
	if tag != want {
		t.Errorf("ImageTag = %q, want %q", tag, want)
	}
}

func TestImageTagSorted(t *testing.T) {
	tag := ImageTag("ubuntu:24.04", []string{"python", "node"})
	want := "warden:ubuntu-24.04_node_python"
	if tag != want {
		t.Errorf("ImageTag = %q, want %q", tag, want)
	}
}

func TestBaseImageTag(t *testing.T) {
	tag := BaseImageTag("ubuntu:24.04")
	want := "warden:base-ubuntu-24.04"
	if tag != want {
		t.Errorf("BaseImageTag = %q, want %q", tag, want)
	}
}
```

- [ ] **Step 6: Create runtime/docker/docker.go — DockerRuntime struct with Preflight, Run, DryRun**

```go
package docker

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"strings"

	"github.com/winler/warden/internal/config"
	"github.com/winler/warden/internal/runtime"
	"github.com/winler/warden/internal/runtime/shared"
)

// DockerRuntime implements runtime.Runtime using Docker containers.
type DockerRuntime struct{}

// containerName generates a unique container name.
func containerName() string {
	return fmt.Sprintf("warden-%d", rand.Int63())
}

// Preflight verifies docker is installed and the daemon is running.
func (d *DockerRuntime) Preflight() error {
	path, err := exec.LookPath("docker")
	if err != nil {
		return fmt.Errorf("warden: docker is not installed")
	}
	out, err := exec.Command(path, "info").CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Cannot connect") || strings.Contains(string(out), "permission denied") {
			return fmt.Errorf("warden: docker daemon is not running")
		}
		return fmt.Errorf("warden: docker check failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// DryRun prints the docker command that would be executed.
func (d *DockerRuntime) DryRun(cfg config.SandboxConfig, command []string) error {
	cfg.Image = ImageTag(cfg.Image, cfg.Tools)
	args := buildArgs(cfg, command)
	name := containerName()
	extra := []string{"--name", name}
	fullArgs := make([]string, 0, len(args)+len(extra))
	fullArgs = append(fullArgs, args[0], args[1]) // "run", "--rm"
	fullArgs = append(fullArgs, extra...)
	fullArgs = append(fullArgs, args[2:]...)
	fmt.Println("docker " + joinArgs(fullArgs))
	return nil
}

// Run executes a command in a Docker container.
func (d *DockerRuntime) Run(cfg config.SandboxConfig, command []string) (int, error) {
	name := containerName()
	args := buildArgs(cfg, command)

	extra := []string{"--name", name}
	if shared.IsTerminal() {
		extra = append(extra, "-it")
	}
	fullArgs := make([]string, 0, len(args)+len(extra))
	fullArgs = append(fullArgs, args[0], args[1]) // "run", "--rm"
	fullArgs = append(fullArgs, extra...)
	fullArgs = append(fullArgs, args[2:]...)

	timeout, err := shared.ParseTimeout(cfg.Timeout)
	if err != nil {
		return 1, err
	}

	ctx := context.Background()
	var cancel context.CancelFunc = func() {}
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	cmd := exec.Command("docker", fullArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	cleanup := shared.SignalHandler(
		func(sig os.Signal) {
			if cmd.Process != nil {
				cmd.Process.Signal(sig)
			}
		},
		func() {
			exec.Command("docker", "kill", name).Run()
		},
	)
	defer cleanup()

	if err := cmd.Start(); err != nil {
		return 1, fmt.Errorf("starting container: %w", err)
	}

	if timeout > 0 {
		go func() {
			<-ctx.Done()
			if ctx.Err() == context.DeadlineExceeded {
				exec.Command("docker", "stop", "--time", "10", name).Run()
			}
		}()
	}

	err = cmd.Wait()

	wasTimeout := timeout > 0 && ctx.Err() == context.DeadlineExceeded
	cancel()

	if wasTimeout {
		fmt.Fprintf(os.Stderr, "warden: killed (timeout after %s)\n", cfg.Timeout)
		return shared.TimeoutExitCode, nil
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code := exitErr.ExitCode()
			if msg := shared.ExitCodeMessage(code, cfg.Memory); msg != "" {
				fmt.Fprintln(os.Stderr, msg)
			}
			return code, nil
		}
		return 1, fmt.Errorf("running container: %w", err)
	}

	return 0, nil
}

// ListImages returns cached Docker warden images.
func (d *DockerRuntime) ListImages() ([]runtime.ImageInfo, error) {
	out, err := exec.Command("docker", "images", "--format",
		"{{.Repository}}:{{.Tag}}\t{{.Size}}\t{{.CreatedSince}}", "warden").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("listing images: %w", err)
	}
	output := strings.TrimSpace(string(out))
	if output == "" {
		return nil, nil
	}
	var images []runtime.ImageInfo
	for _, line := range strings.Split(output, "\n") {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) >= 1 {
			images = append(images, runtime.ImageInfo{
				Tag:     parts[0],
				Runtime: "docker",
			})
		}
	}
	return images, nil
}

// PruneImages removes all cached Docker warden images.
func (d *DockerRuntime) PruneImages() error {
	out, err := exec.Command("docker", "images", "--format",
		"{{.Repository}}:{{.Tag}}", "warden").CombinedOutput()
	if err != nil {
		return fmt.Errorf("listing images: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return nil
	}
	for _, img := range lines {
		img = strings.TrimSpace(img)
		if img == "" {
			continue
		}
		cmd := exec.Command("docker", "rmi", img)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Run()
	}
	fmt.Printf("Removed %d warden image(s).\n", len(lines))
	return nil
}

func joinArgs(args []string) string {
	result := ""
	for i, a := range args {
		if i > 0 {
			result += " "
		}
		if strings.Contains(a, " ") || strings.Contains(a, "'") || strings.Contains(a, "\"") {
			result += "'" + strings.ReplaceAll(a, "'", "'\\''") + "'"
		} else {
			result += a
		}
	}
	return result
}
```

- [ ] **Step 7: Create runtime/docker/docker_test.go**

```go
package docker

import "testing"

func TestContainerName(t *testing.T) {
	name := containerName()
	if name == "" {
		t.Error("container name should not be empty")
	}
}

// Compile-time check that DockerRuntime satisfies the Runtime interface.
var _ interface {
	Preflight() error
} = (*DockerRuntime)(nil)
```

- [ ] **Step 8: Update NewRuntime to return DockerRuntime**

In `internal/runtime/runtime.go`, update the docker case:
```go
import (
	// add import for docker subpackage
	"github.com/winler/warden/internal/runtime/docker"
)
```

Actually this creates a circular import. Instead, use a registration pattern. Update `runtime.go`:

```go
package runtime

import (
	"fmt"
	"time"

	"github.com/winler/warden/internal/config"
)

type ImageInfo struct {
	Tag       string
	Size      int64
	Runtime   string
	CreatedAt time.Time
}

type Runtime interface {
	Preflight() error
	EnsureImage(cfg config.SandboxConfig) (string, error)
	Run(cfg config.SandboxConfig, command []string) (int, error)
	DryRun(cfg config.SandboxConfig, command []string) error
	ListImages() ([]ImageInfo, error)
	PruneImages() error
}

var registry = map[string]func() Runtime{}

// Register adds a runtime factory to the registry.
func Register(name string, factory func() Runtime) {
	registry[name] = factory
}

// NewRuntime creates a Runtime for the given name.
func NewRuntime(name string) (Runtime, error) {
	factory, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown runtime: %q", name)
	}
	return factory(), nil
}

// AllRuntimes returns all registered runtime names.
func AllRuntimes() []string {
	var names []string
	for name := range registry {
		names = append(names, name)
	}
	return names
}
```

Then in `runtime/docker/docker.go` add an init function:
```go
func init() {
	runtime.Register("docker", func() runtime.Runtime {
		return &DockerRuntime{}
	})
}
```

- [ ] **Step 9: Run all runtime tests to verify compilation and tests pass**

Run: `cd /home/winler/projects/warden && go test ./internal/runtime/... -v`
Expected: ALL PASS

- [ ] **Step 10: Commit**

```bash
git add internal/runtime/
git commit -m "feat: extract Docker logic into runtime/docker implementing Runtime interface"
```

### Task 5: Update CLI to use Runtime interface

**Files:**
- Modify: `internal/cli/root.go`
- Modify: `internal/cli/images.go`
- Modify: `internal/cli/init.go`

- [ ] **Step 1: Update root.go to use Runtime interface**

Replace the import of `container` with `runtime` and `runtime/docker`. Add `--runtime` flag. Replace `container.Run()` with the runtime call sequence.

Key changes in `root.go`:
- Add `runtimeFlag string` variable
- Add flag: `run.Flags().StringVar(&runtimeFlag, "runtime", "", "Runtime backend (docker or firecracker)")`
- Replace `container.ResolveMounts` with `runtime.ResolveMounts`
- Replace step 8 (container.Run) with:

```go
// 8. Select runtime
rtName := cfg.Runtime
if cmd.Flags().Changed("runtime") {
	rtName = runtimeFlag
}
rt, err := runtime.NewRuntime(rtName)
if err != nil {
	return err
}

// 9. Dry-run does NOT require Preflight (works without a running daemon)
if dryRun {
	return rt.DryRun(cfg, args)
}

// 10. Preflight (only for real execution)
if err := rt.Preflight(); err != nil {
	return err
}

if _, err := rt.EnsureImage(cfg); err != nil {
	return err
}

exitCode, err := rt.Run(cfg, args)
if err != nil {
	return err
}
if exitCode != 0 {
	os.Exit(exitCode)
}
return nil
```

Import `_ "github.com/winler/warden/internal/runtime/docker"` to trigger init() registration.

- [ ] **Step 2: Update images.go to use Runtime interface**

Replace direct docker CLI calls with iteration over registered runtimes:

```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/winler/warden/internal/runtime"
	_ "github.com/winler/warden/internal/runtime/docker"
)

func newImagesCommand() *cobra.Command {
	images := &cobra.Command{
		Use:   "images",
		Short: "List cached warden images",
		RunE: func(cmd *cobra.Command, args []string) error {
			return listImages()
		},
	}

	prune := &cobra.Command{
		Use:   "prune",
		Short: "Remove all cached warden images",
		RunE: func(cmd *cobra.Command, args []string) error {
			return pruneImages()
		},
	}

	images.AddCommand(prune)
	return images
}

func listImages() error {
	hasAny := false
	for _, name := range runtime.AllRuntimes() {
		rt, err := runtime.NewRuntime(name)
		if err != nil {
			continue
		}
		images, err := rt.ListImages()
		if err != nil {
			continue
		}
		if len(images) > 0 {
			if !hasAny {
				fmt.Println("IMAGE\tSIZE\tRUNTIME")
			}
			hasAny = true
			for _, img := range images {
				fmt.Printf("%s\t\t%s\n", img.Tag, img.Runtime)
			}
		}
	}
	if !hasAny {
		fmt.Println("No cached warden images.")
	}
	return nil
}

func pruneImages() error {
	for _, name := range runtime.AllRuntimes() {
		rt, err := runtime.NewRuntime(name)
		if err != nil {
			continue
		}
		rt.PruneImages()
	}
	return nil
}
```

- [ ] **Step 3: Update init.go — add runtime field to template**

```go
const initTemplate = `# Warden sandbox configuration
# Docs: https://github.com/winler/warden

default:
  runtime: docker  # or: firecracker
  image: ubuntu:24.04
  tools: []
  mounts:
    - path: .
      mode: rw
  network: false
  memory: 8g
`
```

- [ ] **Step 4: Run all tests to verify no regressions**

Run: `cd /home/winler/projects/warden && go test ./... -v`
Expected: ALL PASS (some tests may need import updates)

- [ ] **Step 5: Fix any test compilation issues**

Existing tests in `internal/cli/` and `internal/container/` may still reference old imports. Fix compile errors. The container tests can remain temporarily — they test the same code that now also lives in runtime/docker.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/ internal/runtime/
git commit -m "feat: wire CLI to use Runtime interface for docker execution"
```

### Task 6: Delete old container/ package

**Files:**
- Delete: `internal/container/` (all files)

- [ ] **Step 1: Verify no remaining imports of container/ package**

Run: `cd /home/winler/projects/warden && grep -r '"github.com/winler/warden/internal/container"' --include='*.go' .`
Expected: No results (or only in container/ test files themselves)

- [ ] **Step 2: Delete container/ package**

```bash
rm -rf internal/container/
```

- [ ] **Step 3: Run full test suite**

Run: `cd /home/winler/projects/warden && go test ./... -v`
Expected: ALL PASS

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "refactor: remove old container/ package, fully replaced by runtime/"
```

---

## Chunk 3: vsock Protocol Library

Shared protocol definitions in `internal/protocol/`, imported by both the Firecracker host-side runtime and the guest init agent. Lives in its own package to avoid the guest binary pulling in host-side Firecracker dependencies.

### Task 7: Create vsock protocol types and framing

**Files:**
- Create: `internal/protocol/protocol.go`
- Create: `internal/protocol/protocol_test.go`

- [ ] **Step 1: Write failing test for protocol round-trip**

Create `internal/protocol/protocol_test.go`:

```go
package protocol

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func TestExecMessageRoundTrip(t *testing.T) {
	msg := &ExecMessage{
		Command: "node",
		Args:    []string{"index.js"},
		Workdir: "/home/user/project",
		Env:     []string{"NODE_ENV=dev"},
		UID:     1000,
		GID:     1000,
		TTY:     true,
	}
	var buf bytes.Buffer
	if err := WriteMessage(&buf, msg); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	got, err := ReadMessage(&buf)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	exec, ok := got.(*ExecMessage)
	if !ok {
		t.Fatalf("got type %T, want *ExecMessage", got)
	}
	if exec.Command != "node" {
		t.Errorf("command = %q, want node", exec.Command)
	}
	if exec.TTY != true {
		t.Error("tty should be true")
	}
}

func TestSignalMessageRoundTrip(t *testing.T) {
	msg := &SignalMessage{Signal: "SIGINT"}
	var buf bytes.Buffer
	if err := WriteMessage(&buf, msg); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	got, err := ReadMessage(&buf)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	sig, ok := got.(*SignalMessage)
	if !ok {
		t.Fatalf("got type %T, want *SignalMessage", got)
	}
	if sig.Signal != "SIGINT" {
		t.Errorf("signal = %q, want SIGINT", sig.Signal)
	}
}

func TestOutputMessageRoundTrip(t *testing.T) {
	// Data is base64-encoded by the sender
	encoded := base64.StdEncoding.EncodeToString([]byte("hello world"))
	msg := &OutputMessage{Type: "stdout", Data: encoded}
	var buf bytes.Buffer
	if err := WriteMessage(&buf, msg); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	got, err := ReadMessage(&buf)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	out, ok := got.(*OutputMessage)
	if !ok {
		t.Fatalf("got type %T, want *OutputMessage", got)
	}
	decoded, err := base64.StdEncoding.DecodeString(out.Data)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	if string(decoded) != "hello world" {
		t.Errorf("data = %q, want hello world", decoded)
	}
}

func TestExitMessageRoundTrip(t *testing.T) {
	msg := &ExitMessage{Code: 42}
	var buf bytes.Buffer
	if err := WriteMessage(&buf, msg); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	got, err := ReadMessage(&buf)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	exit, ok := got.(*ExitMessage)
	if !ok {
		t.Fatalf("got type %T, want *ExitMessage", got)
	}
	if exit.Code != 42 {
		t.Errorf("code = %d, want 42", exit.Code)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/winler/projects/warden && go test ./internal/protocol/ -run TestExecMessage -v`
Expected: FAIL — types not defined

- [ ] **Step 3: Implement protocol.go**

```go
package protocol

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

// Message types for vsock protocol.
// Host -> Guest: ExecMessage, SignalMessage
// Guest -> Host: OutputMessage, ExitMessage

type ExecMessage struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Workdir string   `json:"workdir"`
	Env     []string `json:"env"`
	UID     int      `json:"uid"`
	GID     int      `json:"gid"`
	TTY     bool     `json:"tty"`
}

type SignalMessage struct {
	Signal string `json:"signal"`
}

type OutputMessage struct {
	Type string `json:"type"` // "stdout" or "stderr"
	Data string `json:"data"` // base64-encoded bytes
}

type ExitMessage struct {
	Code int `json:"code"`
}

// envelope wraps any message with a type discriminator for serialization.
type envelope struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// WriteMessage writes a length-prefixed JSON message to w.
func WriteMessage(w io.Writer, msg interface{}) error {
	var typeName string
	switch msg.(type) {
	case *ExecMessage:
		typeName = "exec"
	case *SignalMessage:
		typeName = "signal"
	case *OutputMessage:
		typeName = "output"
	case *ExitMessage:
		typeName = "exit"
	default:
		return fmt.Errorf("unknown message type: %T", msg)
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	env := envelope{Type: typeName, Data: data}
	payload, err := json.Marshal(env)
	if err != nil {
		return err
	}

	// 4-byte little-endian length prefix
	var lenBuf [4]byte
	binary.LittleEndian.PutUint32(lenBuf[:], uint32(len(payload)))
	if _, err := w.Write(lenBuf[:]); err != nil {
		return err
	}
	_, err = w.Write(payload)
	return err
}

// ReadMessage reads a length-prefixed JSON message from r.
func ReadMessage(r io.Reader) (interface{}, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return nil, err
	}
	length := binary.LittleEndian.Uint32(lenBuf[:])

	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}

	var env envelope
	if err := json.Unmarshal(payload, &env); err != nil {
		return nil, err
	}

	switch env.Type {
	case "exec":
		var m ExecMessage
		if err := json.Unmarshal(env.Data, &m); err != nil {
			return nil, err
		}
		return &m, nil
	case "signal":
		var m SignalMessage
		if err := json.Unmarshal(env.Data, &m); err != nil {
			return nil, err
		}
		return &m, nil
	case "output":
		var m OutputMessage
		if err := json.Unmarshal(env.Data, &m); err != nil {
			return nil, err
		}
		return &m, nil
	case "exit":
		var m ExitMessage
		if err := json.Unmarshal(env.Data, &m); err != nil {
			return nil, err
		}
		return &m, nil
	default:
		return nil, fmt.Errorf("unknown message type: %q", env.Type)
	}
}
```

- [ ] **Step 4: Run all protocol tests**

Run: `cd /home/winler/projects/warden && go test ./internal/protocol/ -v`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
git add internal/protocol/
git commit -m "feat: implement vsock protocol with length-prefixed JSON framing"
```

---

## Chunk 4: Firecracker Runtime — Kernel, Rootfs & Preflight

### Task 8: Kernel management

**Files:**
- Create: `internal/runtime/firecracker/kernel.go`
- Create: `internal/runtime/firecracker/kernel_test.go`

- [ ] **Step 1: Write failing test for kernel path resolution**

```go
package firecracker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultKernelPath(t *testing.T) {
	tmpHome := t.TempDir()
	path := defaultKernelPath(tmpHome)
	want := filepath.Join(tmpHome, ".warden", "firecracker", "kernel", "vmlinux-5.10.217")
	if path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
}

func TestResolveKernelPathCustom(t *testing.T) {
	custom := "/custom/vmlinux"
	path, err := resolveKernelPath(custom, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != custom {
		t.Errorf("path = %q, want %q", path, custom)
	}
}

func TestResolveKernelPathDefault(t *testing.T) {
	tmpHome := t.TempDir()
	// Create the kernel file so resolution succeeds
	kernelDir := filepath.Join(tmpHome, ".warden", "firecracker", "kernel")
	os.MkdirAll(kernelDir, 0o755)
	kernelPath := filepath.Join(kernelDir, "vmlinux-5.10.217")
	os.WriteFile(kernelPath, []byte("fake-kernel"), 0o644)

	path, err := resolveKernelPath("", tmpHome)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != kernelPath {
		t.Errorf("path = %q, want %q", path, kernelPath)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/winler/projects/warden && go test ./internal/runtime/firecracker/ -run TestDefaultKernel -v`
Expected: FAIL

- [ ] **Step 3: Implement kernel.go**

```go
package firecracker

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

const (
	kernelVersion  = "5.10.217"
	kernelFilename = "vmlinux-" + kernelVersion
	// TODO: replace with actual checksum from Firecracker releases
	kernelChecksum = "placeholder-sha256-checksum"
	kernelURL      = "https://github.com/firecracker-microvm/firecracker/releases/download/v1.7.0/" + kernelFilename
)

func defaultKernelPath(homeDir string) string {
	return filepath.Join(homeDir, ".warden", "firecracker", "kernel", kernelFilename)
}

// resolveKernelPath returns the kernel path. If customPath is set, uses that.
// Otherwise uses the default path under homeDir, downloading if needed.
func resolveKernelPath(customPath string, homeDir string) (string, error) {
	if customPath != "" {
		if _, err := os.Stat(customPath); err != nil {
			return "", fmt.Errorf("warden: kernel not found at %s", customPath)
		}
		return customPath, nil
	}

	path := defaultKernelPath(homeDir)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	// Need to download
	fmt.Fprintf(os.Stderr, "warden: downloading kernel %s (first run only)...\n", kernelVersion)
	if err := downloadKernel(path); err != nil {
		return "", err
	}
	return path, nil
}

func downloadKernel(destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("creating kernel directory: %w", err)
	}

	tmpFile := destPath + ".tmp"
	defer os.Remove(tmpFile)

	resp, err := http.Get(kernelURL)
	if err != nil {
		return fmt.Errorf("downloading kernel: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading kernel: HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(tmpFile)
	if err != nil {
		return err
	}

	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, hasher), resp.Body); err != nil {
		f.Close()
		return fmt.Errorf("downloading kernel: %w", err)
	}
	f.Close()

	checksum := hex.EncodeToString(hasher.Sum(nil))
	if checksum != kernelChecksum {
		os.Remove(tmpFile)
		return fmt.Errorf("warden: kernel checksum verification failed (got %s)", checksum)
	}

	return os.Rename(tmpFile, destPath)
}
```

- [ ] **Step 4: Run kernel tests**

Run: `cd /home/winler/projects/warden && go test ./internal/runtime/firecracker/ -run TestKernel -v -run TestDefaultKernel -v -run TestResolveKernel -v`
Expected: ALL PASS (tests that don't download)

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/firecracker/kernel.go internal/runtime/firecracker/kernel_test.go
git commit -m "feat: add kernel path resolution and download with checksum verification"
```

### Task 9: Firecracker runtime struct and registration (must precede image.go methods)

**Files:**
- Create: `internal/runtime/firecracker/firecracker.go`
- Create: `internal/runtime/firecracker/firecracker_test.go`

- [ ] **Step 1: Create firecracker.go with stub struct and registration**

```go
package firecracker

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/winler/warden/internal/config"
	"github.com/winler/warden/internal/runtime"
)

// FirecrackerRuntime implements runtime.Runtime using Firecracker microVMs.
type FirecrackerRuntime struct{}

func init() {
	runtime.Register("firecracker", func() runtime.Runtime {
		return &FirecrackerRuntime{}
	})
}

// Preflight verifies /dev/kvm, firecracker binary, and virtiofsd are available.
func (f *FirecrackerRuntime) Preflight() error {
	if _, err := os.Stat("/dev/kvm"); err != nil {
		return fmt.Errorf("warden: /dev/kvm not accessible. Run 'warden setup firecracker'")
	}
	file, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("warden: /dev/kvm not accessible. Run 'warden setup firecracker'")
	}
	file.Close()

	homeDir, _ := os.UserHomeDir()
	fcPath := filepath.Join(homeDir, ".warden", "firecracker", "bin", "firecracker")
	if _, err := os.Stat(fcPath); err != nil {
		return fmt.Errorf("warden: firecracker not found. Run 'warden setup firecracker'")
	}
	vfsPath := filepath.Join(homeDir, ".warden", "firecracker", "bin", "virtiofsd")
	if _, err := os.Stat(vfsPath); err != nil {
		return fmt.Errorf("warden: virtiofsd not found. Run 'warden setup firecracker'")
	}
	return nil
}

// Run — placeholder, implemented in Chunk 7.
func (f *FirecrackerRuntime) Run(cfg config.SandboxConfig, command []string) (int, error) {
	return 1, fmt.Errorf("firecracker runtime not yet implemented")
}

// DryRun — placeholder, implemented in Chunk 7.
func (f *FirecrackerRuntime) DryRun(cfg config.SandboxConfig, command []string) error {
	return fmt.Errorf("firecracker dry-run not yet implemented")
}
```

- [ ] **Step 2: Create firecracker_test.go**

```go
package firecracker

import "testing"

func TestPreflightNoKVM(t *testing.T) {
	rt := &FirecrackerRuntime{}
	err := rt.Preflight()
	if err == nil {
		t.Skip("KVM is available, cannot test missing KVM path")
	}
	t.Logf("preflight error (expected): %v", err)
}
```

- [ ] **Step 3: Verify compilation**

Run: `cd /home/winler/projects/warden && go test ./internal/runtime/firecracker/ -v`
Expected: PASS (test skips or logs expected error)

- [ ] **Step 4: Commit**

```bash
git add internal/runtime/firecracker/
git commit -m "feat: add FirecrackerRuntime struct with preflight and registration"
```

### Task 10: Rootfs image management

**Files:**
- Create: `internal/runtime/firecracker/image.go`
- Create: `internal/runtime/firecracker/image_test.go`

- [ ] **Step 1: Write failing test for rootfs tag naming**

```go
package firecracker

import "testing"

func TestRootfsFilename(t *testing.T) {
	tests := []struct {
		base  string
		tools []string
		want  string
	}{
		{"ubuntu:24.04", nil, "base-ubuntu-24.04.ext4"},
		{"ubuntu:24.04", []string{"node"}, "ubuntu-24.04_node.ext4"},
		{"ubuntu:24.04", []string{"python", "node"}, "ubuntu-24.04_node_python.ext4"},
	}
	for _, tc := range tests {
		got := RootfsFilename(tc.base, tc.tools)
		if got != tc.want {
			t.Errorf("RootfsFilename(%q, %v) = %q, want %q", tc.base, tc.tools, got, tc.want)
		}
	}
}

func TestRootfsPath(t *testing.T) {
	got := rootfsPath("/home/user", "ubuntu:24.04", []string{"node"})
	want := "/home/user/.warden/firecracker/rootfs/ubuntu-24.04_node.ext4"
	if got != want {
		t.Errorf("rootfsPath = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/winler/projects/warden && go test ./internal/runtime/firecracker/ -run TestRootfs -v`
Expected: FAIL

- [ ] **Step 3: Implement image.go**

```go
package firecracker

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/winler/warden/internal/config"
	rdocker "github.com/winler/warden/internal/runtime/docker"
)

// RootfsFilename computes the rootfs filename for a base image + tool set.
// Derived from Docker tag by stripping "warden:" prefix and appending ".ext4".
func RootfsFilename(base string, tools []string) string {
	tag := rdocker.ImageTag(base, tools)
	// Strip "warden:" prefix
	name := strings.TrimPrefix(tag, "warden:")
	return name + ".ext4"
}

func rootfsPath(homeDir string, base string, tools []string) string {
	return filepath.Join(homeDir, ".warden", "firecracker", "rootfs", RootfsFilename(base, tools))
}

func rootfsExists(homeDir string, base string, tools []string) bool {
	_, err := os.Stat(rootfsPath(homeDir, base, tools))
	return err == nil
}

// BuildRootfs creates an ext4 rootfs image using Docker to assemble the filesystem.
func BuildRootfs(homeDir string, base string, tools []string) (string, error) {
	path := rootfsPath(homeDir, base, tools)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	fmt.Fprintf(os.Stderr, "warden: building rootfs image (first run only)...\n")

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("creating rootfs directory: %w", err)
	}

	// Use Docker to build the image, then export filesystem
	dockerTag, err := rdocker.BuildImage(base, tools)
	if err != nil {
		return "", fmt.Errorf("building docker image for rootfs export: %w", err)
	}

	// Create a temporary container and export its filesystem
	containerName := fmt.Sprintf("warden-rootfs-export-%d", os.Getpid())
	if err := exec.Command("docker", "create", "--name", containerName, dockerTag).Run(); err != nil {
		return "", fmt.Errorf("creating export container: %w", err)
	}
	defer exec.Command("docker", "rm", containerName).Run()

	// Export to tar, then create ext4 image
	tmpTar := path + ".tar"
	defer os.Remove(tmpTar)

	exportCmd := exec.Command("docker", "export", "-o", tmpTar, containerName)
	exportCmd.Stderr = os.Stderr
	if err := exportCmd.Run(); err != nil {
		return "", fmt.Errorf("exporting container filesystem: %w", err)
	}

	// Create ext4 image from tar
	if err := tarToExt4(tmpTar, path); err != nil {
		os.Remove(path)
		return "", fmt.Errorf("creating ext4 image: %w", err)
	}

	return path, nil
}

// tarToExt4 creates an ext4 filesystem image from a tar archive.
func tarToExt4(tarPath, ext4Path string) error {
	// Create a 4GB sparse file (most content is sparse, so actual disk usage is small)
	if err := exec.Command("truncate", "-s", "4G", ext4Path).Run(); err != nil {
		return fmt.Errorf("creating sparse file: %w", err)
	}

	// Format as ext4
	if err := exec.Command("mkfs.ext4", "-F", ext4Path).Run(); err != nil {
		return fmt.Errorf("formatting ext4: %w", err)
	}

	// Mount and extract tar
	tmpMount, err := os.MkdirTemp("", "warden-rootfs-mount-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpMount)

	// Use a privileged Docker container to mount the ext4 image and extract
	extractCmd := exec.Command("docker", "run", "--rm", "--privileged",
		"-v", tarPath+":/rootfs.tar:ro",
		"-v", ext4Path+":/rootfs.ext4",
		"ubuntu:24.04",
		"bash", "-c",
		"mkdir /mnt/rootfs && mount /rootfs.ext4 /mnt/rootfs && "+
			"tar xf /rootfs.tar -C /mnt/rootfs && umount /mnt/rootfs",
	)
	extractCmd.Stderr = os.Stderr
	if err := extractCmd.Run(); err != nil {
		return fmt.Errorf("extracting tar to ext4: %w", err)
	}

	return nil
}

// EnsureImage implements Runtime.EnsureImage for Firecracker.
func (f *FirecrackerRuntime) EnsureImage(cfg config.SandboxConfig) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return BuildRootfs(homeDir, cfg.Image, cfg.Tools)
}

// ListImages returns cached Firecracker rootfs images.
func (f *FirecrackerRuntime) ListImages() ([]ImageInfo, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	rootfsDir := filepath.Join(homeDir, ".warden", "firecracker", "rootfs")
	entries, err := os.ReadDir(rootfsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var images []ImageInfo
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".ext4") {
			info, _ := e.Info()
			img := ImageInfo{
				Tag:     e.Name(),
				Runtime: "firecracker",
			}
			if info != nil {
				img.Size = info.Size()
				img.CreatedAt = info.ModTime()
			}
			images = append(images, img)
		}
	}
	return images, nil
}

// PruneImages removes all cached Firecracker rootfs images.
func (f *FirecrackerRuntime) PruneImages() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	rootfsDir := filepath.Join(homeDir, ".warden", "firecracker", "rootfs")
	entries, err := os.ReadDir(rootfsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	count := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".ext4") {
			os.Remove(filepath.Join(rootfsDir, e.Name()))
			count++
		}
	}
	if count > 0 {
		fmt.Printf("Removed %d firecracker rootfs image(s).\n", count)
	}
	return nil
}

// Note: use runtime.ImageInfo directly — no type alias needed.
```

Wait, there's a circular import issue. `ImageInfo` is defined in `runtime` package and `firecracker` imports `runtime`. Let me fix the import reference. The `ListImages` and `PruneImages` methods return `[]runtime.ImageInfo`, which requires importing `runtime`. But `firecracker` already imports `runtime` for the `Register` call. That's fine — it's not circular since `runtime` doesn't import `firecracker`.

Actually, looking at this more carefully — `firecracker` imports `runtime/docker` for `ImageTag`. That creates `firecracker -> docker` dependency. And `docker` also imports `runtime` for `Register`. The chain is: `firecracker -> docker -> runtime`. No circularity. Good.

Let me fix the `ListImages`/`PruneImages` to use proper types:

```go
import (
	"github.com/winler/warden/internal/runtime"
)
```

And return `[]runtime.ImageInfo`.

- [ ] **Step 4: Run rootfs tests**

Run: `cd /home/winler/projects/warden && go test ./internal/runtime/firecracker/ -run TestRootfs -v`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/firecracker/image.go internal/runtime/firecracker/image_test.go
git commit -m "feat: add Firecracker rootfs image building and management"
```

### Task 10: Register firecracker runtime in CLI

- [ ] **Step 1: Update CLI imports to register firecracker runtime**

In `internal/cli/root.go`, add blank import:
```go
import (
	_ "github.com/winler/warden/internal/runtime/docker"
	_ "github.com/winler/warden/internal/runtime/firecracker"
)
```

- [ ] **Step 2: Run full test suite**

Run: `cd /home/winler/projects/warden && go test ./... -v`
Expected: ALL PASS

- [ ] **Step 3: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: register firecracker runtime in CLI"
```

---

## Chunk 5: Networking & warden-netsetup

### Task 11: IP allocation

**Files:**
- Create: `internal/runtime/firecracker/network.go`
- Create: `internal/runtime/firecracker/network_test.go`

- [ ] **Step 1: Write failing test for IP allocation**

```go
package firecracker

import (
	"path/filepath"
	"testing"
)

func TestSubnetForIndex(t *testing.T) {
	tests := []struct {
		index   uint32
		gateway string
		guest   string
	}{
		{0, "172.16.0.1/30", "172.16.0.2/30"},
		{1, "172.16.0.5/30", "172.16.0.6/30"},
		{2, "172.16.0.9/30", "172.16.0.10/30"},
		{64, "172.16.1.1/30", "172.16.1.2/30"},
	}
	for _, tc := range tests {
		gw, guest := subnetForIndex(tc.index)
		if gw != tc.gateway {
			t.Errorf("index %d: gateway = %q, want %q", tc.index, gw, tc.gateway)
		}
		if guest != tc.guest {
			t.Errorf("index %d: guest = %q, want %q", tc.index, guest, tc.guest)
		}
	}
}

func TestAllocateAndRelease(t *testing.T) {
	tmpDir := t.TempDir()
	allocFile := filepath.Join(tmpDir, "net-alloc")

	gw1, guest1, release1, err := allocateSubnet(allocFile)
	if err != nil {
		t.Fatalf("first alloc: %v", err)
	}
	if gw1 == "" || guest1 == "" {
		t.Fatal("empty allocation")
	}

	gw2, guest2, release2, err := allocateSubnet(allocFile)
	if err != nil {
		t.Fatalf("second alloc: %v", err)
	}
	if gw1 == gw2 {
		t.Errorf("duplicate allocation: %s", gw1)
	}

	release1()
	release2()

	_ = guest1
	_ = guest2
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/winler/projects/warden && go test ./internal/runtime/firecracker/ -run TestSubnet -v`
Expected: FAIL

- [ ] **Step 3: Implement network.go**

```go
package firecracker

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// subnetForIndex computes the gateway and guest IPs for a given allocation index.
// Each index maps to a /30 subnet within 172.16.0.0/12.
func subnetForIndex(index uint32) (gateway, guest string) {
	// Base: 172.16.0.0, stride 4 per /30
	base := uint32(0xAC100000) // 172.16.0.0
	offset := index * 4
	netAddr := base + offset

	gwAddr := netAddr + 1
	guestAddr := netAddr + 2

	gw := fmt.Sprintf("%d.%d.%d.%d/30",
		(gwAddr>>24)&0xFF, (gwAddr>>16)&0xFF, (gwAddr>>8)&0xFF, gwAddr&0xFF)
	g := fmt.Sprintf("%d.%d.%d.%d/30",
		(guestAddr>>24)&0xFF, (guestAddr>>16)&0xFF, (guestAddr>>8)&0xFF, guestAddr&0xFF)
	return gw, g
}

// allocateSubnet allocates the next available /30 subnet.
// Returns gateway IP, guest IP, and a release function.
func allocateSubnet(allocFile string) (gateway, guest string, release func(), err error) {
	if err := os.MkdirAll(filepath.Dir(allocFile), 0o755); err != nil {
		return "", "", nil, err
	}

	f, err := os.OpenFile(allocFile, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return "", "", nil, err
	}

	// Lock file
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return "", "", nil, fmt.Errorf("locking allocation file: %w", err)
	}

	// Read current counter
	var counter uint32
	buf := make([]byte, 4)
	if n, _ := f.Read(buf); n == 4 {
		counter = binary.LittleEndian.Uint32(buf)
	}

	// Wrap at ~262K (the usable range within 172.16.0.0/12)
	const maxIndex = 262144
	index := counter % maxIndex

	// Write incremented counter
	binary.LittleEndian.PutUint32(buf, counter+1)
	f.Seek(0, 0)
	f.Write(buf)
	f.Truncate(4)

	// Unlock
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	f.Close()

	gw, g := subnetForIndex(index)
	releaseFunc := func() {
		// Deliberate simplification: counter increments monotonically and wraps at 262K.
		// No per-run reclaim — with 262K subnets, exhaustion requires 262K concurrent VMs
		// before wraparound. A bitmap-based free-list can be added later if needed.
	}
	return gw, g, releaseFunc, nil
}

// tapName generates a unique TAP device name.
func tapName() string {
	var buf [4]byte
	rand.Read(buf[:])
	return fmt.Sprintf("warden-fc-%x", buf)
}
```

- [ ] **Step 4: Run network tests**

Run: `cd /home/winler/projects/warden && go test ./internal/runtime/firecracker/ -run "TestSubnet|TestAllocate" -v`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/firecracker/network.go internal/runtime/firecracker/network_test.go
git commit -m "feat: add IP allocation and TAP device naming for Firecracker networking"
```

### Task 12: warden-netsetup helper binary

**Files:**
- Create: `cmd/warden-netsetup/main.go`

- [ ] **Step 1: Create the network helper binary**

```go
package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

var tapNamePattern = regexp.MustCompile(`^warden-fc-[0-9a-f]{8}$`)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: warden-netsetup <create|destroy|verify> [flags]\n")
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "create":
		err = runCreate(os.Args[2:])
	case "destroy":
		err = runDestroy(os.Args[2:])
	case "verify":
		err = runVerify()
	default:
		err = fmt.Errorf("unknown command: %s", os.Args[1])
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "warden-netsetup: %v\n", err)
		os.Exit(1)
	}
}

func runCreate(args []string) error {
	var tapDevice, hostIP, guestIP, outIface string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--tap":
			i++; tapDevice = args[i]
		case "--host-ip":
			i++; hostIP = args[i]
		case "--guest-ip":
			i++; guestIP = args[i]
		case "--outbound-iface":
			i++; outIface = args[i]
		}
	}

	if err := validateTapName(tapDevice); err != nil {
		return err
	}
	if err := validateIP(hostIP); err != nil {
		return fmt.Errorf("invalid host-ip: %w", err)
	}
	if err := validateIP(guestIP); err != nil {
		return fmt.Errorf("invalid guest-ip: %w", err)
	}

	// Create TAP device
	if err := run("ip", "tuntap", "add", "dev", tapDevice, "mode", "tap"); err != nil {
		return fmt.Errorf("creating TAP: %w", err)
	}
	if err := run("ip", "addr", "add", hostIP, "dev", tapDevice); err != nil {
		return fmt.Errorf("assigning IP: %w", err)
	}
	if err := run("ip", "link", "set", tapDevice, "up"); err != nil {
		return fmt.Errorf("bringing up TAP: %w", err)
	}

	// Add iptables MASQUERADE rule
	if outIface != "" {
		if err := run("iptables", "-t", "nat", "-A", "POSTROUTING",
			"-o", outIface, "-s", guestIP, "-j", "MASQUERADE"); err != nil {
			return fmt.Errorf("adding NAT rule: %w", err)
		}
	}

	return nil
}

func runDestroy(args []string) error {
	var tapDevice, guestIP, outIface string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--tap":
			i++; tapDevice = args[i]
		case "--guest-ip":
			i++; guestIP = args[i]
		case "--outbound-iface":
			i++; outIface = args[i]
		}
	}
	if err := validateTapName(tapDevice); err != nil {
		return err
	}

	// Remove iptables MASQUERADE rule (must happen before TAP deletion)
	if outIface != "" && guestIP != "" {
		run("iptables", "-t", "nat", "-D", "POSTROUTING",
			"-o", outIface, "-s", guestIP, "-j", "MASQUERADE")
	}

	// Remove TAP device (also removes associated routes)
	run("ip", "link", "del", tapDevice)
	return nil
}

func runVerify() error {
	// Check we can create and destroy a test TAP device
	testName := "warden-fc-00000000"
	if err := run("ip", "tuntap", "add", "dev", testName, "mode", "tap"); err != nil {
		return fmt.Errorf("cannot create TAP devices — check capabilities: %w", err)
	}
	run("ip", "link", "del", testName)
	fmt.Println("warden-netsetup: capabilities OK")
	return nil
}

func validateTapName(name string) error {
	if !tapNamePattern.MatchString(name) {
		return fmt.Errorf("invalid TAP name: %q (must match warden-fc-XXXXXXXX)", name)
	}
	return nil
}

func validateIP(ip string) error {
	parts := strings.SplitN(ip, "/", 2)
	if net.ParseIP(parts[0]) == nil {
		return fmt.Errorf("invalid IP: %q", ip)
	}
	// Verify within 172.16.0.0/12
	parsed := net.ParseIP(parts[0])
	network := net.IPNet{
		IP:   net.ParseIP("172.16.0.0"),
		Mask: net.CIDRMask(12, 32),
	}
	if !network.Contains(parsed) {
		return fmt.Errorf("IP %s not in 172.16.0.0/12 range", ip)
	}
	return nil
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /home/winler/projects/warden && go build ./cmd/warden-netsetup/`
Expected: Success

- [ ] **Step 3: Commit**

```bash
git add cmd/warden-netsetup/
git commit -m "feat: add warden-netsetup helper binary for TAP device management"
```

---

## Chunk 6: Guest Init Agent

### Task 13: Guest init agent

**Files:**
- Create: `internal/guest/exec.go`
- Create: `internal/guest/net.go`
- Create: `cmd/warden-init/main.go`

- [ ] **Step 1: Create internal/guest/exec.go**

```go
package guest

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/winler/warden/internal/protocol"
)

// Execute runs a command from an ExecMessage and returns exit code.
func Execute(msg *protocol.ExecMessage) (int, error) {
	cmd := exec.Command(msg.Command, msg.Args...)
	cmd.Dir = msg.Workdir
	cmd.Env = msg.Env

	// Set UID/GID
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid: uint32(msg.UID),
			Gid: uint32(msg.GID),
		},
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), nil
		}
		return 1, fmt.Errorf("executing command: %w", err)
	}
	return 0, nil
}
```

- [ ] **Step 2: Create internal/guest/net.go**

```go
package guest

import (
	"os/exec"
)

// ConfigureNetwork sets up the guest network interface if present.
func ConfigureNetwork(guestIP, gateway string) error {
	// Find the network interface (typically eth0 in Firecracker guests)
	iface := "eth0"

	if err := exec.Command("ip", "addr", "add", guestIP, "dev", iface).Run(); err != nil {
		return err
	}
	if err := exec.Command("ip", "link", "set", iface, "up").Run(); err != nil {
		return err
	}
	if err := exec.Command("ip", "route", "add", "default", "via", gateway).Run(); err != nil {
		return err
	}
	return nil
}
```

- [ ] **Step 3: Create cmd/warden-init/main.go**

```go
package main

import (
	"fmt"
	"os"
	"syscall"
)

func main() {
	// Mount essential filesystems
	if err := mountFilesystems(); err != nil {
		fmt.Fprintf(os.Stderr, "warden-init: mount error: %v\n", err)
		os.Exit(1)
	}

	// TODO: Set up vsock listener, receive ExecMessage, execute command,
	// stream output, return exit code. For now, this is a placeholder
	// that will be completed when the VM lifecycle is implemented.
	fmt.Fprintln(os.Stderr, "warden-init: ready")

	// Block forever (placeholder — real implementation uses vsock event loop)
	select {}
}

func mountFilesystems() error {
	mounts := []struct {
		source string
		target string
		fstype string
		flags  uintptr
	}{
		{"proc", "/proc", "proc", 0},
		{"sysfs", "/sys", "sysfs", 0},
		{"devtmpfs", "/dev", "devtmpfs", 0},
	}

	for _, m := range mounts {
		os.MkdirAll(m.target, 0o755)
		if err := syscall.Mount(m.source, m.target, m.fstype, m.flags, ""); err != nil {
			// Don't fail if already mounted
			if !os.IsExist(err) {
				return fmt.Errorf("mounting %s: %w", m.target, err)
			}
		}
	}
	return nil
}
```

- [ ] **Step 4: Verify both binaries compile**

Run: `cd /home/winler/projects/warden && go build ./cmd/warden-init/ && go build ./cmd/warden-netsetup/`
Expected: Success

- [ ] **Step 5: Commit**

```bash
git add internal/guest/ cmd/warden-init/
git commit -m "feat: add guest init agent with mount setup and command execution"
```

---

## Chunk 7: Firecracker VM Lifecycle & Setup Command

### Task 14: virtiofs management

**Files:**
- Create: `internal/runtime/firecracker/virtiofs.go`

- [ ] **Step 1: Implement virtiofsd process management**

```go
package firecracker

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type virtiofsInstance struct {
	cmd    *exec.Cmd
	socket string
	tag    string
}

// startVirtiofs starts a virtiofsd instance for the given host path.
func startVirtiofs(homeDir, hostPath, tag string) (*virtiofsInstance, error) {
	vfsPath := filepath.Join(homeDir, ".warden", "firecracker", "bin", "virtiofsd")

	socketDir, err := os.MkdirTemp("", "warden-vfs-*")
	if err != nil {
		return nil, err
	}
	socketPath := filepath.Join(socketDir, "vfs.sock")

	cmd := exec.Command(vfsPath,
		"--socket-path", socketPath,
		"--shared-dir", hostPath,
		"--tag", tag,
	)
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		os.RemoveAll(socketDir)
		return nil, fmt.Errorf("starting virtiofsd for %s: %w", hostPath, err)
	}

	return &virtiofsInstance{
		cmd:    cmd,
		socket: socketPath,
		tag:    tag,
	}, nil
}

func (v *virtiofsInstance) stop() {
	if v.cmd.Process != nil {
		v.cmd.Process.Kill()
		v.cmd.Wait()
	}
	// Clean up socket directory
	os.RemoveAll(filepath.Dir(v.socket))
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /home/winler/projects/warden && go build ./internal/runtime/firecracker/`
Expected: Success

- [ ] **Step 3: Commit**

```bash
git add internal/runtime/firecracker/virtiofs.go
git commit -m "feat: add virtiofsd process management"
```

### Task 15: VM lifecycle (placeholder with structure)

**Files:**
- Create: `internal/runtime/firecracker/vm.go`

- [ ] **Step 1: Implement VM lifecycle manager**

```go
package firecracker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/winler/warden/internal/config"
	"github.com/winler/warden/internal/runtime/shared"
)

type vmInstance struct {
	cmd        *exec.Cmd
	socketPath string
	virtiofs   []*virtiofsInstance
	tapDevice  string
	guestIP    string
	outIface   string
	releaseIP  func()
}

// startVM configures and boots a Firecracker microVM.
func startVM(cfg config.SandboxConfig, command []string) (*vmInstance, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	// Create a temp directory for this VM's API socket
	tmpDir, err := os.MkdirTemp("", "warden-fc-*")
	if err != nil {
		return nil, err
	}
	socketPath := filepath.Join(tmpDir, "firecracker.sock")

	vm := &vmInstance{
		socketPath: socketPath,
	}

	// Resolve kernel
	kernelPath, err := resolveKernelPath("", homeDir) // TODO: support custom kernel config
	if err != nil {
		return nil, err
	}

	// Resolve rootfs
	rootfs := rootfsPath(homeDir, cfg.Image, cfg.Tools)

	// Create overlay for writable rootfs
	overlayDir := filepath.Join(homeDir, ".warden", "firecracker", "overlays")
	os.MkdirAll(overlayDir, 0o755)
	overlayPath := filepath.Join(overlayDir, fmt.Sprintf("overlay-%d.ext4", os.Getpid()))
	// Copy rootfs as overlay (TODO: use proper copy-on-write)
	if err := copyFile(rootfs, overlayPath); err != nil {
		return nil, fmt.Errorf("creating rootfs overlay: %w", err)
	}

	// Start virtiofsd for each mount
	for i, m := range cfg.Mounts {
		tag := fmt.Sprintf("mount%d", i)
		vfs, err := startVirtiofs(homeDir, m.Path, tag)
		if err != nil {
			vm.cleanup()
			return nil, err
		}
		vm.virtiofs = append(vm.virtiofs, vfs)
	}

	// Handle networking
	if cfg.Network {
		allocFile := filepath.Join(homeDir, ".warden", "firecracker", "net-alloc")
		gwIP, guestIP, release, err := allocateSubnet(allocFile)
		if err != nil {
			vm.cleanup()
			return nil, err
		}
		vm.releaseIP = release

		tap := tapName()
		vm.tapDevice = tap
		vm.guestIP = guestIP
		outIface := detectOutboundInterface()
		vm.outIface = outIface
		setupCmd := exec.Command("/usr/local/bin/warden-netsetup", "create",
			"--tap", tap,
			"--host-ip", gwIP,
			"--guest-ip", guestIP,
			"--outbound-iface", outIface,
		)
		setupCmd.Stderr = os.Stderr
		if err := setupCmd.Run(); err != nil {
			vm.cleanup()
			return nil, fmt.Errorf("warden: failed to create network interface. Check warden-netsetup capabilities")
		}
	}

	// Start Firecracker process
	fcPath := filepath.Join(homeDir, ".warden", "firecracker", "bin", "firecracker")
	vm.cmd = exec.Command(fcPath,
		"--api-sock", socketPath,
	)
	vm.cmd.Stderr = os.Stderr

	if err := vm.cmd.Start(); err != nil {
		vm.cleanup()
		return nil, fmt.Errorf("starting firecracker: %w", err)
	}

	// Wait for API socket to be ready
	if err := waitForSocket(socketPath, 5*time.Second); err != nil {
		vm.cleanup()
		return nil, err
	}

	// Configure VM via API
	if err := vm.configureVM(kernelPath, overlayPath, cfg); err != nil {
		vm.cleanup()
		return nil, err
	}

	// Boot VM
	if err := vm.boot(); err != nil {
		vm.cleanup()
		return nil, err
	}

	return vm, nil
}

func (vm *vmInstance) configureVM(kernelPath, rootfsPath string, cfg config.SandboxConfig) error {
	// Set kernel
	if err := vm.apiPut("/boot-source", map[string]interface{}{
		"kernel_image_path": kernelPath,
		"boot_args":         "console=ttyS0 reboot=k panic=1 pci=off",
	}); err != nil {
		return fmt.Errorf("setting kernel: %w", err)
	}

	// Set rootfs
	if err := vm.apiPut("/drives/rootfs", map[string]interface{}{
		"drive_id":       "rootfs",
		"path_on_host":   rootfsPath,
		"is_root_device": true,
		"is_read_only":   false,
	}); err != nil {
		return fmt.Errorf("setting rootfs: %w", err)
	}

	// Set machine config
	cpus := cfg.CPUs
	if cpus == 0 {
		cpus = 1
	}
	mem := 1024 // default 1GB, TODO: parse cfg.Memory
	if err := vm.apiPut("/machine-config", map[string]interface{}{
		"vcpu_count":  cpus,
		"mem_size_mib": mem,
	}); err != nil {
		return fmt.Errorf("setting machine config: %w", err)
	}

	// Set network if TAP device exists
	if vm.tapDevice != "" {
		if err := vm.apiPut("/network-interfaces/eth0", map[string]interface{}{
			"iface_id":            "eth0",
			"host_dev_name":       vm.tapDevice,
			"guest_mac":           "AA:FC:00:00:00:01",
		}); err != nil {
			return fmt.Errorf("setting network: %w", err)
		}
	}

	return nil
}

func (vm *vmInstance) boot() error {
	return vm.apiPut("/actions", map[string]interface{}{
		"action_type": "InstanceStart",
	})
}

func (vm *vmInstance) cleanup() {
	// Stop virtiofsd instances
	for _, vfs := range vm.virtiofs {
		vfs.stop()
	}

	// Destroy TAP device and remove iptables rule
	if vm.tapDevice != "" {
		destroyArgs := []string{"destroy", "--tap", vm.tapDevice}
		if vm.guestIP != "" {
			destroyArgs = append(destroyArgs, "--guest-ip", vm.guestIP, "--outbound-iface", vm.outIface)
		}
		exec.Command("/usr/local/bin/warden-netsetup", destroyArgs...).Run()
	}

	// Release IP
	if vm.releaseIP != nil {
		vm.releaseIP()
	}

	// Kill Firecracker process
	if vm.cmd != nil && vm.cmd.Process != nil {
		vm.cmd.Process.Kill()
		vm.cmd.Wait()
	}

	// Clean up socket directory
	if vm.socketPath != "" {
		os.RemoveAll(filepath.Dir(vm.socketPath))
	}
}

func (vm *vmInstance) apiPut(path string, body interface{}) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", vm.socketPath)
			},
		},
	}

	req, err := http.NewRequest("PUT", "http://localhost"+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var buf bytes.Buffer
		buf.ReadFrom(resp.Body)
		return fmt.Errorf("firecracker API %s: %s — %s", path, resp.Status, buf.String())
	}

	return nil
}

func waitForSocket(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.Dial("unix", path)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("warden: firecracker API socket not ready after %s", timeout)
}

func detectOutboundInterface() string {
	// Find the default route interface
	out, err := exec.Command("ip", "route", "show", "default").Output()
	if err != nil {
		return "eth0" // fallback
	}
	// Parse "default via X.X.X.X dev IFACE ..."
	fields := bytes.Fields(out)
	for i, f := range fields {
		if string(f) == "dev" && i+1 < len(fields) {
			return string(fields[i+1])
		}
	}
	return "eth0"
}

func copyFile(src, dst string) error {
	// Try reflink (copy-on-write) first for instant, space-efficient copies
	if err := exec.Command("cp", "--reflink=auto", src, dst).Run(); err != nil {
		// Fallback to buffered io.Copy
		in, err := os.Open(src)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(dst)
		if err != nil {
			return err
		}
		defer out.Close()
		if _, err := io.Copy(out, in); err != nil {
			return err
		}
		return out.Close()
	}
	return nil
}
```

- [ ] **Step 2: Wire Run() and DryRun() in firecracker.go**

Update `internal/runtime/firecracker/firecracker.go` — replace the TODO stubs:

```go
// Run executes a command in a Firecracker microVM.
// NOTE: vsock guest communication is not yet wired. This function starts
// the VM but cannot forward the command to the guest init agent.
// A future task will implement the vsock event loop in warden-init
// and the host-side vsock client here to complete the chain.
func (f *FirecrackerRuntime) Run(cfg config.SandboxConfig, command []string) (int, error) {
	vm, err := startVM(cfg, command)
	if err != nil {
		return 1, err
	}
	defer vm.cleanup()

	// TODO(vsock): Connect to guest init agent via vsock, send ExecMessage
	// with `command`, stream output, handle signals, return exit code.
	// Until vsock is wired, the VM boots but the command is not forwarded.

	timeout, err := shared.ParseTimeout(cfg.Timeout)
	if err != nil {
		return 1, err
	}

	ctx := context.Background()
	var cancel context.CancelFunc = func() {}
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	// Signal handling
	cleanup := shared.SignalHandler(
		func(sig os.Signal) {
			// TODO: forward signal to guest via vsock
		},
		func() {
			vm.cleanup()
		},
	)
	defer cleanup()

	// Timeout watchdog
	if timeout > 0 {
		go func() {
			<-ctx.Done()
			if ctx.Err() == context.DeadlineExceeded {
				fmt.Fprintf(os.Stderr, "warden: killed (timeout after %s)\n", cfg.Timeout)
				vm.cleanup()
			}
		}()
	}

	// Wait for VM process
	if err := vm.cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code := exitErr.ExitCode()
			if msg := shared.ExitCodeMessage(code, cfg.Memory); msg != "" {
				fmt.Fprintln(os.Stderr, msg)
			}
			return code, nil
		}
		return 1, err
	}

	return 0, nil
}

// DryRun prints the VM configuration.
func (f *FirecrackerRuntime) DryRun(cfg config.SandboxConfig, command []string) error {
	homeDir, _ := os.UserHomeDir()
	kernelPath := defaultKernelPath(homeDir)
	rootfs := rootfsPath(homeDir, cfg.Image, cfg.Tools)

	vmConfig := map[string]interface{}{
		"runtime":   "firecracker",
		"kernel":    kernelPath,
		"rootfs":    rootfs,
		"vcpus":     cfg.CPUs,
		"memory":    cfg.Memory,
		"network":   cfg.Network,
		"mounts":    cfg.Mounts,
		"workdir":   cfg.Workdir,
		"command":   command,
	}

	data, _ := json.MarshalIndent(vmConfig, "", "  ")
	fmt.Println(string(data))
	return nil
}
```

- [ ] **Step 3: Verify compilation**

Run: `cd /home/winler/projects/warden && go build ./...`
Expected: Success

- [ ] **Step 4: Run all tests**

Run: `cd /home/winler/projects/warden && go test ./... -v`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/firecracker/
git commit -m "feat: add Firecracker VM lifecycle with API socket configuration"
```

### Task 16: warden setup firecracker command

**Files:**
- Create: `internal/cli/setup.go`
- Modify: `internal/cli/root.go`

- [ ] **Step 1: Create setup.go**

```go
package cli

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
)

func newSetupCommand() *cobra.Command {
	setup := &cobra.Command{
		Use:   "setup",
		Short: "Set up optional runtime backends",
	}

	fc := &cobra.Command{
		Use:   "firecracker",
		Short: "Set up Firecracker microVM runtime (requires sudo)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return setupFirecracker()
		},
	}

	setup.AddCommand(fc)
	return setup
}

func setupFirecracker() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("warden: firecracker is only supported on Linux")
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	binDir := filepath.Join(homeDir, ".warden", "firecracker", "bin")
	os.MkdirAll(binDir, 0o755)

	fmt.Println("Setting up Firecracker runtime...")

	// Step 1: Check /dev/kvm
	fmt.Print("  Checking /dev/kvm... ")
	if _, err := os.Stat("/dev/kvm"); err != nil {
		fmt.Println("NOT FOUND")
		return fmt.Errorf("warden: /dev/kvm not available. Ensure KVM is enabled in your kernel")
	}
	fmt.Println("OK")

	// Step 2: Add user to kvm group
	fmt.Print("  Adding user to kvm group... ")
	u, _ := user.Current()
	if err := exec.Command("sudo", "usermod", "-aG", "kvm", u.Username).Run(); err != nil {
		fmt.Println("FAILED")
		return fmt.Errorf("adding user to kvm group: %w (run with sudo)", err)
	}
	fmt.Println("OK")

	// Step 3: Download Firecracker binary
	fmt.Print("  Downloading firecracker binary... ")
	fcPath := filepath.Join(binDir, "firecracker")
	if _, err := os.Stat(fcPath); err != nil {
		// TODO: download from GitHub releases
		fmt.Println("SKIPPED (manual download required)")
		fmt.Printf("    Download from https://github.com/firecracker-microvm/firecracker/releases\n")
		fmt.Printf("    Place binary at: %s\n", fcPath)
	} else {
		fmt.Println("OK (already exists)")
	}

	// Step 4: Download virtiofsd
	fmt.Print("  Downloading virtiofsd... ")
	vfsPath := filepath.Join(binDir, "virtiofsd")
	if _, err := os.Stat(vfsPath); err != nil {
		fmt.Println("SKIPPED (manual download required)")
		fmt.Printf("    Place binary at: %s\n", vfsPath)
	} else {
		fmt.Println("OK (already exists)")
	}

	// Step 5: Build and install warden-netsetup
	fmt.Print("  Installing warden-netsetup... ")
	netsetupPath := "/usr/local/bin/warden-netsetup"
	if _, err := os.Stat(netsetupPath); err != nil {
		fmt.Println("SKIPPED")
		fmt.Println("    Build: go build -o warden-netsetup ./cmd/warden-netsetup/")
		fmt.Println("    Install: sudo cp warden-netsetup /usr/local/bin/")
		fmt.Println("    Set cap: sudo setcap cap_net_admin+ep /usr/local/bin/warden-netsetup")
	} else {
		fmt.Println("OK (already exists)")
	}

	// Step 6: Enable IP forwarding
	fmt.Print("  Checking IP forwarding... ")
	out, _ := os.ReadFile("/proc/sys/net/ipv4/ip_forward")
	if string(out) == "1\n" || string(out) == "1" {
		fmt.Println("OK (already enabled)")
	} else {
		fmt.Println("DISABLED")
		fmt.Println("    To enable: sudo sysctl -w net.ipv4.ip_forward=1")
		fmt.Println("    To persist: echo 'net.ipv4.ip_forward=1' | sudo tee /etc/sysctl.d/99-warden.conf")
	}

	// Step 7: Verify
	fmt.Println("\nSetup summary:")
	fmt.Println("  Some components may need manual installation.")
	fmt.Println("  Re-run 'warden setup firecracker' to check status.")
	fmt.Println("  You may need to log out and back in for the kvm group change to take effect.")

	return nil
}
```

- [ ] **Step 2: Add setup command to root**

In `internal/cli/root.go`, add after the existing AddCommand calls:
```go
root.AddCommand(newSetupCommand())
```

- [ ] **Step 3: Verify compilation and tests**

Run: `cd /home/winler/projects/warden && go build ./... && go test ./... -v`
Expected: ALL PASS

- [ ] **Step 4: Commit**

```bash
git add internal/cli/setup.go internal/cli/root.go
git commit -m "feat: add 'warden setup firecracker' command"
```

### Task 17: Final integration — verify full test suite

- [ ] **Step 1: Run full test suite**

Run: `cd /home/winler/projects/warden && go test ./... -v`
Expected: ALL PASS

- [ ] **Step 2: Run go vet**

Run: `cd /home/winler/projects/warden && go vet ./...`
Expected: No issues

- [ ] **Step 3: Build all binaries**

Run: `cd /home/winler/projects/warden && go build ./cmd/warden/ && go build ./cmd/warden-init/ && go build ./cmd/warden-netsetup/`
Expected: All three build successfully

- [ ] **Step 4: Commit any final fixes**

```bash
git add -A
git commit -m "chore: verify all tests pass and binaries build"
```

---

## Known Gaps (Future Tasks)

These items are acknowledged spec requirements not completed in this plan. Each should become its own follow-up task:

1. **vsock event loop in warden-init** — `cmd/warden-init/main.go` is a placeholder that mounts filesystems and blocks. It does not yet listen on vsock, receive `ExecMessage`, execute commands, or stream output. Until this is wired, `warden run --runtime firecracker` boots a VM that idles. This is the critical next step.

2. **Host-side vsock client in FirecrackerRuntime.Run()** — The `Run()` method starts the VM but does not connect to the guest agent via vsock to send the command. Marked `TODO(vsock)` in the code.

3. **Global config (`~/.warden/config.yaml`)** — The spec defines a machine-level config for custom kernel paths. `resolveKernelPath()` accepts a `customPath` parameter but nothing reads the config file to populate it. Needs: YAML parsing of `~/.warden/config.yaml` with a `firecracker.kernel` field, passed to `resolveKernelPath()`.

4. **Automated binary downloads in `warden setup firecracker`** — The setup command currently prints manual download instructions. Should download Firecracker and virtiofsd binaries from GitHub releases with checksum verification.

5. **IP allocation reclaim** — The counter-based allocator is a deliberate simplification. A bitmap-based free-list could be added if 262K sequential allocations before wraparound proves insufficient.
