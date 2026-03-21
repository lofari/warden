package runtime

import (
	"fmt"
	"sort"
	"time"

	"github.com/winler/warden/internal/config"
)

// ImageInfo describes a cached image or rootfs.
type ImageInfo struct {
	Tag       string
	Size      int64
	Runtime   string
	CreatedAt time.Time
}

// Runtime abstracts the execution backend (Docker, Firecracker, etc.).
type Runtime interface {
	// Preflight checks if the runtime is available and ready.
	Preflight() error

	// EnsureImage ensures the image/rootfs exists, building if needed.
	// Returns an image identifier (Docker tag or rootfs path).
	EnsureImage(cfg config.SandboxConfig) (string, error)

	// Run executes a command in the sandbox.
	// Returns exit code and error. Error is non-nil for infrastructure failures.
	// Exit code is meaningful only when error is nil.
	Run(cfg config.SandboxConfig, command []string) (int, error)

	// DryRun prints what would be executed without running it.
	DryRun(cfg config.SandboxConfig, command []string) error

	// ListImages returns cached images for this runtime.
	ListImages() ([]ImageInfo, error)

	// PruneImages removes all cached images for this runtime.
	PruneImages() error
}

var registry = map[string]func() Runtime{}

// Register adds a runtime factory to the registry.
func Register(name string, factory func() Runtime) {
	registry[name] = factory
}

// NewRuntime creates a Runtime for the given name.
func NewRuntime(name string) (Runtime, error) {
	factory, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown runtime: %q", name)
	}
	return factory(), nil
}

// AllRuntimes returns all registered runtime names.
func AllRuntimes() []string {
	var names []string
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
