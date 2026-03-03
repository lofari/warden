# Warden Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use golem-superpowers:golem-execution during each Golem iteration.

**Goal:** Build a CLI tool that sandboxes any command inside a Docker container with declarative filesystem, network, and resource controls.

**Architecture:** Go CLI shells out to `docker` CLI. Config parsed from `.warden.yaml` profiles merged with CLI flags. Tool installation via embedded shell scripts that build cached Docker images.

**Tech Stack:** Go 1.21+, Cobra (CLI), gopkg.in/yaml.v3, go:embed, os/exec

---

## Task 1: Project Scaffold and CLI Skeleton

**Files:**
- Create: `go.mod`
- Create: `cmd/warden/main.go`
- Create: `internal/cli/root.go`
- Create: `internal/cli/root_test.go`

**Step 1: Initialize Go module**

Run:
```bash
cd /home/winler/projects/warden
go mod init github.com/winler/warden
```

**Step 2: Install cobra dependency**

Run:
```bash
go get github.com/spf13/cobra@latest
```

**Step 3: Write test for root command existence**

```go
// internal/cli/root_test.go
package cli

import (
	"testing"
)

func TestRootCommandHasRunSubcommand(t *testing.T) {
	root := NewRootCommand()
	found := false
	for _, cmd := range root.Commands() {
		if cmd.Name() == "run" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("root command must have a 'run' subcommand")
	}
}

func TestRunCommandRequiresArgs(t *testing.T) {
	root := NewRootCommand()
	root.SetArgs([]string{"run"})
	err := root.Execute()
	if err == nil {
		t.Fatal("run command with no args should return an error")
	}
}
```

**Step 4: Run test to verify it fails**

Run: `go test ./internal/cli/ -v`
Expected: FAIL — `NewRootCommand` undefined

**Step 5: Implement root command with run subcommand**

```go
// internal/cli/root.go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "warden",
		Short: "Secure sandbox for AI coding agents",
	}

	run := &cobra.Command{
		Use:   "run -- <command...>",
		Short: "Run a command in a sandboxed container",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("not implemented")
			return nil
		},
	}

	root.AddCommand(run)
	return root
}
```

**Step 6: Write main.go entrypoint**

```go
// cmd/warden/main.go
package main

import (
	"os"

	"github.com/winler/warden/internal/cli"
)

func main() {
	root := cli.NewRootCommand()
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
```

**Step 7: Run tests to verify they pass**

Run: `go test ./internal/cli/ -v`
Expected: PASS

**Step 8: Verify binary builds and runs**

Run:
```bash
go build -o warden ./cmd/warden
./warden run -- echo hello
./warden run 2>&1 || true
```
Expected: first prints "not implemented", second shows error about missing args

**Step 9: Commit**

```bash
git add cmd/ internal/ go.mod go.sum
git commit -m "feat: project scaffold with cobra CLI skeleton"
```

---

## Task 2: Config Types, Defaults, and YAML Parsing

**Files:**
- Create: `internal/config/types.go`
- Create: `internal/config/defaults.go`
- Create: `internal/config/parse.go`
- Create: `internal/config/parse_test.go`

**Step 1: Write test for default config**

```go
// internal/config/parse_test.go
package config

import (
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Image != "ubuntu:24.04" {
		t.Errorf("default image = %q, want ubuntu:24.04", cfg.Image)
	}
	if cfg.Network != false {
		t.Error("default network should be false")
	}
	if cfg.Memory != "8g" {
		t.Errorf("default memory = %q, want 8g", cfg.Memory)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -v`
Expected: FAIL — types not defined

**Step 3: Define config types and defaults**

```go
// internal/config/types.go
package config

type Mount struct {
	Path string `yaml:"path"`
	Mode string `yaml:"mode"` // "ro" or "rw"
}

type SandboxConfig struct {
	Image   string   `yaml:"image"`
	Tools   []string `yaml:"tools"`
	Mounts  []Mount  `yaml:"mounts"`
	Network bool     `yaml:"network"`
	Timeout string   `yaml:"timeout"`
	Memory  string   `yaml:"memory"`
	CPUs    int      `yaml:"cpus"`
	Workdir string   `yaml:"workdir"`
}
```

```go
// internal/config/defaults.go
package config

import "runtime"

func DefaultConfig() SandboxConfig {
	return SandboxConfig{
		Image:   "ubuntu:24.04",
		Network: false,
		Memory:  "8g",
		CPUs:    runtime.NumCPU(),
	}
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -v`
Expected: PASS

**Step 5: Write test for YAML parsing**

```go
func TestParseWardenYAML(t *testing.T) {
	yaml := `
default:
  image: ubuntu:24.04
  tools: [node, python]
  mounts:
    - path: "."
      mode: rw
  network: false
  timeout: 1h
  memory: 4g
  cpus: 2

profiles:
  web:
    extends: default
    network: true
    tools: [node, python, go]
`
	file, err := ParseWardenYAML([]byte(yaml))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if file.Default.Image != "ubuntu:24.04" {
		t.Errorf("default image = %q, want ubuntu:24.04", file.Default.Image)
	}
	if len(file.Default.Tools) != 2 {
		t.Errorf("default tools count = %d, want 2", len(file.Default.Tools))
	}
	web, ok := file.Profiles["web"]
	if !ok {
		t.Fatal("missing 'web' profile")
	}
	if web.Extends != "default" {
		t.Errorf("web extends = %q, want default", web.Extends)
	}
	if web.Network == nil || *web.Network != true {
		t.Error("web network should be true")
	}
}
```

