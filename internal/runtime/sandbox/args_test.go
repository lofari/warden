package sandbox

import (
	"strings"
	"testing"

	"github.com/winler/warden/internal/config"
)

func TestBuildExecArgs(t *testing.T) {
	cfg := config.SandboxConfig{
		Mounts: []config.Mount{
			{Path: "/home/user/project", Mode: "rw"},
		},
		Workdir: "/home/user/project",
		Env:     []string{"FOO=bar", "HOME=/home/user"},
	}
	args := buildExecArgs(cfg, "warden-abc123", []string{"bash"}, true)
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "sandbox exec") {
		t.Error("missing 'sandbox exec'")
	}
	if !strings.Contains(joined, "-it") {
		t.Error("missing -it for TTY mode")
	}
	if !strings.Contains(joined, "-e FOO=bar") {
		t.Error("missing -e FOO=bar")
	}
	if !strings.Contains(joined, "-e HOME=/home/user") {
		t.Error("missing -e HOME=/home/user")
	}
	if !strings.Contains(joined, "-w /home/user/project") {
		t.Error("missing -w workdir")
	}
	if !strings.Contains(joined, "warden-abc123") {
		t.Error("missing sandbox name")
	}
	if !strings.HasSuffix(joined, "warden-abc123 bash") {
		t.Errorf("args should end with sandbox name + command, got: %s", joined)
	}
}

func TestBuildExecArgsNoTTY(t *testing.T) {
	cfg := config.SandboxConfig{
		Workdir: "/home/user/project",
	}
	args := buildExecArgs(cfg, "warden-abc123", []string{"echo", "hello"}, false)
	joined := strings.Join(args, " ")

	if strings.Contains(joined, "-it") {
		t.Error("should not have -it when TTY is false")
	}
	if !strings.Contains(joined, "-i") {
		t.Error("should have -i even without TTY")
	}
}

func TestBuildExecArgsAuthBroker(t *testing.T) {
	cfg := config.SandboxConfig{
		Workdir: "/home/user/project",
		AuthBroker: &config.AuthBrokerConfig{
			Enabled: true,
		},
	}
	args := buildExecArgs(cfg, "warden-abc123", []string{"claude"}, true)
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "-e ANTHROPIC_BASE_URL=http://localhost:19280") {
		t.Error("missing ANTHROPIC_BASE_URL for auth broker")
	}
}

func TestBuildExecArgsNoAuthBroker(t *testing.T) {
	cfg := config.SandboxConfig{
		Workdir: "/home/user/project",
	}
	args := buildExecArgs(cfg, "warden-abc123", []string{"bash"}, true)
	joined := strings.Join(args, " ")

	if strings.Contains(joined, "ANTHROPIC_BASE_URL") {
		t.Error("should not set ANTHROPIC_BASE_URL when auth broker is not enabled")
	}
	if strings.Contains(joined, "ANTHROPIC_API_KEY") {
		t.Error("should not set ANTHROPIC_API_KEY when auth broker is not enabled")
	}
}
