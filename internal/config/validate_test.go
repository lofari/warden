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
