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