**Step 6: Run test to verify it fails**

Run: `go test ./internal/config/ -v`
Expected: FAIL — `ParseWardenYAML` undefined

**Step 7: Implement YAML parsing**

The file format needs a `WardenFile` struct and a `ProfileEntry` struct that uses pointers for optional fields (so we can distinguish "not set" from "set to zero value" during merging).

```go
// internal/config/parse.go
package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

type ProfileEntry struct {
	Extends string   `yaml:"extends"`
	Image   *string  `yaml:"image"`
	Tools   []string `yaml:"tools"`
	Mounts  []Mount  `yaml:"mounts"`
	Network *bool    `yaml:"network"`
	Timeout *string  `yaml:"timeout"`
	Memory  *string  `yaml:"memory"`
	CPUs    *int     `yaml:"cpus"`
	Workdir *string  `yaml:"workdir"`
}

type WardenFile struct {
	Default  ProfileEntry             `yaml:"default"`
	Profiles map[string]ProfileEntry  `yaml:"profiles"`
}

func ParseWardenYAML(data []byte) (*WardenFile, error) {
	var file WardenFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parsing .warden.yaml: %w", err)
	}
	return &file, nil
}
```

**Step 8: Install yaml.v3 and run tests**

Run:
```bash
go get gopkg.in/yaml.v3@latest
go test ./internal/config/ -v
```
Expected: PASS

**Step 9: Commit**

```bash
git add internal/config/ go.mod go.sum
git commit -m "feat: config types, defaults, and YAML parsing"
```

---

## Task 3: Config Merging

**Files:**
- Create: `internal/config/merge.go`
- Create: `internal/config/merge_test.go`

**Step 1: Write test for profile-to-config conversion**

```go
// internal/config/merge_test.go
package config

import (
	"testing"
)

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool   { return &b }
func intPtr(i int) *int      { return &i }

func TestApplyProfile(t *testing.T) {
	base := DefaultConfig()
	profile := ProfileEntry{
		Image:   strPtr("alpine:3.20"),
		Network: boolPtr(true),
		Memory:  strPtr("4g"),
	}
	result := ApplyProfile(base, profile)
	if result.Image != "alpine:3.20" {
		t.Errorf("image = %q, want alpine:3.20", result.Image)
	}
	if result.Network != true {
		t.Error("network should be true")
	}
	if result.Memory != "4g" {
		t.Errorf("memory = %q, want 4g", result.Memory)
	}
	// Unset fields should keep base values
	if result.CPUs != base.CPUs {
		t.Errorf("cpus = %d, want %d (base default)", result.CPUs, base.CPUs)
	}
}

func TestResolveProfileWithExtends(t *testing.T) {
	yaml := `
default:
  image: ubuntu:24.04
  tools: [node]
  network: false
  timeout: 1h
  memory: 8g
  cpus: 4

profiles:
  strict:
    extends: default
    network: false
    timeout: 30m
`
	file, err := ParseWardenYAML([]byte(yaml))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	cfg, err := ResolveProfile(file, "strict")
	if err != nil {
		t.Fatalf("resolve error: %v", err)
	}
	if cfg.Timeout != "30m" {
		t.Errorf("timeout = %q, want 30m", cfg.Timeout)
	}
	if cfg.Image != "ubuntu:24.04" {
		t.Errorf("image = %q, want ubuntu:24.04 (inherited)", cfg.Image)
	}
	if len(cfg.Tools) != 1 || cfg.Tools[0] != "node" {
		t.Errorf("tools = %v, want [node] (inherited)", cfg.Tools)
	}
}

func TestResolveDefaultProfile(t *testing.T) {
	yaml := `
default:
  image: ubuntu:24.04
  tools: [python]
  memory: 4g
`
	file, err := ParseWardenYAML([]byte(yaml))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	cfg, err := ResolveProfile(file, "")
	if err != nil {
		t.Fatalf("resolve error: %v", err)
	}
	if cfg.Image != "ubuntu:24.04" {
		t.Errorf("image = %q, want ubuntu:24.04", cfg.Image)
	}
	if cfg.Memory != "4g" {
		t.Errorf("memory = %q, want 4g", cfg.Memory)
	}
}

func TestResolveUnknownProfile(t *testing.T) {
	file := &WardenFile{}
	_, err := ResolveProfile(file, "nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown profile")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -v -run TestApply`
Expected: FAIL — `ApplyProfile` undefined

**Step 3: Implement merge logic**

