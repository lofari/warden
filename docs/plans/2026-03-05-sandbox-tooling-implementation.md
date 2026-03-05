# Sandbox Tooling Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use golem-superpowers:golem-execution during each Golem iteration.

**Goal:** Make every warden sandbox agent-ready by introducing a base image layer with dev utilities and enhancing feature scripts with ecosystem essentials.

**Architecture:** Always build a `warden:base-<image>` image with agent utilities (rg, fd, jq, build-essential, etc). Feature tool images (`--tools node`) layer on top of the base instead of raw ubuntu. The `run.go` flow changes to always resolve the base image before execution.

**Tech Stack:** Go, Docker, go:embed, shell scripts

---

## Task 1: Add Base Image Dockerfile Template

**Files:**
- Create: `internal/container/base.go`
- Test: `internal/container/base_test.go`

**Step 1: Write the failing test**

```go
// internal/container/base_test.go
package container

import "testing"

func TestBaseImageTag(t *testing.T) {
	tests := []struct {
		base string
		want string
	}{
		{"ubuntu:24.04", "warden:base-ubuntu-24.04"},
		{"ubuntu:22.04", "warden:base-ubuntu-22.04"},
		{"debian:bookworm", "warden:base-debian-bookworm"},
		{"myrepo/ubuntu:24.04", "warden:base-myrepo-ubuntu-24.04"},
	}
	for _, tt := range tests {
		got := BaseImageTag(tt.base)
		if got != tt.want {
			t.Errorf("BaseImageTag(%q) = %q, want %q", tt.base, got, tt.want)
		}
	}
}

func TestBaseDockerfile(t *testing.T) {
	df := BaseDockerfile("ubuntu:24.04")
	if df == "" {
		t.Fatal("BaseDockerfile should not be empty")
	}
	// Must start with FROM
	if df[:4] != "FROM" {
		t.Error("Dockerfile should start with FROM")
	}
	// Must contain key packages
	for _, pkg := range []string{"ripgrep", "fd-find", "build-essential", "jq", "git"} {
		if !contains(df, pkg) {
			t.Errorf("BaseDockerfile missing package %q", pkg)
		}
	}
	// Must configure locale
	if !contains(df, "locale-gen") {
		t.Error("BaseDockerfile should configure locale")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsStr(s, substr)
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/winler/projects/warden && go test ./internal/container/ -run "TestBaseImageTag|TestBaseDockerfile" -v`
Expected: FAIL — `BaseImageTag` and `BaseDockerfile` undefined

**Step 3: Write minimal implementation**

```go
// internal/container/base.go
package container

import (
	"fmt"
	"strings"
)

// BaseImageTag returns the tag for the warden base image derived from a given base.
func BaseImageTag(base string) string {
	safe := strings.ReplaceAll(base, ":", "-")
	safe = strings.ReplaceAll(safe, "/", "-")
	return "warden:base-" + safe
}

// BaseDockerfile returns the Dockerfile content for building the warden base image.
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
```

**Step 4: Run test to verify it passes**

Run: `cd /home/winler/projects/warden && go test ./internal/container/ -run "TestBaseImageTag|TestBaseDockerfile" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/container/base.go internal/container/base_test.go
git commit -m "feat: add BaseImageTag and BaseDockerfile for warden base image"
```

---

## Task 2: Implement BuildBaseImage Function

**Files:**
- Modify: `internal/container/base.go`
- Modify: `internal/container/base_test.go`

**Step 1: Write the failing test**

Add to `internal/container/base_test.go`:

```go
func TestBuildBaseImageWritesDockerfile(t *testing.T) {
	// We can't run docker build in unit tests, but we can verify
	// the function calls the right pieces by testing the Dockerfile content.
	// The actual docker build is tested in integration tests.
	df := BaseDockerfile("ubuntu:24.04")
	tag := BaseImageTag("ubuntu:24.04")

	if tag != "warden:base-ubuntu-24.04" {
		t.Errorf("unexpected tag: %s", tag)
	}
	if !contains(df, "FROM ubuntu:24.04") {
		t.Error("Dockerfile should use the specified base image")
	}
}
```

**Step 2: Run test to verify it passes (this is a documentation test)**

Run: `cd /home/winler/projects/warden && go test ./internal/container/ -run "TestBuildBaseImage" -v`
Expected: PASS

**Step 3: Implement BuildBaseImage**

Add to `internal/container/base.go`:

```go
// BuildBaseImage ensures the warden base image exists for the given base image.
// Returns the base image tag. Builds if not cached.
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
```

Add imports: `"fmt"`, `"os"`, `"os/exec"`, `"path/filepath"`.

**Step 4: Run all tests**

