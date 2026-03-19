# Firecracker Gaps Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close all implementation gaps in the Firecracker runtime so `warden run --runtime firecracker -- <cmd>` executes commands end-to-end with proper signal handling, timeouts, and cleanup.

**Architecture:** Single vsock connection (port 1024) between host and guest using the existing length-prefixed JSON protocol. Host dials guest CID 3 after VM boot. Guest agent spawns command, streams stdout/stderr as OutputMessages, returns ExitMessage. Independent gaps (memory parsing, IP reclamation, global config, kernel pinning, automated setup) are addressed in parallel tasks.

**Tech Stack:** Go 1.21, `github.com/mdlayher/vsock`, Firecracker v1.15.0, kernel 6.1.155, virtiofsd v1.13.3

---

## File Structure

| File | Responsibility |
|------|---------------|
| `cmd/warden-init/main.go` | Guest init agent: mount filesystems, vsock listener, command execution, output streaming |
| `internal/runtime/firecracker/firecracker.go` | Host-side `Run()`: vsock dial, send ExecMessage, read Output/Exit loop, signal/timeout handling |
| `internal/runtime/firecracker/vm.go` | VM lifecycle: add vsock device config, memory parsing, global config wiring |
| `internal/runtime/firecracker/kernel.go` | Kernel constants: version, URL, checksum |
| `internal/runtime/firecracker/network.go` | IP allocation: PID-tracked allocation file replacing monotonic counter |
| `internal/runtime/firecracker/setup.go` | Download helpers: kernel, Firecracker binary, virtiofsd build (extracted from setup.go CLI) |
| `internal/cli/setup.go` | CLI command: orchestrates setup flow, calls download helpers |
| `internal/config/global.go` | Global config: `LoadGlobalConfig()` for `~/.warden/config.yaml` |

---

## Chunk 1: Foundation — Version Pinning, Memory Parsing, Global Config

These are independent, low-risk changes that don't touch the vsock communication path. Each task is self-contained with its own tests.

### Task 1: Update Kernel Version and Checksum

**Files:**
- Modify: `internal/runtime/firecracker/kernel.go:13-18`
- Modify: `internal/runtime/firecracker/kernel_test.go`

- [ ] **Step 1: Update kernel constants**

In `internal/runtime/firecracker/kernel.go`, replace:

```go
const (
	kernelVersion  = "5.10.217"
	kernelFilename = "vmlinux-" + kernelVersion
	// TODO: replace with actual checksum from Firecracker releases
	kernelChecksum = "placeholder-sha256-checksum"
	kernelURL      = "https://github.com/firecracker-microvm/firecracker/releases/download/v1.7.0/" + kernelFilename
)
```

With:

```go
const (
	kernelVersion  = "6.1.155"
	kernelFilename = "vmlinux-" + kernelVersion
	kernelChecksum = "e20e46d0c36c55c0d1014eb20576171b3f3d922260d9f792017aeff53af3d4f2"
	kernelURL      = "https://s3.amazonaws.com/spec.ccfc.min/firecracker-ci/v1.15/x86_64/" + kernelFilename
)
```

- [ ] **Step 2: Update kernel test expectations**

In `internal/runtime/firecracker/kernel_test.go`, update `TestDefaultKernelPath`:

```go
func TestDefaultKernelPath(t *testing.T) {
	tmpHome := t.TempDir()
	path := defaultKernelPath(tmpHome)
	want := filepath.Join(tmpHome, ".warden", "firecracker", "kernel", "vmlinux-6.1.155")
	if path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
}
```

Update `TestResolveKernelPathDefault` to create the file with the new name:

