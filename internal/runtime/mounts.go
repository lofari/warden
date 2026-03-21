package runtime

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/winler/warden/internal/config"
)

// ResolveMounts converts relative mount paths to absolute and validates they exist.
// All fields from the original config.Mount are preserved.
func ResolveMounts(mounts []config.Mount, baseDir string) ([]config.Mount, error) {
	resolved := make([]config.Mount, 0, len(mounts))
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
		r := m
		r.Path = abs
		r.Mode = mode
		resolved = append(resolved, r)
	}
	return resolved, nil
}