```go
// internal/config/merge.go
package config

import "fmt"

// ApplyProfile overlays a ProfileEntry onto a SandboxConfig.
// Only non-nil fields in the profile override the base.
func ApplyProfile(base SandboxConfig, p ProfileEntry) SandboxConfig {
	if p.Image != nil {
		base.Image = *p.Image
	}
	if p.Tools != nil {
		base.Tools = p.Tools
	}
	if p.Mounts != nil {
		base.Mounts = p.Mounts
	}
	if p.Network != nil {
		base.Network = *p.Network
	}
	if p.Timeout != nil {
		base.Timeout = *p.Timeout
	}
	if p.Memory != nil {
		base.Memory = *p.Memory
	}
	if p.CPUs != nil {
		base.CPUs = *p.CPUs
	}
	if p.Workdir != nil {
		base.Workdir = *p.Workdir
	}
	return base
}

// ResolveProfile resolves a named profile from a WardenFile into a SandboxConfig.
// Empty name resolves the default profile. Handles `extends` chains.
func ResolveProfile(file *WardenFile, name string) (SandboxConfig, error) {
	cfg := DefaultConfig()

	// Always apply default profile first
	cfg = ApplyProfile(cfg, file.Default)

	if name == "" || name == "default" {
		return cfg, nil
	}

	profile, ok := file.Profiles[name]
	if !ok {
		return SandboxConfig{}, fmt.Errorf("unknown profile: %q", name)
	}

	// If extends is set and not "default" (already applied), resolve the parent
	if profile.Extends != "" && profile.Extends != "default" {
		parent, ok := file.Profiles[profile.Extends]
		if !ok {
			return SandboxConfig{}, fmt.Errorf("profile %q extends unknown profile %q", name, profile.Extends)
		}
		cfg = ApplyProfile(cfg, parent)
	}

	cfg = ApplyProfile(cfg, profile)
	return cfg, nil
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/config/merge.go internal/config/merge_test.go
git commit -m "feat: config merging with profile inheritance"
```

---

## Task 4: Mount Resolution and Docker Argument Builder

**Files:**
- Create: `internal/container/args.go`
- Create: `internal/container/args_test.go`

**Step 1: Write test for mount path resolution**

```go
// internal/container/args_test.go
package container

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/winler/warden/internal/config"
)

func TestResolveMounts(t *testing.T) {
	// Create a temp dir to use as a real mount path
	tmp := t.TempDir()

	mounts := []config.Mount{
		{Path: tmp, Mode: "rw"},
	}
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

	mounts := []config.Mount{
		{Path: "project", Mode: "ro"},
	}
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
	args := BuildDockerArgs(cfg, []string{"echo", "hello"})
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
	// The command should be at the end, after the image
	if !strings.HasSuffix(joined, "ubuntu:24.04 echo hello") {
		t.Errorf("args should end with image + command, got: %s", joined)
	}
}

func TestBuildDockerArgsNetworkEnabled(t *testing.T) {
	cfg := config.SandboxConfig{
		Image:   "ubuntu:24.04",
		Network: true,
		Memory:  "8g",
		CPUs:    4,
	}
	args := BuildDockerArgs(cfg, []string{"bash"})
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "--network") {
		t.Error("should not set --network when network is enabled (use Docker default)")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/container/ -v`
Expected: FAIL — package doesn't exist

**Step 3: Implement mount resolution and arg builder**

```go
// internal/container/args.go
package container

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"

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

// BuildDockerArgs translates a SandboxConfig into docker run arguments.
// The command to run is appended after the image name.
func BuildDockerArgs(cfg config.SandboxConfig, command []string) []string {
	args := []string{"run", "--rm"}

	// User mapping
	u, err := user.Current()
	if err == nil {
		args = append(args, "--user", u.Uid+":"+u.Gid)
	}

	// Network
	if !cfg.Network {
		args = append(args, "--network", "none")
	}

	// Resources
	if cfg.Memory != "" {
		args = append(args, "--memory", cfg.Memory)
	}
	if cfg.CPUs > 0 {
		args = append(args, "--cpus", strconv.Itoa(cfg.CPUs))
	}

	// Mounts
	for _, m := range cfg.Mounts {
		args = append(args, "-v", m.Path+":"+m.Path+":"+m.Mode)
	}

	// Workdir
	if cfg.Workdir != "" {
		args = append(args, "-w", cfg.Workdir)
	}

	// Image
	args = append(args, cfg.Image)

	// Command
	args = append(args, command...)

	return args
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/container/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/container/
git commit -m "feat: mount resolution and docker argument builder"
```

---

## Task 5: Docker Prerequisite Check and Image Builder

**Files:**
- Create: `internal/container/docker.go`
- Create: `internal/container/docker_test.go`
- Create: `internal/container/image.go`
- Create: `internal/container/image_test.go`
- Create: `internal/features/features.go`
- Create: `internal/features/features_test.go`
- Create: `features/node.sh`
- Create: `features/python.sh`
- Create: `features/go.sh`
- Create: `features/rust.sh`
- Create: `features/java.sh`

**Step 1: Write test for Docker check**

```go
// internal/container/docker_test.go
package container

import (
	"testing"
)

func TestDockerBinaryPath(t *testing.T) {
	path, err := DockerBinaryPath()
	if err != nil {
		t.Skipf("docker not in PATH: %v", err)
	}
	if path == "" {
		t.Fatal("path should not be empty when err is nil")
	}
}
```

**Step 2: Implement Docker check**

```go
// internal/container/docker.go
package container

import (
	"fmt"
	"os/exec"
	"strings"
)

// DockerBinaryPath finds the docker binary in PATH.
func DockerBinaryPath() (string, error) {
	path, err := exec.LookPath("docker")
	if err != nil {
		return "", fmt.Errorf("warden: docker is not installed")
	}
	return path, nil
}

// CheckDockerReady verifies docker is installed and the daemon is running.
func CheckDockerReady() error {
	dockerPath, err := DockerBinaryPath()
	if err != nil {
		return err
	}
	out, err := exec.Command(dockerPath, "info").CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Cannot connect") || strings.Contains(string(out), "permission denied") {
			return fmt.Errorf("warden: docker daemon is not running")
		}
		return fmt.Errorf("warden: docker check failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}
```