```go
func TestResolveKernelPathDefault(t *testing.T) {
	tmpHome := t.TempDir()
	kernelDir := filepath.Join(tmpHome, ".warden", "firecracker", "kernel")
	os.MkdirAll(kernelDir, 0o755)
	kernelPath := filepath.Join(kernelDir, "vmlinux-6.1.155")
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

- [ ] **Step 3: Add checksum format test**

Append to `internal/runtime/firecracker/kernel_test.go`:

```go
func TestKernelChecksumLength(t *testing.T) {
	if len(kernelChecksum) != 64 {
		t.Errorf("checksum length = %d, want 64 (sha256 hex)", len(kernelChecksum))
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/runtime/firecracker/ -run TestKernel -v`
Expected: All 3 kernel tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/firecracker/kernel.go internal/runtime/firecracker/kernel_test.go
git commit -m "feat: pin kernel to 6.1.155 with verified SHA256 checksum"
```

---

### Task 2: Memory Parsing

**Files:**
- Modify: `internal/runtime/firecracker/vm.go`
- Create: `internal/runtime/firecracker/memory_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/runtime/firecracker/memory_test.go`:

```go
package firecracker

import "testing"

func TestParseMemoryMiB(t *testing.T) {
	tests := []struct {
		input   string
		want    int
		wantErr bool
	}{
		{"", 1024, false},
		{"512m", 512, false},
		{"512M", 512, false},
		{"2g", 2048, false},
		{"2G", 2048, false},
		{"4096", 4096, false},
		{"1024m", 1024, false},
		{"0", 0, true},
		{"-1", 0, true},
		{"abc", 0, true},
		{"5x", 0, true},
	}
	for _, tc := range tests {
		got, err := parseMemoryMiB(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseMemoryMiB(%q) = %d, want error", tc.input, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseMemoryMiB(%q) error: %v", tc.input, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseMemoryMiB(%q) = %d, want %d", tc.input, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/firecracker/ -run TestParseMemoryMiB -v`
Expected: FAIL — `parseMemoryMiB` undefined.

- [ ] **Step 3: Implement parseMemoryMiB**

Add to `internal/runtime/firecracker/vm.go`, before `configureVM`:

```go
// parseMemoryMiB parses a memory string (e.g. "512m", "2g", "1024") into MiB.
// Empty string returns the default of 1024 MiB.
func parseMemoryMiB(s string) (int, error) {
	if s == "" {
		return 1024, nil
	}
	s = strings.TrimSpace(s)

	var numStr string
	var multiplier int

	last := strings.ToLower(s[len(s)-1:])
	switch last {
	case "g":
		numStr = s[:len(s)-1]
		multiplier = 1024
	case "m":
		numStr = s[:len(s)-1]
		multiplier = 1
	default:
		numStr = s
		multiplier = 1
	}

	n, err := strconv.Atoi(numStr)
	if err != nil {
		return 0, fmt.Errorf("warden: invalid memory value %q", s)
	}

	result := n * multiplier
	if result <= 0 {
		return 0, fmt.Errorf("warden: memory must be positive, got %q", s)
	}
	return result, nil
}
```

Add `"strconv"` and `"strings"` to the imports in `vm.go`.

- [ ] **Step 4: Wire into configureVM**

In `internal/runtime/firecracker/vm.go`, replace:

```go
	mem := 1024 // default 1GB, TODO: parse cfg.Memory
```

With:

```go
	mem, err := parseMemoryMiB(cfg.Memory)
	if err != nil {
		return err
	}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/runtime/firecracker/ -run TestParseMemoryMiB -v`
Expected: PASS — all 11 test cases.

Run: `go test ./internal/runtime/firecracker/ -v`
Expected: All existing tests still pass.

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/firecracker/vm.go internal/runtime/firecracker/memory_test.go
git commit -m "feat: parse memory config string into MiB for Firecracker VM"
```

---

### Task 3: Global Config File

**Files:**
- Create: `internal/config/global.go`
- Create: `internal/config/global_test.go`
- Modify: `internal/runtime/firecracker/vm.go:48`

- [ ] **Step 1: Write the failing tests**

Create `internal/config/global_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadGlobalConfigMissing(t *testing.T) {
	cfg, err := LoadGlobalConfig(filepath.Join(t.TempDir(), "nonexistent"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Firecracker.Kernel != "" {
		t.Errorf("kernel = %q, want empty", cfg.Firecracker.Kernel)
	}
}

func TestLoadGlobalConfigValid(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(cfgPath, []byte("firecracker:\n  kernel: /custom/vmlinux\n"), 0o644)

	cfg, err := LoadGlobalConfig(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Firecracker.Kernel != "/custom/vmlinux" {
		t.Errorf("kernel = %q, want /custom/vmlinux", cfg.Firecracker.Kernel)
	}
}

func TestLoadGlobalConfigMalformed(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(cfgPath, []byte("not: [valid: yaml: {{"), 0o644)

	_, err := LoadGlobalConfig(cfgPath)
	if err == nil {
		t.Fatal("expected error for malformed YAML")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestLoadGlobalConfig -v`
Expected: FAIL — `LoadGlobalConfig` undefined.

- [ ] **Step 3: Implement LoadGlobalConfig**

Create `internal/config/global.go`:

```go
package config

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// GlobalConfig holds user-level settings from ~/.warden/config.yaml.
type GlobalConfig struct {
	Firecracker FirecrackerGlobalConfig `yaml:"firecracker"`
}

// FirecrackerGlobalConfig holds Firecracker-specific global settings.
type FirecrackerGlobalConfig struct {
	Kernel string `yaml:"kernel"`
}

// LoadGlobalConfig reads the global config from the given path.
// Returns zero-value config if the file doesn't exist.
func LoadGlobalConfig(path string) (GlobalConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return GlobalConfig{}, nil
		}
		return GlobalConfig{}, fmt.Errorf("reading global config: %w", err)
	}

	var cfg GlobalConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return GlobalConfig{}, fmt.Errorf("parsing global config: %w", err)
	}
	return cfg, nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/config/ -run TestLoadGlobalConfig -v`
Expected: PASS — all 3 tests.

- [ ] **Step 5: Wire into startVM**

In `internal/runtime/firecracker/vm.go`, in the `startVM` function, replace:

```go
	kernelPath, err := resolveKernelPath("", homeDir)
```

With:

```go
	globalCfgPath := filepath.Join(homeDir, ".warden", "config.yaml")
	globalCfg, err := config.LoadGlobalConfig(globalCfgPath)
	if err != nil {
		return nil, err
	}
	kernelPath, err := resolveKernelPath(globalCfg.Firecracker.Kernel, homeDir)
```

- [ ] **Step 6: Run all tests**

Run: `go test ./internal/config/ ./internal/runtime/firecracker/ -v`
Expected: All tests pass.

- [ ] **Step 7: Tidy module**

Run: `go mod tidy`

This promotes `gopkg.in/yaml.v3` from indirect to direct in `go.mod` (now imported by `internal/config/global.go`).

- [ ] **Step 8: Commit**

```bash
git add internal/config/global.go internal/config/global_test.go internal/runtime/firecracker/vm.go go.mod
git commit -m "feat: add global config file support for kernel path override"
```

---

### Task 4: IP Reclamation with PID-based Stale Detection

**Files:**
- Modify: `internal/runtime/firecracker/network.go`
- Modify: `internal/runtime/firecracker/network_test.go`

- [ ] **Step 1: Write the failing tests**

Replace the contents of `internal/runtime/firecracker/network_test.go` with:

```go
package firecracker

import (
	"fmt"
	"os"
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

	// Release first, verify reclamation
	release1()

	// Third allocation should reclaim index 0 (released by release1)
	gw3, _, release3, err := allocateSubnet(allocFile)
	if err != nil {
		t.Fatalf("third alloc: %v", err)
	}
	if gw3 != gw1 {
		t.Errorf("expected reclaimed gw %s, got %s", gw1, gw3)
	}

	release2()
	release3()
	_ = guest1
	_ = guest2
}

func TestAllocateReclainsDeadPIDs(t *testing.T) {
	tmpDir := t.TempDir()
	allocFile := filepath.Join(tmpDir, "net-alloc")

	// Write an entry with a dead PID (PID 999999999 should not exist)
	os.WriteFile(allocFile, []byte("5:999999999\n"), 0o644)

	gw, _, release, err := allocateSubnet(allocFile)
	if err != nil {
		t.Fatalf("alloc: %v", err)
	}

	// Should reclaim index 5 from dead PID
	expectedGw, _ := subnetForIndex(5)
	if gw != expectedGw {
		t.Errorf("expected reclaimed gw %s, got %s", expectedGw, gw)
	}

	release()
}

func TestAllocateOldFormatMigration(t *testing.T) {
	tmpDir := t.TempDir()
	allocFile := filepath.Join(tmpDir, "net-alloc")

	// Write old 4-byte binary counter format
	os.WriteFile(allocFile, []byte{0x05, 0x00, 0x00, 0x00}, 0o644)

	// Should detect old format, reset, and allocate index 0
	gw, _, release, err := allocateSubnet(allocFile)
	if err != nil {
		t.Fatalf("alloc: %v", err)
	}
	expectedGw, _ := subnetForIndex(0)
	if gw != expectedGw {
		t.Errorf("expected gw %s after migration, got %s", expectedGw, gw)
	}

	// Verify file is now in new format (check before release removes the entry)
	data, _ := os.ReadFile(allocFile)
	expected := fmt.Sprintf("0:%d\n", os.Getpid())
	if string(data) != expected {
		t.Errorf("file content = %q, want %q", string(data), expected)
	}

	release()

	// After release, file should be empty (our entry removed)
	dataAfter, _ := os.ReadFile(allocFile)
	if len(dataAfter) != 0 {
		t.Errorf("file content after release = %q, want empty", dataAfter)
	}
}

func TestAllocateEmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	allocFile := filepath.Join(tmpDir, "net-alloc")

	gw, _, release, err := allocateSubnet(allocFile)
	if err != nil {
		t.Fatalf("alloc: %v", err)
	}
	expectedGw, _ := subnetForIndex(0)
	if gw != expectedGw {
		t.Errorf("expected gw %s, got %s", expectedGw, gw)
	}
	release()
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/firecracker/ -run TestAllocate -v`
Expected: FAIL — old allocateSubnet doesn't support PID tracking or release.

- [ ] **Step 3: Rewrite allocateSubnet with PID tracking**

Replace the `allocateSubnet` function in `internal/runtime/firecracker/network.go`:

```go
package firecracker

import (
	"bufio"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

// subnetForIndex computes the gateway and guest IPs for a given allocation index.
// Each index maps to a /30 subnet within 172.16.0.0/12.
func subnetForIndex(index uint32) (gateway, guest string) {
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

type allocEntry struct {
	index uint32
	pid   int
}

// parseAllocFile reads the PID-tracked allocation file.
// Detects old 4-byte binary counter format and resets to empty.
func parseAllocFile(data []byte) []allocEntry {
	// Detect old binary counter format (exactly 4 bytes, not valid text)
	if len(data) == 4 {
		return nil
	}

	var entries []allocEntry
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		idx, err := strconv.ParseUint(parts[0], 10, 32)
		if err != nil {
			continue
		}
		pid, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}
		entries = append(entries, allocEntry{index: uint32(idx), pid: pid})
	}
	return entries
}

func writeAllocFile(f *os.File, entries []allocEntry) error {
	f.Seek(0, 0)
	f.Truncate(0)
	for _, e := range entries {
		fmt.Fprintf(f, "%d:%d\n", e.index, e.pid)
	}
	return nil
}

func isPIDAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	// EPERM means the process exists but we lack permission to signal it
	return err == nil || errors.Is(err, syscall.EPERM)
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

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return "", "", nil, fmt.Errorf("locking allocation file: %w", err)
	}

	// Read from the locked FD, not os.ReadFile (which opens a separate FD)
	f.Seek(0, 0)
	data, _ := io.ReadAll(f)
	entries := parseAllocFile(data)

	// Find reclaimable entries (dead PIDs)
	var alive []allocEntry
	var reclaimable []uint32
	for _, e := range entries {
		if isPIDAlive(e.pid) {
			alive = append(alive, e)
		} else {
			reclaimable = append(reclaimable, e.index)
		}
	}

	// Find the lowest available index: either a gap in the alive sequence or max+1
	var index uint32
	if len(alive) > 0 {
		// Sort alive by index to find gaps
		sort.Slice(alive, func(i, j int) bool { return alive[i].index < alive[j].index })
		// Look for gaps in the sequence
		found := false
		var expected uint32
		for _, e := range alive {
			if e.index > expected {
				index = expected
				found = true
				break
			}
			expected = e.index + 1
		}
		if !found {
			index = expected // max + 1
		}
	}
	// else: empty file, index stays 0

	myPID := os.Getpid()
	alive = append(alive, allocEntry{index: index, pid: myPID})
	writeAllocFile(f, alive)

	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	f.Close()

	gw, g := subnetForIndex(index)

	releaseFunc := func() {
		rf, err := os.OpenFile(allocFile, os.O_RDWR, 0o644)
		if err != nil {
			return
		}
		defer rf.Close()
		if err := syscall.Flock(int(rf.Fd()), syscall.LOCK_EX); err != nil {
			return
		}
		defer syscall.Flock(int(rf.Fd()), syscall.LOCK_UN)

		rf.Seek(0, 0)
		data, _ := io.ReadAll(rf)
		entries := parseAllocFile(data)
		var remaining []allocEntry
		for _, e := range entries {
			if e.pid != myPID || e.index != index {
				remaining = append(remaining, e)
			}
		}
		writeAllocFile(rf, remaining)
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

- [ ] **Step 4: Run tests**

Run: `go test ./internal/runtime/firecracker/ -run "TestSubnet|TestAllocate" -v`
Expected: All 5 network tests pass.

Run: `go test ./internal/runtime/firecracker/ -v`
Expected: All existing tests still pass.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/firecracker/network.go internal/runtime/firecracker/network_test.go
git commit -m "feat: add PID-based IP reclamation with stale detection"
```

---

### Task 5: Add vsock Device Config to configureVM

**Files:**
- Modify: `internal/runtime/firecracker/vm.go`

- [ ] **Step 1: Add vsockPath field to vmInstance**

In `internal/runtime/firecracker/vm.go`, update `vmInstance`:

```go
type vmInstance struct {
	cmd        *exec.Cmd
	socketPath string
	vsockPath  string
	virtiofs   []*virtiofsInstance
	tapDevice  string
	guestIP    string
	outIface   string
	releaseIP  func()
}
```

- [ ] **Step 2: Add vsock device config to configureVM**

In `internal/runtime/firecracker/vm.go`, at the end of `configureVM` (before `return nil`), add:

```go
	// Configure vsock device for host-guest communication
	vsockPath := filepath.Join(filepath.Dir(vm.socketPath), "vsock.sock")
	vm.vsockPath = vsockPath
	if err := vm.apiPut("/vsock", map[string]interface{}{
		"vsock_id":  "vsock0",
		"guest_cid": 3,
		"uds_path":  vsockPath,
	}); err != nil {
		return fmt.Errorf("setting vsock: %w", err)
	}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/runtime/firecracker/ -v`
Expected: All existing tests pass (configureVM is only called in real VM path, not unit tests).

- [ ] **Step 4: Commit**

```bash
git add internal/runtime/firecracker/vm.go
git commit -m "feat: add vsock device configuration to Firecracker VM"
```

---

## Chunk 2: vsock Communication — Guest Agent and Host Client

**Prerequisite:** Task 5 (Chunk 1) must be completed first — it adds the `vsockPath` field to `vmInstance` and the vsock device configuration to `configureVM()`.

The critical path: implementing command execution over vsock.

### Task 6: Add vsock Dependency

**Files:**
- Modify: `go.mod`

- [ ] **Step 1: Add vsock dependency**

Run: `go get github.com/mdlayher/vsock@latest`

- [ ] **Step 2: Verify**

Run: `go mod tidy && go vet ./...`
Expected: Clean.

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add github.com/mdlayher/vsock for AF_VSOCK support"
```

---

### Task 7: Guest Init Agent — vsock Event Loop

**Files:**
- Modify: `cmd/warden-init/main.go`

This binary runs inside the microVM as PID 1. It cannot be unit-tested with `net.Pipe()` directly because it uses `vsock.Listen()`. Instead, we extract the core logic into testable functions and test those.

- [ ] **Step 1: Write the guest agent implementation**

Replace `cmd/warden-init/main.go` with:

```go
package main

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"github.com/mdlayher/vsock"
	"github.com/winler/warden/internal/protocol"
)

const vsockPort = 1024

func main() {
	if err := mountFilesystems(); err != nil {
		fmt.Fprintf(os.Stderr, "warden-init: mount error: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintln(os.Stderr, "warden-init: ready, listening on vsock port", vsockPort)

	exitCode, err := listenAndServe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warden-init: %v\n", err)
		os.Exit(1)
	}
	os.Exit(exitCode)
}

func listenAndServe() (int, error) {
	l, err := vsock.Listen(vsockPort, nil)
	if err != nil {
		return 1, fmt.Errorf("vsock listen: %w", err)
	}
	defer l.Close()

	conn, err := l.Accept()
	if err != nil {
		return 1, fmt.Errorf("vsock accept: %w", err)
	}
	defer conn.Close()

	return handleConnection(conn)
}

func handleConnection(conn io.ReadWriter) (int, error) {
	// Read ExecMessage
	raw, err := protocol.ReadMessage(conn)
	if err != nil {
		return 1, fmt.Errorf("reading exec message: %w", err)
	}
	execMsg, ok := raw.(*protocol.ExecMessage)
	if !ok {
		return 1, fmt.Errorf("expected ExecMessage, got %T", raw)
	}

	// Build command
	cmd := exec.Command(execMsg.Command, execMsg.Args...)
	cmd.Dir = execMsg.Workdir
	cmd.Env = execMsg.Env
	if len(cmd.Env) == 0 {
		cmd.Env = os.Environ()
	}

	// Set UID/GID if specified
	if execMsg.UID != 0 || execMsg.GID != 0 {
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Credential: &syscall.Credential{
				Uid: uint32(execMsg.UID),
				Gid: uint32(execMsg.GID),
			},
		}
	}

	// Set up pipes
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 1, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return 1, fmt.Errorf("stderr pipe: %w", err)
	}

	// Mutex for writes to the connection
	var mu sync.Mutex
	writeMsg := func(msg interface{}) error {
		mu.Lock()
		defer mu.Unlock()
		return protocol.WriteMessage(conn, msg)
	}

	// Start command
	if err := cmd.Start(); err != nil {
		// Command not found → exit code 127
		writeMsg(&protocol.ExitMessage{Code: 127})
		return 127, nil
	}

	// Start signal reader goroutine
	go func() {
		for {
			raw, err := protocol.ReadMessage(conn)
			if err != nil {
				return
			}
			sigMsg, ok := raw.(*protocol.SignalMessage)
			if !ok {
				continue
			}
			sig := parseSignal(sigMsg.Signal)
			if sig != 0 && cmd.Process != nil {
				syscall.Kill(cmd.Process.Pid, sig)
			}
		}
	}()

	// Stream stdout and stderr
	var wg sync.WaitGroup
	streamOutput := func(r io.Reader, streamType string) {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				encoded := base64.StdEncoding.EncodeToString(buf[:n])
				writeMsg(&protocol.OutputMessage{
					Type: streamType,
					Data: encoded,
				})
			}
			if err != nil {
				return
			}
		}
	}

	wg.Add(2)
	go streamOutput(stdout, "stdout")
	go streamOutput(stderr, "stderr")

	// Wait for output streams to drain, then for command to exit
	wg.Wait()
	exitCode := 0
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	writeMsg(&protocol.ExitMessage{Code: exitCode})
	return exitCode, nil
}

func parseSignal(name string) syscall.Signal {
	switch name {
	case "SIGTERM":
		return syscall.SIGTERM
	case "SIGINT":
		return syscall.SIGINT
	case "SIGKILL":
		return syscall.SIGKILL
	case "SIGHUP":
		return syscall.SIGHUP
	default:
		return 0
	}
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
			if !os.IsExist(err) {
				return fmt.Errorf("mounting %s: %w", m.target, err)
			}
		}
	}
	return nil
}
```

- [ ] **Step 2: Write unit test for handleConnection**

Create `cmd/warden-init/main_test.go`:

```go
package main

import (
	"encoding/base64"
	"net"
	"testing"

	"github.com/winler/warden/internal/protocol"
)

func TestHandleConnectionEcho(t *testing.T) {
	guest, host := net.Pipe()
	defer guest.Close()
	defer host.Close()

	// Host sends ExecMessage
	go func() {
		protocol.WriteMessage(host, &protocol.ExecMessage{
			Command: "echo",
			Args:    []string{"hello"},
			Workdir: "/",
			Env:     []string{"PATH=/usr/bin:/bin"},
		})

		// Read responses
		for {
			msg, err := protocol.ReadMessage(host)
			if err != nil {
				return
			}
			switch m := msg.(type) {
			case *protocol.ExitMessage:
				if m.Code != 0 {
					t.Errorf("exit code = %d, want 0", m.Code)
				}
				return
			case *protocol.OutputMessage:
				if m.Type == "stdout" {
					decoded, _ := base64.StdEncoding.DecodeString(m.Data)
					if string(decoded) != "hello\n" {
						t.Errorf("stdout = %q, want %q", decoded, "hello\n")
					}
				}
			}
		}
	}()

	code, err := handleConnection(guest)
	if err != nil {
		t.Fatalf("handleConnection: %v", err)
	}
	if code != 0 {
		t.Errorf("return code = %d, want 0", code)
	}
}

func TestHandleConnectionNotFound(t *testing.T) {
	guest, host := net.Pipe()
	defer guest.Close()
	defer host.Close()

	go func() {
		protocol.WriteMessage(host, &protocol.ExecMessage{
			Command: "/nonexistent/binary",
			Workdir: "/",
			Env:     []string{"PATH=/usr/bin:/bin"},
		})

		for {
			msg, err := protocol.ReadMessage(host)
			if err != nil {
				return
			}
			if exit, ok := msg.(*protocol.ExitMessage); ok {
				if exit.Code != 127 {
					t.Errorf("exit code = %d, want 127", exit.Code)
				}
				return
			}
		}
	}()

	code, _ := handleConnection(guest)
	if code != 127 {
		t.Errorf("return code = %d, want 127", code)
	}
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./cmd/warden-init/ -run TestHandleConnection -v`
Expected: Both tests pass.

- [ ] **Step 4: Commit**

```bash
git add cmd/warden-init/main.go cmd/warden-init/main_test.go
git commit -m "feat: implement vsock event loop in guest init agent"
```

---

### Task 8: Host-side vsock Client — Run() Implementation

**Files:**
- Modify: `internal/runtime/firecracker/firecracker.go`

- [ ] **Step 1: Implement the host-side vsock dial and message loop**

Replace the `Run` method in `internal/runtime/firecracker/firecracker.go`:

```go
// Run executes a command in a Firecracker microVM.
func (f *FirecrackerRuntime) Run(cfg config.SandboxConfig, command []string) (int, error) {
	vm, err := startVM(cfg, command)
	if err != nil {
		return 1, err
	}
	defer vm.cleanup()

	timeout, err := shared.ParseTimeout(cfg.Timeout)
	if err != nil {
		return 1, err
	}

	// Connect to guest agent via vsock UDS
	conn, err := dialGuest(vm.vsockPath, 1024, 5*time.Second)
	if err != nil {
		return 1, err
	}
	defer conn.Close()

	// Send ExecMessage
	execMsg := &protocol.ExecMessage{
		Command: command[0],
		Workdir: cfg.Workdir,
		Env:     cfg.Env,
	}
	if len(command) > 1 {
		execMsg.Args = command[1:]
	}
	if err := protocol.WriteMessage(conn, execMsg); err != nil {
		return 1, fmt.Errorf("sending exec message: %w", err)
	}

	// Set up signal handling
	var mu sync.Mutex
	writeSignal := func(sigName string) {
		mu.Lock()
		defer mu.Unlock()
		protocol.WriteMessage(conn, &protocol.SignalMessage{Signal: sigName})
	}

	// Map os.Signal to protocol signal names (os.Signal.String() returns
	// "terminated"/"interrupt", but the guest expects "SIGTERM"/"SIGINT")
	signalName := func(sig os.Signal) string {
		switch sig {
		case syscall.SIGTERM:
			return "SIGTERM"
		case syscall.SIGINT:
			return "SIGINT"
		case syscall.SIGKILL:
			return "SIGKILL"
		default:
			return sig.String()
		}
	}

	cleanup := shared.SignalHandler(
		func(sig os.Signal) {
			writeSignal(signalName(sig))
		},
		func() {
			if vm.cmd != nil && vm.cmd.Process != nil {
				vm.cmd.Process.Kill()
			}
		},
	)
	defer cleanup()

	// Timeout watchdog
	timedOut := false
	if timeout > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		go func() {
			<-ctx.Done()
			if ctx.Err() == context.DeadlineExceeded {
				timedOut = true
				fmt.Fprintf(os.Stderr, "warden: killed (timeout after %s)\n", cfg.Timeout)
				writeSignal("SIGTERM")
				time.Sleep(10 * time.Second)
				if vm.cmd != nil && vm.cmd.Process != nil {
					vm.cmd.Process.Kill()
				}
			}
		}()
	}

	// Read loop: dispatch Output and Exit messages
	exitCode := 0
	for {
		raw, err := protocol.ReadMessage(conn)
		if err != nil {
			// Connection closed or error — VM likely died
			if timedOut {
				return shared.TimeoutExitCode, nil
			}
			return 1, fmt.Errorf("reading from guest: %w", err)
		}
		switch msg := raw.(type) {
		case *protocol.OutputMessage:
			decoded, err := base64.StdEncoding.DecodeString(msg.Data)
			if err != nil {
				continue
			}
			if msg.Type == "stdout" {
				os.Stdout.Write(decoded)
			} else {
				os.Stderr.Write(decoded)
			}
		case *protocol.ExitMessage:
			exitCode = msg.Code
			if timedOut {
				return shared.TimeoutExitCode, nil
			}
			if m := shared.ExitCodeMessage(exitCode, cfg.Memory); m != "" {
				fmt.Fprintln(os.Stderr, m)
			}
			return exitCode, nil
		}
	}
}
```

- [ ] **Step 2: Add dialGuest function**

Add to `internal/runtime/firecracker/firecracker.go`:

```go
// dialGuest connects to the guest agent via the vsock UDS.
// Polls every 10ms until connection succeeds or timeout.
func dialGuest(vsockUDS string, port uint32, timeout time.Duration) (net.Conn, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.Dial("unix", vsockUDS)
		if err == nil {
			// Send connect request for the vsock port
			// Firecracker's vsock UDS expects "CONNECT <port>\n"
			fmt.Fprintf(conn, "CONNECT %d\n", port)
			buf := make([]byte, 64)
			n, err := conn.Read(buf)
			if err == nil && n > 0 && string(buf[:2]) == "OK" {
				return conn, nil
			}
			conn.Close()
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil, fmt.Errorf("warden: guest agent did not start within %s", timeout)
}
```

- [ ] **Step 3: Update imports**

Update imports in `firecracker.go`:

```go
import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/winler/warden/internal/config"
	"github.com/winler/warden/internal/protocol"
	"github.com/winler/warden/internal/runtime"
	"github.com/winler/warden/internal/runtime/shared"
)
```

Remove the unused `"os/exec"` import (no longer needed since `Run()` doesn't use `exec.ExitError`). Keep `"path/filepath"` (used by `Preflight` and `DryRun`) and add `"syscall"` (used by signal name mapping).

- [ ] **Step 4: Verify compilation**

Run: `go vet ./internal/runtime/firecracker/`
Expected: Clean (no vet errors).

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/firecracker/firecracker.go
git commit -m "feat: implement host-side vsock client for Firecracker command execution"
```

---

### Task 9: Host-side vsock Client — Unit Tests

**Files:**
- Modify: `internal/runtime/firecracker/firecracker_test.go`

- [ ] **Step 1: Write unit tests using net.Pipe**

Replace `internal/runtime/firecracker/firecracker_test.go` with:

```go
package firecracker

import (
	"encoding/base64"
	"net"
	"testing"

	"github.com/winler/warden/internal/protocol"
)

func TestPreflightNoKVM(t *testing.T) {
	rt := &FirecrackerRuntime{}
	err := rt.Preflight()
	// Should fail unless running on a machine with /dev/kvm
	if err == nil {
		t.Skip("running on machine with /dev/kvm, cannot test Preflight failure")
	}
}

// TestHostReadLoop verifies the host-side message dispatch.
// Simulates a guest sending OutputMessages and an ExitMessage.
func TestHostReadLoop(t *testing.T) {
	guest, host := net.Pipe()
	defer guest.Close()
	defer host.Close()

	// Simulate guest: send stdout, stderr, then exit
	go func() {
		encoded := base64.StdEncoding.EncodeToString([]byte("hello"))
		protocol.WriteMessage(guest, &protocol.OutputMessage{Type: "stdout", Data: encoded})

		errEncoded := base64.StdEncoding.EncodeToString([]byte("warning"))
		protocol.WriteMessage(guest, &protocol.OutputMessage{Type: "stderr", Data: errEncoded})

		protocol.WriteMessage(guest, &protocol.ExitMessage{Code: 42})
	}()

	// Read loop (same logic as Run, extracted for testing)
	exitCode := -1
	for {
		raw, err := protocol.ReadMessage(host)
		if err != nil {
			t.Fatalf("ReadMessage: %v", err)
		}
		switch msg := raw.(type) {
		case *protocol.OutputMessage:
			// Just verify we can decode
			_, err := base64.StdEncoding.DecodeString(msg.Data)
			if err != nil {
				t.Errorf("base64 decode: %v", err)
			}
		case *protocol.ExitMessage:
			exitCode = msg.Code
		}
		if exitCode >= 0 {
			break
		}
	}

	if exitCode != 42 {
		t.Errorf("exit code = %d, want 42", exitCode)
	}
}

// TestExecMessageSend verifies ExecMessage is correctly sent.
func TestExecMessageSend(t *testing.T) {
	guest, host := net.Pipe()
	defer guest.Close()
	defer host.Close()

	go func() {
		msg := &protocol.ExecMessage{
			Command: "echo",
			Args:    []string{"test"},
			Workdir: "/tmp",
			Env:     []string{"FOO=bar"},
		}
		if err := protocol.WriteMessage(host, msg); err != nil {
			t.Errorf("WriteMessage: %v", err)
		}
	}()

	raw, err := protocol.ReadMessage(guest)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	exec, ok := raw.(*protocol.ExecMessage)
	if !ok {
		t.Fatalf("got %T, want *ExecMessage", raw)
	}
	if exec.Command != "echo" {
		t.Errorf("command = %q, want echo", exec.Command)
	}
	if len(exec.Args) != 1 || exec.Args[0] != "test" {
		t.Errorf("args = %v, want [test]", exec.Args)
	}
	if exec.Workdir != "/tmp" {
		t.Errorf("workdir = %q, want /tmp", exec.Workdir)
	}
}
```

- [ ] **Step 2: Add dialGuest handshake test**

Append to `internal/runtime/firecracker/firecracker_test.go`:

```go
// TestDialGuestHandshake verifies the CONNECT/OK vsock UDS protocol.
func TestDialGuestHandshake(t *testing.T) {
	// Create a Unix socket that simulates Firecracker's vsock UDS
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "vsock.sock")

	l, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()

	// Simulate Firecracker: accept connection, read CONNECT, respond OK
	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 64)
		n, _ := conn.Read(buf)
		if string(buf[:n]) != "CONNECT 1024\n" {
			t.Errorf("expected CONNECT 1024, got %q", buf[:n])
		}
		conn.Write([]byte("OK 1024\n"))
		// Keep connection open briefly for the test
		buf2 := make([]byte, 1)
		conn.Read(buf2)
	}()

	conn, err := dialGuest(sockPath, 1024, 2*time.Second)
	if err != nil {
		t.Fatalf("dialGuest: %v", err)
	}
	conn.Close()
}
```

Add `"path/filepath"` and `"time"` to the test file imports if not already present.

- [ ] **Step 3: Run tests**

Run: `go test ./internal/runtime/firecracker/ -run "TestHostReadLoop|TestExecMessageSend|TestDialGuestHandshake" -v`
Expected: All three tests pass.

- [ ] **Step 3: Commit**

```bash
git add internal/runtime/firecracker/firecracker_test.go
git commit -m "test: add unit tests for host-side vsock message dispatch"
```

---

## Chunk 3: Automated Setup

### Task 10: Setup Download Helpers

**Files:**
- Create: `internal/runtime/firecracker/setup.go`

- [ ] **Step 1: Implement download and verification helpers**

Create `internal/runtime/firecracker/setup.go`:

```go
package firecracker

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	firecrackerVersion  = "v1.15.0"
	firecrackerURL      = "https://github.com/firecracker-microvm/firecracker/releases/download/" + firecrackerVersion + "/firecracker-" + firecrackerVersion + "-x86_64.tgz"
	firecrackerChecksum = "00cadf7f21e709e939dc0c8d16e2d2ce7b975a62bec6c50f74b421cc8ab3cab4"
	firecrackerBinName  = "release-" + firecrackerVersion + "-x86_64/firecracker-" + firecrackerVersion + "-x86_64"

	virtiofsdVersion  = "v1.13.3"
	virtiofsdURL      = "https://gitlab.com/virtio-fs/virtiofsd/-/archive/" + virtiofsdVersion + "/virtiofsd-" + virtiofsdVersion + ".tar.gz"
	virtiofsdChecksum = "9d5e67e7b19f52a8d3c411acf9beed6206e9352226cbf1e2bdaa4ed609a927ce"
)

// downloadAndVerify downloads a URL and verifies its SHA256 checksum.
// Returns the path to the downloaded temp file.
func downloadAndVerify(url, expectedChecksum string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("downloading %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("downloading %s: HTTP %d", url, resp.StatusCode)
	}

	tmpFile, err := os.CreateTemp("", "warden-download-*")
	if err != nil {
		return "", err
	}

	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmpFile, hasher), resp.Body); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("downloading %s: %w", url, err)
	}
	tmpFile.Close()

	checksum := hex.EncodeToString(hasher.Sum(nil))
	if checksum != expectedChecksum {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("checksum mismatch for %s: got %s, want %s", url, checksum, expectedChecksum)
	}

	return tmpFile.Name(), nil
}