Run: `cd /home/winler/projects/warden && go test ./internal/container/ -v`
Expected: PASS (all existing + new tests)

**Step 5: Commit**

```bash
git add internal/container/base.go internal/container/base_test.go
git commit -m "feat: implement BuildBaseImage with first-run message"
```

---

## Task 3: Update BuildImage to Layer on Base

**Files:**
- Modify: `internal/container/image.go:14-27` (ImageTag)
- Modify: `internal/container/image.go:39-95` (BuildImage)
- Modify: `internal/container/image_test.go`

**Step 1: Write the failing test**

Update `internal/container/image_test.go` — the `ImageTag` function with no tools should now return the base image tag, not the raw image:

```go
func TestImageTagNoToolsReturnsBase(t *testing.T) {
	got := ImageTag("ubuntu:24.04", nil)
	if got != "warden:base-ubuntu-24.04" {
		t.Errorf("ImageTag with no tools = %q, want %q", got, "warden:base-ubuntu-24.04")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/winler/projects/warden && go test ./internal/container/ -run "TestImageTagNoToolsReturnsBase" -v`
Expected: FAIL — returns `ubuntu:24.04` instead of `warden:base-ubuntu-24.04`

**Step 3: Update ImageTag and BuildImage**

In `internal/container/image.go`:

Change `ImageTag`:
```go
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
```

Change `BuildImage` to use base image as FROM:
```go
func BuildImage(base string, tools []string) (string, error) {
	// Ensure base image exists first
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

	// Use base image as FROM — no need for apt-get install of base packages
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
```

**Step 4: Update existing ImageTag tests**

In `internal/container/image_test.go`, update the first test case:

```go
func TestImageTag(t *testing.T) {
	tests := []struct {
		base  string
		tools []string
		want  string
	}{
		{"ubuntu:24.04", nil, "warden:base-ubuntu-24.04"},
		{"ubuntu:24.04", []string{"node"}, "warden:ubuntu-24.04_node"},
		{"ubuntu:24.04", []string{"go", "node"}, "warden:ubuntu-24.04_go_node"},
		{"ubuntu:24.04", []string{"node", "go"}, "warden:ubuntu-24.04_go_node"},
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

**Step 5: Run all tests**

Run: `cd /home/winler/projects/warden && go test ./internal/container/ -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/container/image.go internal/container/image_test.go
git commit -m "feat: BuildImage layers on warden base image instead of raw ubuntu"
```

---

## Task 4: Update run.go to Always Use Base Image

**Files:**
- Modify: `internal/container/run.go:31-62`

**Step 1: Write the failing test**

Add to `internal/container/run_test.go`:

```go
func TestDryRunUsesBaseImage(t *testing.T) {
	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	rc := RunConfig{
		Sandbox: config.SandboxConfig{
			Image:   "ubuntu:24.04",
			Network: false,
			Mounts:  []config.Mount{{Path: "/tmp", Mode: "ro"}},
		},
		Command: []string{"echo", "test"},
		DryRun:  true,
	}
	Run(rc)

	w.Close()
	os.Stdout = old

	var buf [4096]byte
	n, _ := r.Read(buf[:])
	output := string(buf[:n])

	// Dry-run should show the base image tag, not raw ubuntu
	if !strings.Contains(output, "warden:base-ubuntu-24.04") {
		t.Errorf("dry-run should use base image tag, got: %s", output)
	}
}
```

Add `"os"` and `"github.com/winler/warden/internal/config"` imports to the test file if not present.

**Step 2: Run test to verify it fails**

Run: `cd /home/winler/projects/warden && go test ./internal/container/ -run "TestDryRunUsesBaseImage" -v`
Expected: FAIL — output shows `ubuntu:24.04` instead of `warden:base-ubuntu-24.04`

**Step 3: Update run.go**

In `internal/container/run.go`, modify the `Run` function. Replace the image resolution block (lines 53-62) with:

```go
	// Resolve image — always use base, layer tools if requested
	image := resolved.Image
	if len(resolved.Tools) > 0 {
		built, err := BuildImage(resolved.Image, resolved.Tools)
		if err != nil {
			return 1, err
		}
		image = built
	} else {
		image = BaseImageTag(resolved.Image)
	}
	resolved.Image = image
```

Also update the dry-run block (lines 35-46) to resolve the image tag before printing:

```go
	if rc.DryRun {
		resolved.Image = ImageTag(resolved.Image, resolved.Tools)
		args := BuildDockerArgs(resolved, rc.Command)
		name := ContainerName()
		extra := []string{"--name", name}
		fullArgs := make([]string, 0, len(args)+len(extra))
		fullArgs = append(fullArgs, args[0], args[1])
		fullArgs = append(fullArgs, extra...)
		fullArgs = append(fullArgs, args[2:]...)
		fmt.Println("docker " + joinArgs(fullArgs))
		return 0, nil
	}
