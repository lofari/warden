package firecracker

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

const (
	kernelVersion  = "5.10.217"
	kernelFilename = "vmlinux-" + kernelVersion
	// TODO: replace with actual checksum from Firecracker releases
	kernelChecksum = "placeholder-sha256-checksum"
	kernelURL      = "https://github.com/firecracker-microvm/firecracker/releases/download/v1.7.0/" + kernelFilename
)

func defaultKernelPath(homeDir string) string {
	return filepath.Join(homeDir, ".warden", "firecracker", "kernel", kernelFilename)
}

// resolveKernelPath returns the kernel path. If customPath is set, uses that.
// Otherwise uses the default path under homeDir, downloading if needed.
func resolveKernelPath(customPath string, homeDir string) (string, error) {
	if customPath != "" {
		if _, err := os.Stat(customPath); err != nil {
			return "", fmt.Errorf("warden: kernel not found at %s", customPath)
		}
		return customPath, nil
	}

	path := defaultKernelPath(homeDir)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	// Need to download
	fmt.Fprintf(os.Stderr, "warden: downloading kernel %s (first run only)...\n", kernelVersion)
	if err := downloadKernel(path); err != nil {
		return "", err
	}
	return path, nil
}

func downloadKernel(destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("creating kernel directory: %w", err)
	}

	tmpFile := destPath + ".tmp"
	defer os.Remove(tmpFile)

	resp, err := http.Get(kernelURL)
	if err != nil {
		return fmt.Errorf("downloading kernel: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading kernel: HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(tmpFile)
	if err != nil {
		return err
	}

	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, hasher), resp.Body); err != nil {
		f.Close()
		return fmt.Errorf("downloading kernel: %w", err)
	}
	f.Close()

	checksum := hex.EncodeToString(hasher.Sum(nil))
	if checksum != kernelChecksum {
		os.Remove(tmpFile)
		return fmt.Errorf("warden: kernel checksum verification failed (got %s)", checksum)
	}

	return os.Rename(tmpFile, destPath)
}
