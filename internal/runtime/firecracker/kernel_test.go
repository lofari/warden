package firecracker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultKernelPath(t *testing.T) {
	tmpHome := t.TempDir()
	path := defaultKernelPath(tmpHome)
	want := filepath.Join(tmpHome, ".warden", "firecracker", "kernel", "vmlinux-5.10.217")
	if path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
}

func TestResolveKernelPathCustom(t *testing.T) {
	custom := "/custom/vmlinux"
	path, err := resolveKernelPath(custom, "")
	if err == nil {
		t.Fatalf("expected error for nonexistent custom path, got path=%q", path)
	}
}

func TestResolveKernelPathDefault(t *testing.T) {
	tmpHome := t.TempDir()
	// Create the kernel file so resolution succeeds
	kernelDir := filepath.Join(tmpHome, ".warden", "firecracker", "kernel")
	os.MkdirAll(kernelDir, 0o755)
	kernelPath := filepath.Join(kernelDir, "vmlinux-5.10.217")
	os.WriteFile(kernelPath, []byte("fake-kernel"), 0o644)

	path, err := resolveKernelPath("", tmpHome)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != kernelPath {
		t.Errorf("path = %q, want %q", path, kernelPath)
	}
}
