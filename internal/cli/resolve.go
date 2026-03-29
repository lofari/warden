package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/winler/warden/internal/config"
	"github.com/winler/warden/internal/runtime"
)

// resolveOptions holds CLI flag overrides for config resolution.
type resolveOptions struct {
	image      string
	memory     string
	cpus       int
	timeout    string
	workdir    string
	tools      string
	network    *bool // nil = not set, true/false = override
	display    bool
	resolution string
	proxy      []string
	authBroker bool
	ephemeral  bool
	mounts     []string
	env        []string
	profile    string
	runtime    string
}

// resolveConfig loads .warden.yaml, applies overrides, resolves mounts, validates,
// and returns a ready-to-use SandboxConfig.
func resolveConfig(opts resolveOptions) (config.SandboxConfig, error) {
	cfg := config.DefaultConfig()
	wardenPath := findWardenYAML()
	baseDir, err := os.Getwd()
	if err != nil {
		return cfg, fmt.Errorf("warden: cannot determine working directory: %w", err)
	}

	if wardenPath != "" {
		data, err := os.ReadFile(wardenPath)
		if err != nil {
			return cfg, fmt.Errorf("reading %s: %w", wardenPath, err)
		}
		file, err := config.ParseWardenYAML(data)
		if err != nil {
			return cfg, err
		}
		baseDir = filepath.Dir(wardenPath)
		resolved, err := config.ResolveProfile(file, opts.profile)
		if err != nil {
			return cfg, err
		}
		cfg = resolved
	}

	// Apply overrides
	if opts.image != "" {
		cfg.Image = opts.image
	}
	if opts.memory != "" {
		cfg.Memory = opts.memory
	}
	if opts.cpus > 0 {
		cfg.CPUs = opts.cpus
	}
	if opts.timeout != "" {
		cfg.Timeout = opts.timeout
	}
	if opts.workdir != "" {
		cfg.Workdir = opts.workdir
	}
	if opts.tools != "" {
		cfg.Tools = strings.Split(opts.tools, ",")
	}
	if opts.network != nil {
		cfg.Network = *opts.network
	}
	if opts.display {
		cfg.Display = true
	}
	if opts.resolution != "" {
		cfg.Resolution = opts.resolution
	}
	if len(opts.proxy) > 0 {
		cfg.Proxy = opts.proxy
	}
	if opts.authBroker {
		if cfg.AuthBroker == nil {
			cfg.AuthBroker = &config.AuthBrokerConfig{}
		}
		cfg.AuthBroker.Enabled = true
	}
	if opts.ephemeral {
		cfg.Ephemeral = true
	}
	if len(opts.env) > 0 {
		cfg.Env = opts.env
	}
	if opts.runtime != "" {
		cfg.Runtime = opts.runtime
	}

	// Mount overrides
	if len(opts.mounts) > 0 {
		cfg.Mounts = nil
		for _, m := range opts.mounts {
			parts := strings.SplitN(m, ":", 2)
			mode := "ro"
			if len(parts) == 2 {
				mode = parts[1]
			}
			cfg.Mounts = append(cfg.Mounts, config.Mount{Path: parts[0], Mode: mode})
		}
	}

	// Default: mount cwd as rw if no mounts
	if len(cfg.Mounts) == 0 {
		cwd, err := os.Getwd()
		if err != nil {
			return cfg, fmt.Errorf("warden: cannot determine working directory: %w", err)
		}
		cfg.Mounts = []config.Mount{{Path: cwd, Mode: "rw"}}
	}

	// Resolve mount paths
	resolvedMounts, err := runtime.ResolveMounts(cfg.Mounts, baseDir)
	if err != nil {
		return cfg, err
	}
	cfg.Mounts = resolvedMounts

	if err := cfg.Validate(); err != nil {
		return cfg, err
	}

	// Default workdir to first rw mount
	if cfg.Workdir == "" {
		for _, m := range cfg.Mounts {
			if m.Mode == "rw" {
				cfg.Workdir = m.Path
				break
			}
		}
	}

	return cfg, nil
}

// resolveAndRun is the common flow for shell, exec, and run:
// resolve config, select runtime, preflight, ensure image, run command.
func resolveAndRun(opts resolveOptions, command []string) error {
	cfg, err := resolveConfig(opts)
	if err != nil {
		return err
	}

	rt, resolvedName, err := runtime.ResolveRuntime(cfg.Runtime)
	if err != nil {
		return err
	}

	if cfg.Display && resolvedName != "firecracker" {
		return fmt.Errorf("--display is only supported with firecracker runtime")
	}

	if err := rt.Preflight(); err != nil {
		return err
	}

	if _, err := rt.EnsureImage(cfg); err != nil {
		return err
	}

	exitCode, err := rt.Run(cfg, command)
	if err != nil {
		return err
	}
	if exitCode != 0 {
		os.Exit(exitCode)
	}
	return nil
}