// extractFirecrackerBinary extracts the firecracker binary from the release tarball.
func extractFirecrackerBinary(tarballPath, destPath string) error {
	f, err := os.Open(tarballPath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar: %w", err)
		}
		if hdr.Name == firecrackerBinName {
			os.MkdirAll(filepath.Dir(destPath), 0o755)
			out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			return out.Close()
		}
	}
	return fmt.Errorf("binary %s not found in tarball", firecrackerBinName)
}

// buildVirtiofsd downloads the virtiofsd source and builds it inside a Docker container.
func buildVirtiofsd(tarballPath, destPath string) error {
	// Extract source to temp dir
	tmpDir, err := os.MkdirTemp("", "warden-virtiofsd-build-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	// Extract tarball
	f, err := os.Open(tarballPath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar: %w", err)
		}

		target := filepath.Join(tmpDir, hdr.Name)
		// Path traversal protection
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(tmpDir)+string(os.PathSeparator)) {
			continue
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			os.MkdirAll(target, 0o755)
		case tar.TypeReg:
			os.MkdirAll(filepath.Dir(target), 0o755)
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY, os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}
			io.Copy(out, tr)
			out.Close()
		}
	}

	// Find the source directory (virtiofsd-v1.13.3/)
	srcDir := filepath.Join(tmpDir, "virtiofsd-"+virtiofsdVersion)

	// Build using Docker with Rust toolchain
	cmd := exec.Command("docker", "run", "--rm",
		"-v", srcDir+":/src",
		"-w", "/src",
		"rust:latest",
		"cargo", "build", "--release",
	)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("building virtiofsd: %w", err)
	}

	// Copy binary out
	builtBin := filepath.Join(srcDir, "target", "release", "virtiofsd")
	return copyBinary(builtBin, destPath)
}

