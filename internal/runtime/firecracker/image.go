package firecracker

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/winler/warden/internal/config"
	"github.com/winler/warden/internal/runtime"
	rdocker "github.com/winler/warden/internal/runtime/docker"
)

// RootfsFilename computes the rootfs filename for a base image + tool set.
func RootfsFilename(base string, tools []string) string {
	tag := rdocker.ImageTag(base, tools)
	name := strings.TrimPrefix(tag, "warden:")
	return name + ".ext4"
}

func rootfsPath(homeDir string, base string, tools []string) string {
	return filepath.Join(homeDir, ".warden", "firecracker", "rootfs", RootfsFilename(base, tools))
}

func rootfsExists(homeDir string, base string, tools []string) bool {
	_, err := os.Stat(rootfsPath(homeDir, base, tools))
	return err == nil
}

// BuildRootfs creates an ext4 rootfs image using Docker to assemble the filesystem.
func BuildRootfs(homeDir string, base string, tools []string) (string, error) {
	path := rootfsPath(homeDir, base, tools)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	fmt.Fprintf(os.Stderr, "warden: building rootfs image (first run only)...\n")

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("creating rootfs directory: %w", err)
	}

	// Use Docker to build the image, then export filesystem
	dockerTag, err := rdocker.BuildImage(base, tools)
	if err != nil {
		return "", fmt.Errorf("building docker image for rootfs export: %w", err)
	}

	// Create a temporary container and export its filesystem
	containerName := fmt.Sprintf("warden-rootfs-export-%d", os.Getpid())
	if err := exec.Command("docker", "create", "--name", containerName, dockerTag).Run(); err != nil {
		return "", fmt.Errorf("creating export container: %w", err)
	}
	defer exec.Command("docker", "rm", containerName).Run()

	// Export to tar, then extract and create ext4 image
	tmpTar := path + ".tar"
	defer os.Remove(tmpTar)

	exportCmd := exec.Command("docker", "export", "-o", tmpTar, containerName)
	exportCmd.Stderr = os.Stderr
	if err := exportCmd.Run(); err != nil {
		return "", fmt.Errorf("exporting container filesystem: %w", err)
	}

	extractDir, err := os.MkdirTemp("", "warden-rootfs-extract-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(extractDir)

	extractCmd := exec.Command("tar", "xf", tmpTar, "-C", extractDir, "--exclude=dev/*")
	extractCmd.Stderr = os.Stderr
	if err := extractCmd.Run(); err != nil {
		return "", fmt.Errorf("extracting rootfs tar: %w", err)
	}

	// Inject warden-init as PID 1
	initBin, err := findWardenInitBinary(homeDir)
	if err != nil {
		return "", err
	}
	if err := injectWardenInit(initBin, extractDir); err != nil {
		return "", fmt.Errorf("injecting warden-init: %w", err)
	}

	// Inject warden-shim for command proxying
	shimBin, err := findWardenShimBinary(homeDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warden: warden-shim not found, proxy will not work in Firecracker\n")
	} else {
		if err := injectBinary(shimBin, extractDir, "usr/local/bin/warden-shim"); err != nil {
			return "", fmt.Errorf("injecting warden-shim: %w", err)
		}
	}

	if err := dirToExt4(extractDir, path, "4G"); err != nil {
		os.Remove(path)
		return "", fmt.Errorf("creating ext4 image: %w", err)
	}

	return path, nil
}

// injectWardenInit copies the warden-init binary into the rootfs directory.
func injectWardenInit(binaryPath, rootDir string) error {
	src, err := os.Open(binaryPath)
	if err != nil {
		return fmt.Errorf("opening warden-init binary: %w", err)
	}
	defer src.Close()

	dst, err := os.OpenFile(filepath.Join(rootDir, "warden-init"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	return err
}

// findWardenShimBinary locates the pre-built warden-shim binary.
func findWardenShimBinary(homeDir string) (string, error) {
	exe, err := os.Executable()
	if err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "warden-shim")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	candidate := filepath.Join(homeDir, ".warden", "bin", "warden-shim")
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}
	return "", fmt.Errorf("warden-shim not found")
}

// injectBinary copies a binary into the rootfs directory at the given relative path.
func injectBinary(binaryPath, rootDir, relPath string) error {
	destDir := filepath.Join(rootDir, filepath.Dir(relPath))
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	src, err := os.Open(binaryPath)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(filepath.Join(rootDir, relPath), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer dst.Close()
	_, err = io.Copy(dst, src)
	return err
}

// findWardenInitBinary locates the pre-built warden-init binary.
func findWardenInitBinary(homeDir string) (string, error) {
	exe, err := os.Executable()
	if err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "warden-init")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	candidate := filepath.Join(homeDir, ".warden", "bin", "warden-init")
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}

	return "", fmt.Errorf("warden-init binary not found. Build with: CGO_ENABLED=0 go build -o ~/.warden/bin/warden-init ./cmd/warden-init/")
}

// dirToExt4 creates an ext4 filesystem image populated from a directory.
// Uses mke2fs -d which works without root or privileged containers.
func dirToExt4(srcDir, ext4Path, size string) error {
	if err := exec.Command("truncate", "-s", size, ext4Path).Run(); err != nil {
		return fmt.Errorf("creating sparse file: %w", err)
	}
	cmd := exec.Command("mke2fs",
		"-t", "ext4",
		"-d", srcDir,
		"-F",
		"-L", "rootfs",
		ext4Path,
	)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("mke2fs -d: %w", err)
	}
	return nil
}

// EnsureImage implements Runtime.EnsureImage for Firecracker.
func (f *FirecrackerRuntime) EnsureImage(cfg config.SandboxConfig) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return BuildRootfs(homeDir, cfg.Image, cfg.Tools)
}

// ListImages returns cached Firecracker rootfs images.
func (f *FirecrackerRuntime) ListImages() ([]runtime.ImageInfo, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	rootfsDir := filepath.Join(homeDir, ".warden", "firecracker", "rootfs")
	entries, err := os.ReadDir(rootfsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var images []runtime.ImageInfo
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".ext4") {
			info, _ := e.Info()
			img := runtime.ImageInfo{
				Tag:     e.Name(),
				Runtime: "firecracker",
			}
			if info != nil {
				img.Size = info.Size()
				img.CreatedAt = info.ModTime()
			}
			images = append(images, img)
		}
	}
	return images, nil
}

// PruneImages removes all cached Firecracker rootfs images.
func (f *FirecrackerRuntime) PruneImages() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	rootfsDir := filepath.Join(homeDir, ".warden", "firecracker", "rootfs")
	entries, err := os.ReadDir(rootfsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	count := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".ext4") {
			os.Remove(filepath.Join(rootfsDir, e.Name()))
			count++
		}
	}
	if count > 0 {
		fmt.Printf("Removed %d firecracker rootfs image(s).\n", count)
	}
	return nil
}