**Step 3: Write test for feature script embedding**

```go
// internal/features/features_test.go
package features

import (
	"testing"
)

func TestGetFeatureScript(t *testing.T) {
	script, err := GetFeatureScript("node")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(script) == 0 {
		t.Fatal("script should not be empty")
	}
}

func TestGetFeatureScriptUnknown(t *testing.T) {
	_, err := GetFeatureScript("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown feature")
	}
}

func TestAvailableFeatures(t *testing.T) {
	features := AvailableFeatures()
	expected := []string{"go", "java", "node", "python", "rust"}
	if len(features) != len(expected) {
		t.Fatalf("got %d features, want %d", len(features), len(expected))
	}
	for i, f := range features {
		if f != expected[i] {
			t.Errorf("feature[%d] = %q, want %q", i, f, expected[i])
		}
	}
}
```

**Step 4: Create feature scripts**

```bash
# features/node.sh
#!/bin/bash
set -euo pipefail
curl -fsSL https://deb.nodesource.com/setup_22.x | bash -
apt-get install -y nodejs
```

```bash
# features/python.sh
#!/bin/bash
set -euo pipefail
apt-get install -y python3 python3-pip python3-venv
ln -sf /usr/bin/python3 /usr/bin/python
```

```bash
# features/go.sh
#!/bin/bash
set -euo pipefail
GO_VERSION="1.23.6"
curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" | tar -C /usr/local -xzf -
echo 'export PATH=$PATH:/usr/local/go/bin' >> /etc/profile.d/go.sh
export PATH=$PATH:/usr/local/go/bin
```

```bash
# features/rust.sh
#!/bin/bash
set -euo pipefail
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y --default-toolchain stable
echo 'source /root/.cargo/env' >> /etc/profile.d/rust.sh
```

```bash
# features/java.sh
#!/bin/bash
set -euo pipefail
apt-get install -y openjdk-21-jdk-headless
```

**Step 5: Implement features embedding**

```go
// internal/features/features.go
package features

import (
	"embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed all:scripts
var scriptsFS embed.FS

// GetFeatureScript returns the contents of a feature install script.
func GetFeatureScript(name string) ([]byte, error) {
	data, err := scriptsFS.ReadFile("scripts/" + name + ".sh")
	if err != nil {
		return nil, fmt.Errorf("unknown feature: %q", name)
	}
	return data, nil
}

// AvailableFeatures returns sorted list of built-in feature names.
func AvailableFeatures() []string {
	entries, _ := scriptsFS.ReadDir("scripts")
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sh") {
			names = append(names, strings.TrimSuffix(e.Name(), ".sh"))
		}
	}
	sort.Strings(names)
	return names
}
```

Note: the feature scripts live at `features/` in the repo root but are embedded from `internal/features/scripts/`. We need to put the `.sh` files under `internal/features/scripts/` for `go:embed` to work (it embeds relative to the Go source file). So the actual file layout is:

- `internal/features/scripts/node.sh`
- `internal/features/scripts/python.sh`
- `internal/features/scripts/go.sh`
- `internal/features/scripts/rust.sh`
- `internal/features/scripts/java.sh`

**Step 6: Write test for image tag computation**

```go
// internal/container/image_test.go
package container

import (
	"testing"
)

func TestImageTag(t *testing.T) {
	tests := []struct {
		base  string
		tools []string
		want  string
	}{
		{"ubuntu:24.04", nil, "ubuntu:24.04"},
		{"ubuntu:24.04", []string{"node"}, "warden:ubuntu-24.04_node"},
		{"ubuntu:24.04", []string{"go", "node"}, "warden:ubuntu-24.04_go_node"},
		{"ubuntu:24.04", []string{"node", "go"}, "warden:ubuntu-24.04_go_node"}, // sorted
		{"alpine:3.20", []string{"python"}, "warden:alpine-3.20_python"},
	}
	for _, tt := range tests {
		got := ImageTag(tt.base, tt.tools)
		if got != tt.want {
			t.Errorf("ImageTag(%q, %v) = %q, want %q", tt.base, tt.tools, got, tt.want)
		}
	}
}
```

**Step 7: Implement image tag and builder**

```go
// internal/container/image.go
package container

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/winler/warden/internal/features"
)

// ImageTag computes the docker image tag for a base image + tool set.
// If no tools, returns the base image as-is.
func ImageTag(base string, tools []string) string {
	if len(tools) == 0 {
		return base
	}
	sorted := make([]string, len(tools))
	copy(sorted, tools)
	sort.Strings(sorted)
	// Replace : with - for tag safety
	safeName := strings.ReplaceAll(base, ":", "-")
	safeName = strings.ReplaceAll(safeName, "/", "-")
	return "warden:" + safeName + "_" + strings.Join(sorted, "_")
}

// ImageExists checks if a docker image exists locally.
func ImageExists(tag string) (bool, error) {
	cmd := exec.Command("docker", "image", "inspect", tag)
	cmd.Stdout = nil
	cmd.Stderr = nil
	err := cmd.Run()
	return err == nil, nil
}

// BuildImage creates a warden image with the specified tools installed.
func BuildImage(base string, tools []string) (string, error) {
	tag := ImageTag(base, tools)

	exists, err := ImageExists(tag)
	if err != nil {
		return "", err
	}
	if exists {
		return tag, nil
	}

	// Create temp build context
	tmpDir, err := os.MkdirTemp("", "warden-build-*")
	if err != nil {
		return "", fmt.Errorf("creating build dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Write feature scripts
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

	// Write Dockerfile
	dockerfile := fmt.Sprintf("FROM %s\nRUN apt-get update && apt-get install -y curl git ca-certificates\nCOPY features/ /tmp/warden-features/\n%s\nRUN rm -rf /tmp/warden-features/ /var/lib/apt/lists/*\n",
		base, strings.Join(runLines, "\n"))

	if err := os.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte(dockerfile), 0o644); err != nil {
		return "", fmt.Errorf("writing Dockerfile: %w", err)
	}

	// Build
	cmd := exec.Command("docker", "build", "-t", tag, tmpDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("building image: %w", err)
	}

	return tag, nil
}
```

