package container

import (
	"os"
	"strings"
	"testing"

	"github.com/winler/warden/internal/config"
)

func TestContainerName(t *testing.T) {
	name := ContainerName()
	if !strings.HasPrefix(name, "warden-") {
		t.Errorf("container name %q should start with warden-", name)
	}
}

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

func TestJoinArgsQuotesSpaces(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"simple", []string{"echo", "hello"}, "echo hello"},
		{"spaces", []string{"echo", "hello world"}, "echo 'hello world'"},
		{"single_quote", []string{"echo", "it's"}, "echo 'it'\\''s'"},
		{"double_quote", []string{"echo", `say "hi"`}, `echo 'say "hi"'`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := joinArgs(tt.args)
			if got != tt.want {
				t.Errorf("joinArgs(%v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}
