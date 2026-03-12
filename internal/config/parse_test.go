package config

import (
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Image != "ubuntu:24.04" {
		t.Errorf("default image = %q, want ubuntu:24.04", cfg.Image)
	}
	if cfg.Network != false {
		t.Error("default network should be false")
	}
	if cfg.Memory != "8g" {
		t.Errorf("default memory = %q, want 8g", cfg.Memory)
	}
}

func TestDefaultConfigRuntime(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Runtime != "docker" {
		t.Errorf("default runtime = %q, want docker", cfg.Runtime)
	}
}

func TestParseWardenYAML(t *testing.T) {
	yaml := `
default:
  image: ubuntu:24.04
  tools: [node, python]
  mounts:
    - path: "."
      mode: rw
  network: false
  timeout: 1h
  memory: 4g
  cpus: 2

profiles:
  web:
    extends: default
    network: true
    tools: [node, python, go]
`
	file, err := ParseWardenYAML([]byte(yaml))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if file.Default.Image == nil || *file.Default.Image != "ubuntu:24.04" {
		t.Errorf("default image = %v, want ubuntu:24.04", file.Default.Image)
	}
	if len(file.Default.Tools) != 2 {
		t.Errorf("default tools count = %d, want 2", len(file.Default.Tools))
	}
	web, ok := file.Profiles["web"]
	if !ok {
		t.Fatal("missing 'web' profile")
	}
	if web.Extends != "default" {
		t.Errorf("web extends = %q, want default", web.Extends)
	}
	if web.Network == nil || *web.Network != true {
		t.Error("web network should be true")
	}
}

func TestParseWardenYAMLWithRuntime(t *testing.T) {
	yaml := `
default:
  runtime: docker
  image: ubuntu:24.04

profiles:
  secure:
    extends: default
    runtime: firecracker
`
	file, err := ParseWardenYAML([]byte(yaml))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if file.Default.Runtime == nil || *file.Default.Runtime != "docker" {
		t.Errorf("default runtime = %v, want docker", file.Default.Runtime)
	}
	secure := file.Profiles["secure"]
	if secure.Runtime == nil || *secure.Runtime != "firecracker" {
		t.Errorf("secure runtime = %v, want firecracker", secure.Runtime)
	}
}
