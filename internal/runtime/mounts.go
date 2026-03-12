package runtime

import (
	"fmt"
	"os"
	"path/filepath"

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
