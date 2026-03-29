package cli

import (
	"os"
	"testing"
)

func TestResolveConfigDefaultMountsCwd(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := resolveConfig(resolveOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Mounts) == 0 {
		t.Fatal("expected default mount")
	}
	if cfg.Mounts[0].Path != cwd {
		t.Errorf("expected mount path %s, got %s", cwd, cfg.Mounts[0].Path)
	}
	if cfg.Mounts[0].Mode != "rw" {
		t.Errorf("expected mount mode rw, got %s", cfg.Mounts[0].Mode)
	}
}

func TestResolveConfigSetsWorkdir(t *testing.T) {
	cfg, err := resolveConfig(resolveOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Workdir == "" {
		t.Error("expected workdir to be set from default mount")
	}
}