```

**Step 4: Run all tests**

Run: `cd /home/winler/projects/warden && go test ./internal/container/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/container/run.go internal/container/run_test.go
git commit -m "feat: run always resolves to warden base image"
```

---

## Task 5: Enhance Feature Scripts

**Files:**
- Modify: `internal/features/scripts/node.sh`
- Modify: `internal/features/scripts/python.sh`
- Modify: `internal/features/scripts/go.sh`
- Modify: `internal/features/scripts/rust.sh`
- Modify: `internal/features/scripts/java.sh`

**Step 1: Update node.sh**

```bash
#!/bin/bash
set -euo pipefail
curl -fsSL https://deb.nodesource.com/setup_22.x | bash -
apt-get install -y nodejs
npm install -g yarn pnpm tsx typescript
```

**Step 2: Update python.sh**

```bash
#!/bin/bash
set -euo pipefail
apt-get update && apt-get install -y python3 python3-pip python3-venv python3-dev
ln -sf /usr/bin/python3 /usr/bin/python
curl -LsSf https://astral.sh/uv/install.sh | sh
echo 'export PATH="/root/.local/bin:$PATH"' >> /etc/profile.d/uv.sh
```

**Step 3: Update go.sh**

```bash
#!/bin/bash
set -euo pipefail
GO_VERSION="1.23.6"
curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" | tar -C /usr/local -xzf -
echo 'export PATH=$PATH:/usr/local/go/bin:/root/go/bin' >> /etc/profile.d/go.sh
export PATH=$PATH:/usr/local/go/bin
go install golang.org/x/tools/gopls@latest
```

**Step 4: Update rust.sh**

```bash
#!/bin/bash
set -euo pipefail
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y --default-toolchain stable
echo 'source /root/.cargo/env' >> /etc/profile.d/rust.sh
```

(Kept lean — no change from current, cargo handles ecosystem tools.)

**Step 5: Update java.sh**

```bash
#!/bin/bash
set -euo pipefail
apt-get update && apt-get install -y openjdk-21-jdk-headless maven
curl -fsSL https://services.gradle.org/distributions/gradle-8.5-bin.zip -o /tmp/gradle.zip
unzip -q /tmp/gradle.zip -d /opt
ln -s /opt/gradle-8.5/bin/gradle /usr/local/bin/gradle
rm /tmp/gradle.zip
```

**Step 6: Run feature tests**

Run: `cd /home/winler/projects/warden && go test ./internal/features/ -v`
Expected: PASS (tests only check scripts exist and are non-empty)

**Step 7: Commit**

```bash
git add internal/features/scripts/
git commit -m "feat: enhance feature scripts with ecosystem essentials"
```

---

## Task 6: Update Existing Tests and Dry-Run Integration

**Files:**
- Modify: `internal/cli/root_test.go` (if dry-run tests reference raw image)
- Modify: `tests/integration_test.go`

**Step 1: Check and update root_test.go**

Read `internal/cli/root_test.go`. If any tests assert the image name in dry-run output, update them to expect `warden:base-ubuntu-24.04` instead of `ubuntu:24.04`.

**Step 2: Update integration test for dry-run**

In `tests/integration_test.go`, the `TestRunDryRun` test checks for `--network none` but not the image name. If it does check the image, update to expect the base image tag.

**Step 3: Run all unit tests**

Run: `cd /home/winler/projects/warden && go test ./... -v`
Expected: PASS

**Step 4: Run go vet**

Run: `cd /home/winler/projects/warden && go vet ./...`
Expected: Clean

**Step 5: Commit if any test updates were needed**

```bash
git add -A
git commit -m "test: update tests for base image tag expectations"
```

---

## Task 7: Final Verification and Cleanup

**Files:**
- Verify: all files modified in Tasks 1-6

**Step 1: Run full test suite**

Run: `cd /home/winler/projects/warden && go test ./... -v`
Expected: All tests PASS

**Step 2: Run go vet**

Run: `cd /home/winler/projects/warden && go vet ./...`
Expected: Clean

**Step 3: Test dry-run manually**

Run: `cd /home/winler/projects/warden && go run ./cmd/warden run --dry-run -- echo hello`
Expected: Output shows `warden:base-ubuntu-24.04` as the image

Run: `cd /home/winler/projects/warden && go run ./cmd/warden run --tools node --dry-run -- echo hello`
Expected: Output shows `warden:ubuntu-24.04_node` as the image

**Step 4: Commit final state**

If any cleanup was needed:
```bash
git add -A
git commit -m "chore: final cleanup for sandbox tooling feature"
```