**Step 8: Run unit tests**

Run: `go test ./internal/... -v`
Expected: PASS

**Step 9: Commit**

```bash
git add internal/features/ internal/container/docker.go internal/container/docker_test.go internal/container/image.go internal/container/image_test.go
git commit -m "feat: docker check, feature scripts, and image builder"
```

---

## Task 6: Container Execution with TTY, Signals, Timeout, and Exit Codes

**Files:**
- Create: `internal/container/run.go`
- Create: `internal/container/run_test.go`
- Create: `internal/container/timeout.go`
- Create: `internal/container/timeout_test.go`

**Step 1: Write test for timeout duration parsing**

```go
// internal/container/timeout_test.go
package container

import (
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input string
		want  time.Duration
	}{
		{"30m", 30 * time.Minute},
		{"1h", 1 * time.Hour},
		{"2h30m", 2*time.Hour + 30*time.Minute},
		{"90s", 90 * time.Second},
		{"", 0},
	}
	for _, tt := range tests {
		got, err := ParseTimeout(tt.input)
		if err != nil {
			t.Errorf("ParseTimeout(%q) error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseTimeout(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestExitCodeMessage(t *testing.T) {
	tests := []struct {
		code   int
		memory string
		want   string
	}{
		{0, "8g", ""},
		{1, "8g", ""},
		{137, "8g", "warden: killed (out of memory, limit was 8g)"},
	}
	for _, tt := range tests {
		got := ExitCodeMessage(tt.code, tt.memory)
		if got != tt.want {
			t.Errorf("ExitCodeMessage(%d, %q) = %q, want %q", tt.code, tt.memory, got, tt.want)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/container/ -v -run TestParse`
Expected: FAIL — `ParseTimeout` undefined

**Step 3: Implement timeout parsing and exit code translation**

```go
// internal/container/timeout.go
package container

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

// ExitCodeMessage returns a human-readable message for special exit codes.
// Returns empty string for normal exit codes.
func ExitCodeMessage(code int, memory string) string {
	switch code {
	case 137:
		return fmt.Sprintf("warden: killed (out of memory, limit was %s)", memory)
	default:
		return ""
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/container/ -v -run "TestParse|TestExit"`
Expected: PASS

**Step 5: Write test for container name generation**

```go
// internal/container/run_test.go
package container

import (
	"strings"
	"testing"
)

func TestContainerName(t *testing.T) {
	name := ContainerName()
	if !strings.HasPrefix(name, "warden-") {
		t.Errorf("container name %q should start with warden-", name)
	}
}
```

**Step 6: Implement container runner**

```go
// internal/container/run.go
package container

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/winler/warden/internal/config"
)

const timeoutExitCode = 124

// ContainerName generates a unique container name.
func ContainerName() string {
	return fmt.Sprintf("warden-%d", rand.Int63())
}

// RunConfig holds everything needed to run a container.
type RunConfig struct {
	Sandbox config.SandboxConfig
	Command []string
	DryRun  bool
}

// Run executes the sandboxed command. Returns the exit code.
func Run(rc RunConfig) (int, error) {
	// Check Docker is available
	if err := CheckDockerReady(); err != nil {
		return 1, err
	}

	// Resolve image (build if tools requested)
	image := rc.Sandbox.Image
	if len(rc.Sandbox.Tools) > 0 {
		built, err := BuildImage(rc.Sandbox.Image, rc.Sandbox.Tools)
		if err != nil {
			return 1, err
		}
		image = built
	}

	// Update config with resolved image
	resolved := rc.Sandbox
	resolved.Image = image

	// Build docker args
	name := ContainerName()
	args := BuildDockerArgs(resolved, rc.Command)

	// Insert container name and TTY flags after "run"
	extra := []string{"--name", name}
	if isTerminal() {
		extra = append(extra, "-it")
	}
	// Insert after args[0] ("run") and args[1] ("--rm")
	fullArgs := append([]string{args[0], args[1]}, append(extra, args[2:]...)...)

	if rc.DryRun {
		fmt.Println("docker " + joinArgs(fullArgs))
		return 0, nil
	}

	// Parse timeout
	timeout, err := ParseTimeout(resolved.Timeout)
	if err != nil {
		return 1, err
	}

	// Set up context with timeout
	ctx := context.Background()
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	// Run docker
	cmd := exec.CommandContext(ctx, "docker", fullArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Forward signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sigCount := 0
		for sig := range sigCh {
			sigCount++
			if sigCount >= 2 {
				// Force kill on second signal
				exec.Command("docker", "kill", name).Run()
				return
			}
			// Forward first signal — docker run in -it mode handles this
			cmd.Process.Signal(sig)
		}
	}()
	defer signal.Stop(sigCh)

	err = cmd.Run()

	// Handle timeout
	if ctx.Err() == context.DeadlineExceeded {
		// Kill the container
		exec.Command("docker", "kill", name).Run()
		fmt.Fprintf(os.Stderr, "warden: killed (timeout after %s)\n", resolved.Timeout)
		return timeoutExitCode, nil
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code := exitErr.ExitCode()
			if msg := ExitCodeMessage(code, resolved.Memory); msg != "" {
				fmt.Fprintln(os.Stderr, msg)
			}
			return code, nil
		}
		return 1, fmt.Errorf("running container: %w", err)
	}

	return 0, nil
}

func isTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func joinArgs(args []string) string {
	result := ""
	for i, a := range args {
		if i > 0 {
			result += " "
		}
		result += a
	}
	return result
}
```

