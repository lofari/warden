package firecracker

import (
	"fmt"
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

	if err := dirToExt4(extractDir, path, "4G"); err != nil {
		os.Remove(path)
		return "", fmt.Errorf("creating ext4 image: %w", err)
	}

	return path, nil
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
