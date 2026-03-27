package docker

import (
	"os"
	"os/user"
	"path/filepath"
	"strconv"

	"github.com/winler/warden/internal/config"
)

// buildArgs translates a SandboxConfig into docker run arguments.
func buildArgs(cfg config.SandboxConfig, command []string, proxyDir string) []string {
	args := []string{"run", "--rm"}

	// Security hardening
	args = append(args,
		"--security-opt", "no-new-privileges",
		"--cap-drop", "ALL",
		"--pids-limit", "4096",
	)

	u, err := user.Current()
	if err == nil {
		args = append(args, "--user", u.Uid+":"+u.Gid)
	}

	if !cfg.Network {
		args = append(args, "--network", "none")
	}

	if cfg.Memory != "" {
		args = append(args, "--memory", cfg.Memory)
	}
	if cfg.CPUs > 0 {
		args = append(args, "--cpus", strconv.Itoa(cfg.CPUs))
	}

	for _, m := range cfg.Mounts {
		args = append(args, "-v", m.Path+":"+m.Path+":"+m.Mode)
	}

	for _, e := range cfg.Env {
		args = append(args, "-e", e)
	}

	// Proxy mounts: socket directory + shim binary per proxied command
	if proxyDir != "" {
		args = append(args, "-v", proxyDir+":/run/warden-proxy:ro")
		homeDir, _ := os.UserHomeDir()
		shimPath := filepath.Join(homeDir, ".warden", "bin", "warden-shim")
		for _, cmd := range cfg.Proxy {
			args = append(args, "-v", shimPath+":/usr/local/bin/"+cmd+":ro")
		}
	}

	if cfg.Workdir != "" {
		args = append(args, "-w", cfg.Workdir)
	}

	args = append(args, cfg.Image)
	args = append(args, command...)
	return args
}
