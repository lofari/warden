package config

import "fmt"

// ApplyProfile overlays a ProfileEntry onto a SandboxConfig.
// Only non-nil fields in the profile override the base.
func ApplyProfile(base SandboxConfig, p ProfileEntry) SandboxConfig {
	if p.Runtime != nil {
		base.Runtime = *p.Runtime
	}
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
	if p.Proxy != nil {
		base.Proxy = p.Proxy
	}
	return base
}

// ResolveProfile resolves a named profile from a WardenFile into a SandboxConfig.
// Empty name resolves the default profile. Handles `extends` chains recursively
// with cycle detection.
func ResolveProfile(file *WardenFile, name string) (SandboxConfig, error) {
	cfg := DefaultConfig()
	cfg = ApplyProfile(cfg, file.Default)

	if name == "" || name == "default" {
		return cfg, nil
	}

	chain, err := resolveExtendsChain(file, name)
	if err != nil {
		return SandboxConfig{}, err
	}

	for _, p := range chain {
		cfg = ApplyProfile(cfg, p)
	}
	return cfg, nil
}

func resolveExtendsChain(file *WardenFile, name string) ([]ProfileEntry, error) {
	var chain []ProfileEntry
	seen := map[string]bool{}
	current := name

	for current != "" && current != "default" {
		if seen[current] {
			return nil, fmt.Errorf("circular extends: profile %q", current)
		}
		seen[current] = true
		profile, ok := file.Profiles[current]
		if !ok {
			return nil, fmt.Errorf("unknown profile: %q", current)
		}
		chain = append(chain, profile)
		current = profile.Extends
	}

	// Reverse so root ancestor is applied first
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain, nil
}
