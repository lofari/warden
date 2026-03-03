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
