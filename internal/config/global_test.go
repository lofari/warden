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