func copyBinary(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	os.MkdirAll(filepath.Dir(dst), 0o755)
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

// fileMatchesChecksum checks if a file exists and matches the expected SHA256.
func fileMatchesChecksum(path, expectedChecksum string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return false
	}
	return hex.EncodeToString(hasher.Sum(nil)) == expectedChecksum
}

// SetupDirs creates the Firecracker directory structure.
func SetupDirs(homeDir string) error {
	dirs := []string{
		filepath.Join(homeDir, ".warden", "firecracker", "kernel"),
		filepath.Join(homeDir, ".warden", "firecracker", "rootfs"),
		filepath.Join(homeDir, ".warden", "firecracker", "bin"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// DownloadKernelSetup downloads the kernel for setup (with idempotency check).
func DownloadKernelSetup(homeDir string) error {
	dest := defaultKernelPath(homeDir)
	if fileMatchesChecksum(dest, kernelChecksum) {
		return nil // already installed
	}

	fmt.Fprintf(os.Stderr, "  Downloading kernel %s...\n", kernelVersion)
	return downloadKernel(dest)
}

// DownloadFirecracker downloads and extracts the Firecracker binary.
func DownloadFirecracker(homeDir string) error {
	dest := filepath.Join(homeDir, ".warden", "firecracker", "bin", "firecracker")

	// Simple existence check (we don't have a per-binary checksum, just tarball)
	if _, err := os.Stat(dest); err == nil {
		return nil // already installed
	}

	fmt.Fprintf(os.Stderr, "  Downloading Firecracker %s...\n", firecrackerVersion)
	tarball, err := downloadAndVerify(firecrackerURL, firecrackerChecksum)
	if err != nil {
		return err
	}
	defer os.Remove(tarball)

	return extractFirecrackerBinary(tarball, dest)
}

// BuildAndInstallVirtiofsd downloads virtiofsd source and builds via Docker.
func BuildAndInstallVirtiofsd(homeDir string) error {
	dest := filepath.Join(homeDir, ".warden", "firecracker", "bin", "virtiofsd")
	if _, err := os.Stat(dest); err == nil {
		return nil // already installed
	}

	fmt.Fprintf(os.Stderr, "  Downloading virtiofsd %s source...\n", virtiofsdVersion)
	tarball, err := downloadAndVerify(virtiofsdURL, virtiofsdChecksum)
	if err != nil {
		return err
	}
	defer os.Remove(tarball)

	fmt.Fprintln(os.Stderr, "  Building virtiofsd (this may take a few minutes)...")
	return buildVirtiofsd(tarball, dest)
}

// BuildNetsetup builds the warden-netsetup binary.
func BuildNetsetup() error {
	tmpBin := "/tmp/warden-netsetup-build"
	fmt.Fprintln(os.Stderr, "  Building warden-netsetup...")

	cmd := exec.Command("go", "build", "-o", tmpBin, "./cmd/warden-netsetup/")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("building warden-netsetup: %w", err)
	}

	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  warden-netsetup built at", tmpBin)
	fmt.Fprintln(os.Stderr, "  Install with:")
	fmt.Fprintf(os.Stderr, "    sudo install %s /usr/local/bin/warden-netsetup && sudo setcap cap_net_admin+ep /usr/local/bin/warden-netsetup\n", tmpBin)
	return nil
}

// CheckIPForwarding checks if IP forwarding is enabled.
func CheckIPForwarding() {
	data, _ := os.ReadFile("/proc/sys/net/ipv4/ip_forward")
	val := strings.TrimSpace(string(data))
	if val == "1" {
		fmt.Fprintln(os.Stderr, "  IP forwarding: enabled")
	} else {
		fmt.Fprintln(os.Stderr, "  IP forwarding: DISABLED")
		fmt.Fprintln(os.Stderr, "    To enable: sudo sysctl -w net.ipv4.ip_forward=1")
		fmt.Fprintln(os.Stderr, "    To persist: echo 'net.ipv4.ip_forward=1' | sudo tee /etc/sysctl.d/99-warden.conf")
	}
}

// CheckNetsetupCaps checks if warden-netsetup has the required capabilities.
func CheckNetsetupCaps() {
	path := "/usr/local/bin/warden-netsetup"
	if _, err := os.Stat(path); err != nil {
		fmt.Fprintln(os.Stderr, "  warden-netsetup: NOT INSTALLED")
		return
	}
	out, err := exec.Command("getcap", path).Output()
	if err != nil || !strings.Contains(string(out), "cap_net_admin") {
		fmt.Fprintln(os.Stderr, "  warden-netsetup: missing cap_net_admin")
		fmt.Fprintf(os.Stderr, "    Run: sudo setcap cap_net_admin+ep %s\n", path)
	} else {
		fmt.Fprintln(os.Stderr, "  warden-netsetup: OK")
	}
}
```

- [ ] **Step 2: Verify compilation**

Run: `go vet ./internal/runtime/firecracker/`
Expected: Clean.

- [ ] **Step 3: Commit**

```bash
git add internal/runtime/firecracker/setup.go
git commit -m "feat: add download and build helpers for Firecracker setup"
```

---

### Task 11: Rewrite setup.go CLI Command

**Files:**
- Modify: `internal/cli/setup.go`

- [ ] **Step 1: Replace setupFirecracker with automated flow**

Replace the `setupFirecracker` function in `internal/cli/setup.go`:

```go
package cli

import (
	"fmt"
	"os"
	"os/exec"
	goruntime "runtime"

	"github.com/spf13/cobra"
	"github.com/winler/warden/internal/runtime/firecracker"
)

func newSetupCommand() *cobra.Command {
	setup := &cobra.Command{
		Use:   "setup",
		Short: "Set up optional runtime backends",
	}

	fc := &cobra.Command{
		Use:   "firecracker",
		Short: "Set up Firecracker microVM runtime",
		RunE: func(cmd *cobra.Command, args []string) error {
			return setupFirecracker()
		},
	}

	setup.AddCommand(fc)
	return setup
}

func setupFirecracker() error {
	if goruntime.GOOS != "linux" {
		return fmt.Errorf("warden: firecracker is only supported on Linux")
	}

	// Check /dev/kvm
	fmt.Fprintln(os.Stderr, "Setting up Firecracker runtime...")
	fmt.Fprint(os.Stderr, "  Checking /dev/kvm... ")
	if _, err := os.Stat("/dev/kvm"); err != nil {
		fmt.Fprintln(os.Stderr, "NOT FOUND")
		return fmt.Errorf("warden: /dev/kvm not available. Ensure KVM is enabled")
	}
	fmt.Fprintln(os.Stderr, "OK")

	// Check Docker (needed for virtiofsd build and rootfs building)
	fmt.Fprint(os.Stderr, "  Checking Docker... ")
	if _, err := exec.LookPath("docker"); err != nil {
		fmt.Fprintln(os.Stderr, "NOT FOUND")
		return fmt.Errorf("warden: docker is required for Firecracker setup (virtiofsd build, rootfs building)")
	}
	fmt.Fprintln(os.Stderr, "OK")

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	// Create directory structure
	if err := firecracker.SetupDirs(homeDir); err != nil {
		return fmt.Errorf("creating directories: %w", err)
	}

	// Download kernel
	if err := firecracker.DownloadKernelSetup(homeDir); err != nil {
		return fmt.Errorf("kernel: %w", err)
	}

	// Download Firecracker binary
	if err := firecracker.DownloadFirecracker(homeDir); err != nil {
		return fmt.Errorf("firecracker: %w", err)
	}

	// Build virtiofsd
	if err := firecracker.BuildAndInstallVirtiofsd(homeDir); err != nil {
		return fmt.Errorf("virtiofsd: %w", err)
	}

	// Build warden-netsetup
	if err := firecracker.BuildNetsetup(); err != nil {
		return fmt.Errorf("warden-netsetup: %w", err)
	}

	// System checks
	fmt.Fprintln(os.Stderr, "\nSystem configuration:")
	firecracker.CheckIPForwarding()
	firecracker.CheckNetsetupCaps()

	// Verification
	fmt.Fprintln(os.Stderr, "\nVerification:")
	rt := &firecracker.FirecrackerRuntime{}
	if err := rt.Preflight(); err != nil {
		fmt.Fprintf(os.Stderr, "  Preflight check: FAILED — %v\n", err)
		fmt.Fprintln(os.Stderr, "  Some components may need manual installation. Re-run to check status.")
	} else {
		fmt.Fprintln(os.Stderr, "  Preflight check: PASSED")
		fmt.Fprintln(os.Stderr, "\nFirecracker runtime is ready! Use --runtime firecracker to use it.")
	}

	return nil
}
```

- [ ] **Step 2: Verify compilation**

Run: `go vet ./internal/cli/`
Expected: Clean.

- [ ] **Step 3: Run all tests**

Run: `go test ./... -count=1`
Expected: All existing tests pass.

- [ ] **Step 4: Commit**

```bash
git add internal/cli/setup.go
git commit -m "feat: automate warden setup firecracker with downloads and builds"
```

---

### Task 12: Final Verification

**Files:** None (verification only)

- [ ] **Step 1: Run full test suite**

Run: `go test ./... -count=1 -v`
Expected: All tests pass.

- [ ] **Step 2: Run vet**

Run: `go vet ./...`
Expected: Clean.

- [ ] **Step 3: Build binaries**

Run: `go build -o /dev/null ./cmd/warden/ && CGO_ENABLED=0 go build -o /dev/null ./cmd/warden-init/`
Expected: Both build successfully.

- [ ] **Step 4: Dry-run verification**

Run: `go run ./cmd/warden/ run --runtime firecracker --dry-run -- echo hello`
Expected: Prints VM config JSON. If Chunk 1 was completed first, the kernel path should show `vmlinux-6.1.155`.

- [ ] **Step 5: Commit any fixups if needed**

Only if previous steps revealed issues.
