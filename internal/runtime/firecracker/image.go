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

	// Export to tar, then create ext4 image
	tmpTar := path + ".tar"
	defer os.Remove(tmpTar)

	exportCmd := exec.Command("docker", "export", "-o", tmpTar, containerName)
	exportCmd.Stderr = os.Stderr
	if err := exportCmd.Run(); err != nil {
		return "", fmt.Errorf("exporting container filesystem: %w", err)
	}

	// Create ext4 image from tar
	if err := tarToExt4(tmpTar, path); err != nil {
		os.Remove(path)
		return "", fmt.Errorf("creating ext4 image: %w", err)
	}

	return path, nil
}

// tarToExt4 creates an ext4 filesystem image from a tar archive.
func tarToExt4(tarPath, ext4Path string) error {
	// Create a 4GB sparse file
	if err := exec.Command("truncate", "-s", "4G", ext4Path).Run(); err != nil {
		return fmt.Errorf("creating sparse file: %w", err)
	}

	// Format as ext4
	if err := exec.Command("mkfs.ext4", "-F", ext4Path).Run(); err != nil {
		return fmt.Errorf("formatting ext4: %w", err)
	}

	// Use a privileged Docker container to mount the ext4 image and extract
	extractCmd := exec.Command("docker", "run", "--rm", "--privileged",
		"-v", tarPath+":/rootfs.tar:ro",
		"-v", ext4Path+":/rootfs.ext4",
		"ubuntu:24.04",
		"bash", "-c",
		"mkdir /mnt/rootfs && mount /rootfs.ext4 /mnt/rootfs && "+
			"tar xf /rootfs.tar -C /mnt/rootfs && umount /mnt/rootfs",
	)
	extractCmd.Stderr = os.Stderr
	if err := extractCmd.Run(); err != nil {
		return fmt.Errorf("extracting tar to ext4: %w", err)
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
