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

func TestApplyProfileRuntime(t *testing.T) {
	base := DefaultConfig()
	profile := ProfileEntry{
		Runtime: strPtr("firecracker"),
	}
	result := ApplyProfile(base, profile)
	if result.Runtime != "firecracker" {
		t.Errorf("runtime = %q, want firecracker", result.Runtime)
	}
}

func TestApplyProfileRuntimeNil(t *testing.T) {
	base := DefaultConfig()
	profile := ProfileEntry{}
	result := ApplyProfile(base, profile)
	if result.Runtime != "" {
		t.Errorf("runtime = %q, want empty (auto-detect)", result.Runtime)
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

func TestResolveProfileMultiLevelExtends(t *testing.T) {
	img1 := "base-image"
	mem1 := "1g"
	img2 := "mid-image"
	timeout := "30m"
	img3 := "leaf-image"

	file := &WardenFile{
		Profiles: map[string]ProfileEntry{
			"base":   {Image: &img1, Memory: &mem1},
			"middle": {Extends: "base", Image: &img2, Timeout: &timeout},
			"leaf":   {Extends: "middle", Image: &img3},
		},
	}
	cfg, err := ResolveProfile(file, "leaf")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Image != "leaf-image" {
		t.Errorf("expected leaf-image, got %s", cfg.Image)
	}
	if cfg.Timeout != "30m" {
		t.Errorf("expected 30m timeout, got %s", cfg.Timeout)
	}
	if cfg.Memory != "1g" {
		t.Errorf("expected 1g memory, got %s", cfg.Memory)
	}
}

func TestResolveProfileCycleDetection(t *testing.T) {
	img := "img"
	file := &WardenFile{
		Profiles: map[string]ProfileEntry{
			"a": {Extends: "b", Image: &img},
			"b": {Extends: "a", Image: &img},
		},
	}
	_, err := ResolveProfile(file, "a")
	if err == nil {
		t.Error("expected error for circular extends")
	}
}
