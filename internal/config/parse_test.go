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
	if cfg.Runtime != "" {
		t.Errorf("default runtime = %q, want empty (auto-detect)", cfg.Runtime)
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

func TestParseWardenYAMLWithAccessControls(t *testing.T) {
	yaml := `
default:
  mounts:
    - path: .
      mode: rw
      deny_extra:
        - secrets/
        - "*.secret"
      read_only:
        - .git/hooks
        - .github/workflows
`
	file, err := ParseWardenYAML([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Default.Mounts) != 1 {
		t.Fatalf("expected 1 mount, got %d", len(file.Default.Mounts))
	}
	m := file.Default.Mounts[0]
	if len(m.DenyExtra) != 2 {
		t.Fatalf("expected 2 deny_extra, got %d", len(m.DenyExtra))
	}
	if m.DenyExtra[0] != "secrets/" {
		t.Fatalf("expected deny_extra[0] = 'secrets/', got %q", m.DenyExtra[0])
	}
	if len(m.ReadOnly) != 2 {
		t.Fatalf("expected 2 read_only, got %d", len(m.ReadOnly))
	}
}

func TestParseWardenYAMLWithProxy(t *testing.T) {
	yaml := `
default:
  proxy:
    - claude
  tools: [node]

profiles:
  ai-dev:
    extends: default
    proxy: [claude, cursor]
`
	file, err := ParseWardenYAML([]byte(yaml))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(file.Default.Proxy) != 1 || file.Default.Proxy[0] != "claude" {
		t.Errorf("default proxy = %v, want [claude]", file.Default.Proxy)
	}
	aiDev := file.Profiles["ai-dev"]
	if len(aiDev.Proxy) != 2 {
		t.Errorf("ai-dev proxy count = %d, want 2", len(aiDev.Proxy))
	}

	// Test merge: profile proxy overrides default
	cfg, err := ResolveProfile(file, "ai-dev")
	if err != nil {
		t.Fatalf("resolve error: %v", err)
	}
	if len(cfg.Proxy) != 2 || cfg.Proxy[0] != "claude" || cfg.Proxy[1] != "cursor" {
		t.Errorf("resolved proxy = %v, want [claude cursor]", cfg.Proxy)
	}
}

func TestParseWardenYAMLWithAuthBroker(t *testing.T) {
	yaml := `
default:
  auth_broker:
    enabled: true
    credentials: ~/.claude/.credentials.json
    target: api.anthropic.com
  network: true

profiles:
  no-broker:
    extends: default
    auth_broker:
      enabled: false
`
	file, err := ParseWardenYAML([]byte(yaml))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if file.Default.AuthBroker == nil {
		t.Fatal("default auth_broker is nil")
	}
	if !file.Default.AuthBroker.Enabled {
		t.Error("default auth_broker.enabled = false, want true")
	}
	if file.Default.AuthBroker.Credentials != "~/.claude/.credentials.json" {
		t.Errorf("credentials = %q", file.Default.AuthBroker.Credentials)
	}
	if file.Default.AuthBroker.Target != "api.anthropic.com" {
		t.Errorf("target = %q", file.Default.AuthBroker.Target)
	}

	cfg, err := ResolveProfile(file, "no-broker")
	if err != nil {
		t.Fatalf("resolve error: %v", err)
	}
	if cfg.AuthBroker == nil {
		t.Fatal("resolved auth_broker is nil")
	}
	if cfg.AuthBroker.Enabled {
		t.Error("resolved auth_broker.enabled = true, want false after profile override")
	}
}

func TestParseWardenYAMLEphemeral(t *testing.T) {
	data := []byte(`
ephemeral: true
`)
	file, err := ParseWardenYAML(data)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	cfg, err := ResolveProfile(file, "")
	if err != nil {
		t.Fatalf("resolve error: %v", err)
	}
	if !cfg.Ephemeral {
		t.Error("expected ephemeral=true")
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
