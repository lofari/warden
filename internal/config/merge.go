package config

import "fmt"

// ApplyProfile overlays a ProfileEntry onto a SandboxConfig.
// Only non-nil fields in the profile override the base.
func ApplyProfile(base SandboxConfig, p ProfileEntry) SandboxConfig {
	if p.Image != nil {
		base.Image = *p.Image
	}
	if p.Tools != nil {
		base.Tools = p.Tools
	}
	if p.Mounts != nil {
		base.Mounts = p.Mounts
	}
	if p.Network != nil {
		base.Network = *p.Network
	}
	if p.Timeout != nil {
		base.Timeout = *p.Timeout
	}
	if p.Memory != nil {
		base.Memory = *p.Memory
	}
	if p.CPUs != nil {
		base.CPUs = *p.CPUs
	}
	if p.Workdir != nil {
		base.Workdir = *p.Workdir
	}
	if p.Env != nil {
		base.Env = p.Env
	}
	return base
}

// ResolveProfile resolves a named profile from a WardenFile into a SandboxConfig.
// Empty name resolves the default profile. Handles `extends` chains.
func ResolveProfile(file *WardenFile, name string) (SandboxConfig, error) {
	cfg := DefaultConfig()

	// Always apply default profile first
	cfg = ApplyProfile(cfg, file.Default)

	if name == "" || name == "default" {
		return cfg, nil
	}

	profile, ok := file.Profiles[name]
	if !ok {
		return SandboxConfig{}, fmt.Errorf("unknown profile: %q", name)
	}

	// If extends is set and not "default" (already applied), resolve the parent
	if profile.Extends != "" && profile.Extends != "default" {
		parent, ok := file.Profiles[profile.Extends]
		if !ok {
			return SandboxConfig{}, fmt.Errorf("profile %q extends unknown profile %q", name, profile.Extends)
		}
		cfg = ApplyProfile(cfg, parent)
	}

	cfg = ApplyProfile(cfg, profile)
	return cfg, nil
}
