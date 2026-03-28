package runtime

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

// RunningInstance describes a running warden sandbox.
type RunningInstance struct {
	Name    string    `json:"name"`
	Runtime string    `json:"runtime"`
	Command string    `json:"command"`
	Started time.Time `json:"started"`
	CPU     float64   `json:"cpu"`    // -1 if unavailable
	Memory  int64     `json:"memory"` // -1 if unavailable
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

	// ListRunning returns currently running sandboxes for this runtime.
	// Returns nil, nil if the runtime is not available.
	ListRunning() ([]RunningInstance, error)

	// Stop halts a running sandbox/container/VM by name.
	Stop(name string) error

	// Remove deletes a stopped sandbox/container/VM and reclaims resources.
	Remove(name string) error
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

// ResolveRuntime selects a runtime. If preferred is non-empty, uses it exactly.
// If empty, auto-detects: Firecracker > Docker Sandbox > Docker.
// Returns the runtime, the resolved name, and any error.
func ResolveRuntime(preferred string) (Runtime, string, error) {
	if preferred != "" {
		rt, err := NewRuntime(preferred)
		return rt, preferred, err
	}

	if firecrackerAvailable() {
		rt, err := NewRuntime("firecracker")
		return rt, "firecracker", err
	}

	fmt.Fprintln(os.Stderr, "warden: firecracker unavailable, checking docker sandbox...")

	if sandboxAvailable() {
		rt, err := NewRuntime("sandbox")
		return rt, "sandbox", err
	}

	fmt.Fprintln(os.Stderr, "warden: docker sandbox unavailable, falling back to docker (less isolation)")
	rt, err := NewRuntime("docker")
	return rt, "docker", err
}

func sandboxAvailable() bool {
	dockerPath, err := exec.LookPath("docker")
	if err != nil {
		return false
	}
	cmd := exec.Command(dockerPath, "sandbox", "version")
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil
}

func firecrackerAvailable() bool {
	if _, err := os.Stat("/dev/kvm"); err != nil {
		return false
	}
	f, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	f.Close()

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	fcPath := filepath.Join(homeDir, ".warden", "firecracker", "bin", "firecracker")
	if _, err := os.Stat(fcPath); err != nil {
		return false
	}
	return true
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