**Step 7: Run all tests**

Run: `go test ./internal/... -v`
Expected: PASS

**Step 8: Commit**

```bash
git add internal/container/run.go internal/container/run_test.go internal/container/timeout.go internal/container/timeout_test.go
git commit -m "feat: container execution with TTY, signals, timeout, exit codes"
```

---

## Task 7: CLI Wiring

**Files:**
- Modify: `internal/cli/root.go`
- Modify: `internal/cli/root_test.go`

This task wires the cobra flags to the config resolution and container execution pipeline.

**Step 1: Write test for flag parsing**

```go
// Add to internal/cli/root_test.go

func TestRunFlagParsing(t *testing.T) {
	root := NewRootCommand()
	root.SetArgs([]string{
		"run",
		"--mount", "/tmp:rw",
		"--no-network",
		"--memory", "4g",
		"--cpus", "2",
		"--timeout", "30m",
		"--image", "alpine:3.20",
		"--dry-run",
		"--", "echo", "hello",
	})
	err := root.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
```

**Step 2: Rewrite root.go with full flag wiring**

The run command needs to:
1. Parse all CLI flags
2. Try to load `.warden.yaml` if it exists
3. Resolve profile
4. Override with CLI flags
5. Resolve mount paths
6. Call `container.Run`
7. Exit with the container's exit code

```go
// internal/cli/root.go
package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/winler/warden/internal/config"
	"github.com/winler/warden/internal/container"
)

func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "warden",
		Short: "Secure sandbox for AI coding agents",
	}

	var (
		mountFlags  []string
		network     bool
		noNetwork   bool
		timeout     string
		memory      string
		cpus        int
		tools       string
		image       string
		profile     string
		workdir     string
		dryRun      bool
	)

	run := &cobra.Command{
		Use:   "run [flags] -- <command...>",
		Short: "Run a command in a sandboxed container",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// 1. Load .warden.yaml if it exists
			cfg := config.DefaultConfig()
			wardenPath := findWardenYAML()
			baseDir, _ := os.Getwd()

			if wardenPath != "" {
				data, err := os.ReadFile(wardenPath)
				if err != nil {
					return fmt.Errorf("reading %s: %w", wardenPath, err)
				}
				file, err := config.ParseWardenYAML(data)
				if err != nil {
					return err
				}
				baseDir = filepath.Dir(wardenPath)
				resolved, err := config.ResolveProfile(file, profile)
				if err != nil {
					return err
				}
				cfg = resolved
			}

			// 2. CLI flag overrides
			if cmd.Flags().Changed("image") {
				cfg.Image = image
			}
			if cmd.Flags().Changed("memory") {
				cfg.Memory = memory
			}
			if cmd.Flags().Changed("cpus") {
				cfg.CPUs = cpus
			}
			if cmd.Flags().Changed("timeout") {
				cfg.Timeout = timeout
			}
			if cmd.Flags().Changed("workdir") {
				cfg.Workdir = workdir
			}
			if cmd.Flags().Changed("tools") {
				cfg.Tools = strings.Split(tools, ",")
			}
			if cmd.Flags().Changed("network") {
				cfg.Network = true
			}
			if cmd.Flags().Changed("no-network") {
				cfg.Network = false
			}

			// 3. Mount overrides from CLI
			if len(mountFlags) > 0 {
				cfg.Mounts = nil
				for _, m := range mountFlags {
					parts := strings.SplitN(m, ":", 2)
					mode := "ro"
					if len(parts) == 2 {
						mode = parts[1]
					}
					cfg.Mounts = append(cfg.Mounts, config.Mount{Path: parts[0], Mode: mode})
				}
			}

			// 4. Default: mount cwd as rw if no mounts specified
			if len(cfg.Mounts) == 0 {
				cwd, _ := os.Getwd()
				cfg.Mounts = []config.Mount{{Path: cwd, Mode: "rw"}}
			}

			// 5. Resolve mount paths
			resolved, err := container.ResolveMounts(cfg.Mounts, baseDir)
			if err != nil {
				return err
			}
			cfg.Mounts = make([]config.Mount, len(resolved))
			for i, r := range resolved {
				cfg.Mounts[i] = config.Mount{Path: r.Path, Mode: r.Mode}
			}

			// 6. Default workdir to first rw mount
			if cfg.Workdir == "" {
				for _, m := range cfg.Mounts {
					if m.Mode == "rw" {
						cfg.Workdir = m.Path
						break
					}
				}
			}

			// 7. Run
			rc := container.RunConfig{
				Sandbox: cfg,
				Command: args,
				DryRun:  dryRun,
			}
			exitCode, err := container.Run(rc)
			if err != nil {
				return err
			}
			if exitCode != 0 {
				os.Exit(exitCode)
			}
			return nil
		},
	}

	run.Flags().StringArrayVar(&mountFlags, "mount", nil, "Mount host path (path:mode, mode is ro or rw)")
	run.Flags().BoolVar(&network, "network", false, "Enable networking")
	run.Flags().BoolVar(&noNetwork, "no-network", false, "Disable networking")
	run.Flags().StringVar(&timeout, "timeout", "", "Max execution time (e.g. 30m, 2h)")
	run.Flags().StringVar(&memory, "memory", "", "Memory limit (e.g. 4g)")
	run.Flags().IntVar(&cpus, "cpus", 0, "CPU limit")
	run.Flags().StringVar(&tools, "tools", "", "Dev tools to install (comma-separated: node,python,go,rust,java)")
	run.Flags().StringVar(&image, "image", "", "Base image (default: ubuntu:24.04)")
	run.Flags().StringVar(&profile, "profile", "", "Profile from .warden.yaml")
	run.Flags().StringVar(&workdir, "workdir", "", "Working directory inside container")
	run.Flags().BoolVar(&dryRun, "dry-run", false, "Print docker command without executing")

	root.AddCommand(run)
	return root
}

// findWardenYAML walks up from cwd to find .warden.yaml.
func findWardenYAML() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		path := filepath.Join(dir, ".warden.yaml")
		if _, err := os.Stat(path); err == nil {
			return path
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}
```

