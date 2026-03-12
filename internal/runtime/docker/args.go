package docker

import (
	"os/user"
	"strconv"

	"github.com/winler/warden/internal/config"
)

// buildArgs translates a SandboxConfig into docker run arguments.
func buildArgs(cfg config.SandboxConfig, command []string) []string {
	args := []string{"run", "--rm"}

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

	if cfg.Workdir != "" {
		args = append(args, "-w", cfg.Workdir)
	}

	args = append(args, cfg.Image)
	args = append(args, command...)
	return args
}
