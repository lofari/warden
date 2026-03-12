package firecracker

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/winler/warden/internal/config"
	"github.com/winler/warden/internal/runtime"
)

// FirecrackerRuntime implements runtime.Runtime using Firecracker microVMs.
type FirecrackerRuntime struct{}

func init() {
	runtime.Register("firecracker", func() runtime.Runtime {
		return &FirecrackerRuntime{}
	})
}

// Preflight verifies /dev/kvm, firecracker binary, and virtiofsd are available.
func (f *FirecrackerRuntime) Preflight() error {
	if _, err := os.Stat("/dev/kvm"); err != nil {
		return fmt.Errorf("warden: /dev/kvm not accessible. Run 'warden setup firecracker'")
	}
	file, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("warden: /dev/kvm not accessible. Run 'warden setup firecracker'")
	}
	file.Close()

	homeDir, _ := os.UserHomeDir()
	fcPath := filepath.Join(homeDir, ".warden", "firecracker", "bin", "firecracker")
	if _, err := os.Stat(fcPath); err != nil {
		return fmt.Errorf("warden: firecracker not found. Run 'warden setup firecracker'")
	}
	vfsPath := filepath.Join(homeDir, ".warden", "firecracker", "bin", "virtiofsd")
	if _, err := os.Stat(vfsPath); err != nil {
		return fmt.Errorf("warden: virtiofsd not found. Run 'warden setup firecracker'")
	}
	return nil
}

// EnsureImage — placeholder, implemented in a later chunk.
func (f *FirecrackerRuntime) EnsureImage(cfg config.SandboxConfig) (string, error) {
	return "", fmt.Errorf("firecracker EnsureImage not yet implemented")
}

// Run — placeholder, implemented in Chunk 7.
func (f *FirecrackerRuntime) Run(cfg config.SandboxConfig, command []string) (int, error) {
	return 1, fmt.Errorf("firecracker runtime not yet implemented")
}

// DryRun — placeholder, implemented in Chunk 7.
func (f *FirecrackerRuntime) DryRun(cfg config.SandboxConfig, command []string) error {
	return fmt.Errorf("firecracker dry-run not yet implemented")
}

// ListImages — placeholder, implemented in a later chunk.
func (f *FirecrackerRuntime) ListImages() ([]runtime.ImageInfo, error) {
	return nil, fmt.Errorf("firecracker ListImages not yet implemented")
}

// PruneImages — placeholder, implemented in a later chunk.
func (f *FirecrackerRuntime) PruneImages() error {
	return fmt.Errorf("firecracker PruneImages not yet implemented")
}