**Step 3: Run tests**

Run: `go test ./internal/cli/ -v`
Expected: PASS

**Step 4: Build and verify dry-run works**

Run:
```bash
go build -o warden ./cmd/warden
./warden run --mount /tmp:ro --no-network --dry-run -- echo hello
```
Expected: prints the docker run command

**Step 5: Commit**

```bash
git add internal/cli/
git commit -m "feat: wire CLI flags to config resolution and container execution"
```

---

## Task 8: `warden init` and `warden images` Commands

**Files:**
- Create: `internal/cli/init.go`
- Create: `internal/cli/init_test.go`
- Create: `internal/cli/images.go`
- Create: `internal/cli/images_test.go`
- Modify: `internal/cli/root.go` (add subcommands)

**Step 1: Write test for init template**

```go
// internal/cli/init_test.go
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitCreatesWardenYAML(t *testing.T) {
	tmp := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmp)
	defer os.Chdir(origDir)

	err := runInit()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(tmp, ".warden.yaml"))
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if !strings.Contains(string(data), "default:") {
		t.Error("generated file should contain 'default:' section")
	}
}

func TestInitRefusesToOverwrite(t *testing.T) {
	tmp := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmp)
	defer os.Chdir(origDir)

	os.WriteFile(filepath.Join(tmp, ".warden.yaml"), []byte("existing"), 0o644)
	err := runInit()
	if err == nil {
		t.Fatal("should refuse to overwrite existing file")
	}
}
```

**Step 2: Implement init command**

```go
// internal/cli/init.go
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

const initTemplate = `# Warden sandbox configuration
# Docs: https://github.com/winler/warden

default:
  image: ubuntu:24.04
  tools: []
  mounts:
    - path: .
      mode: rw
  network: false
  memory: 8g
`

func newInitCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Generate a starter .warden.yaml",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit()
		},
	}
}

func runInit() error {
	path := ".warden.yaml"
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("warden: %s already exists", path)
	}
	if err := os.WriteFile(path, []byte(initTemplate), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	fmt.Println("Created .warden.yaml")
	return nil
}
```

**Step 3: Write test for images list output**

```go
// internal/cli/images_test.go
package cli

import (
	"testing"
)

func TestParseImageTag(t *testing.T) {
	tests := []struct {
		tag     string
		isWarden bool
	}{
		{"warden:ubuntu-24.04_node", true},
		{"warden:alpine-3.20_go_python", true},
		{"ubuntu:24.04", false},
		{"nginx:latest", false},
	}
	for _, tt := range tests {
		got := isWardenImage(tt.tag)
		if got != tt.isWarden {
			t.Errorf("isWardenImage(%q) = %v, want %v", tt.tag, got, tt.isWarden)
		}
	}
}
```

**Step 4: Implement images command**

```go
// internal/cli/images.go
package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
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

func isWardenImage(tag string) bool {
	return strings.HasPrefix(tag, "warden:")
}

func listImages() error {
	out, err := exec.Command("docker", "images", "--format", "{{.Repository}}:{{.Tag}}\t{{.Size}}\t{{.CreatedSince}}", "warden").CombinedOutput()
	if err != nil {
		return fmt.Errorf("listing images: %w", err)
	}
	output := strings.TrimSpace(string(out))
	if output == "" {
		fmt.Println("No cached warden images.")
		return nil
	}
	fmt.Println("IMAGE\tSIZE\tCREATED")
	fmt.Println(output)
	return nil
}

func pruneImages() error {
	out, err := exec.Command("docker", "images", "--format", "{{.Repository}}:{{.Tag}}", "warden").CombinedOutput()
	if err != nil {
		return fmt.Errorf("listing images: %w", err)
	}
	images := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(images) == 0 || images[0] == "" {
		fmt.Println("No cached warden images.")
		return nil
	}
	for _, img := range images {
		img = strings.TrimSpace(img)
		if img == "" {
			continue
		}
		rmCmd := exec.Command("docker", "rmi", img)
		rmCmd.Stdout = os.Stdout
		rmCmd.Stderr = os.Stderr
		rmCmd.Run()
	}
	fmt.Printf("Removed %d warden image(s).\n", len(images))
	return nil
}
```

