package container

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"

	"github.com/winler/warden/internal/config"
)

// ResolvedMount is a mount with an absolute host path.
type ResolvedMount struct {
	Path string
	Mode string
}

// ResolveMounts converts relative mount paths to absolute and validates they exist.
func ResolveMounts(mounts []config.Mount, baseDir string) ([]ResolvedMount, error) {
	resolved := make([]ResolvedMount, 0, len(mounts))
	for _, m := range mounts {
		p := m.Path
		if !filepath.IsAbs(p) {
			p = filepath.Join(baseDir, p)
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			return nil, fmt.Errorf("resolving mount path %q: %w", m.Path, err)
		}
		if _, err := os.Stat(abs); err != nil {
			return nil, fmt.Errorf("warden: mount path %s does not exist", m.Path)
		}
		mode := m.Mode
		if mode == "" {
			mode = "ro"
		}
		resolved = append(resolved, ResolvedMount{Path: abs, Mode: mode})
	}
	return resolved, nil
}

// BuildDockerArgs translates a SandboxConfig into docker run arguments.
// The command to run is appended after the image name.
func BuildDockerArgs(cfg config.SandboxConfig, command []string) []string {
	args := []string{"run", "--rm"}

	// User mapping
	u, err := user.Current()
	if err == nil {
		args = append(args, "--user", u.Uid+":"+u.Gid)
	}

	// Network
	if !cfg.Network {
		args = append(args, "--network", "none")
	}

	// Resources
	if cfg.Memory != "" {
		args = append(args, "--memory", cfg.Memory)
	}
	if cfg.CPUs > 0 {
		args = append(args, "--cpus", strconv.Itoa(cfg.CPUs))
	}

	// Mounts
	for _, m := range cfg.Mounts {
		args = append(args, "-v", m.Path+":"+m.Path+":"+m.Mode)
	}

	// Workdir
	if cfg.Workdir != "" {
		args = append(args, "-w", cfg.Workdir)
	}

	// Image
	args = append(args, cfg.Image)

	// Command
	args = append(args, command...)

	return args
}