**Step 5: Wire init and images into root command**

Add to `NewRootCommand()` in `root.go`, after the `run` command:
```go
root.AddCommand(newInitCommand())
root.AddCommand(newImagesCommand())
```

**Step 6: Run tests**

Run: `go test ./internal/cli/ -v`
Expected: PASS

**Step 7: Build and verify**

Run:
```bash
go build -o warden ./cmd/warden
./warden init --help
./warden images --help
```

**Step 8: Commit**

```bash
git add internal/cli/init.go internal/cli/init_test.go internal/cli/images.go internal/cli/images_test.go internal/cli/root.go
git commit -m "feat: warden init and warden images commands"
```

---

## Task 9: Integration Tests

**Files:**
- Create: `tests/integration_test.go`

These tests require Docker running. They are gated behind the `integration` build tag.

**Step 1: Write integration tests**

```go
//go:build integration

// tests/integration_test.go
package tests

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func wardenBin(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "warden")
	cmd := exec.Command("go", "build", "-o", bin, "../cmd/warden")
	cmd.Dir = ".."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build failed: %s\n%s", err, out)
	}
	return bin
}

func TestRunEchoCommand(t *testing.T) {
	bin := wardenBin(t)
	cmd := exec.Command(bin, "run", "--no-network", "--", "echo", "hello from warden")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("warden run failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "hello from warden") {
		t.Errorf("output = %q, want 'hello from warden'", string(out))
	}
}

func TestRunMountReadOnly(t *testing.T) {
	bin := wardenBin(t)
	tmp := t.TempDir()
	testFile := filepath.Join(tmp, "test.txt")
	os.WriteFile(testFile, []byte("readable"), 0o644)

	// Read should work
	cmd := exec.Command(bin, "run", "--mount", tmp+":ro", "--no-network", "--", "cat", testFile)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("read failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "readable") {
		t.Errorf("should be able to read file, got: %s", out)
	}

	// Write should fail
	cmd = exec.Command(bin, "run", "--mount", tmp+":ro", "--no-network", "--", "sh", "-c", "echo nope > "+testFile)
	err = cmd.Run()
	if err == nil {
		t.Error("writing to read-only mount should fail")
	}
}

func TestRunNoNetworkBlocks(t *testing.T) {
	bin := wardenBin(t)
	cmd := exec.Command(bin, "run", "--no-network", "--timeout", "10s", "--", "sh", "-c", "curl -s --max-time 5 https://example.com || exit 1")
	err := cmd.Run()
	if err == nil {
		t.Error("network request should fail with --no-network")
	}
}

func TestRunExitCodePropagation(t *testing.T) {
	bin := wardenBin(t)
	cmd := exec.Command(bin, "run", "--no-network", "--", "sh", "-c", "exit 42")
	err := cmd.Run()
	if exitErr, ok := err.(*exec.ExitError); ok {
		if exitErr.ExitCode() != 42 {
			t.Errorf("exit code = %d, want 42", exitErr.ExitCode())
		}
	} else if err == nil {
		t.Error("expected non-zero exit")
	}
}

func TestRunDryRun(t *testing.T) {
	bin := wardenBin(t)
	cmd := exec.Command(bin, "run", "--mount", "/tmp:ro", "--no-network", "--dry-run", "--", "echo", "hello")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("dry-run failed: %v\n%s", err, out)
	}
	output := string(out)
	if !strings.Contains(output, "docker") {
		t.Errorf("dry-run should print docker command, got: %s", output)
	}
	if !strings.Contains(output, "--network none") {
		t.Errorf("dry-run should show --network none, got: %s", output)
	}
}

func TestRunTimeout(t *testing.T) {
	bin := wardenBin(t)
	cmd := exec.Command(bin, "run", "--no-network", "--timeout", "5s", "--", "sleep", "60")
	err := cmd.Run()
	if exitErr, ok := err.(*exec.ExitError); ok {
		if exitErr.ExitCode() != 124 {
			t.Errorf("exit code = %d, want 124 (timeout)", exitErr.ExitCode())
		}
	} else if err == nil {
		t.Error("expected timeout exit")
	}
}
```

**Step 2: Run integration tests**

Run: `go test -tags integration ./tests/ -v -timeout 120s`
Expected: PASS (may be slow on first run due to image pull)

**Step 3: Commit**

```bash
git add tests/
git commit -m "test: integration tests for run, mounts, network, timeout, exit codes"
```

---

## Summary

| Task | Description | Dependencies |
|---|---|---|
| 1 | Project scaffold + CLI skeleton | none |
| 2 | Config types, defaults, YAML parsing | 1 |
| 3 | Config merging (profile inheritance) | 2 |
| 4 | Mount resolution + Docker arg builder | 2 |
| 5 | Docker check + image builder + feature scripts | 4 |
| 6 | Container execution (TTY, signals, timeout, exit codes) | 4, 5 |
| 7 | CLI wiring (connect all pieces) | 3, 6 |
| 8 | `warden init` + `warden images` commands | 7 |
| 9 | Integration tests | 8 |
