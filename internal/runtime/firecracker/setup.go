package firecracker

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	firecrackerVersion  = "v1.15.0"
	firecrackerURL      = "https://github.com/firecracker-microvm/firecracker/releases/download/" + firecrackerVersion + "/firecracker-" + firecrackerVersion + "-x86_64.tgz"
	firecrackerChecksum = "00cadf7f21e709e939dc0c8d16e2d2ce7b975a62bec6c50f74b421cc8ab3cab4"
	firecrackerBinName  = "release-" + firecrackerVersion + "-x86_64/firecracker-" + firecrackerVersion + "-x86_64"

	virtiofsdVersion  = "v1.13.3"
	virtiofsdURL      = "https://gitlab.com/virtio-fs/virtiofsd/-/archive/" + virtiofsdVersion + "/virtiofsd-" + virtiofsdVersion + ".tar.gz"
	virtiofsdChecksum = "9d5e67e7b19f52a8d3c411acf9beed6206e9352226cbf1e2bdaa4ed609a927ce"
)

// downloadAndVerify downloads a URL and verifies its SHA256 checksum.
// Returns the path to the downloaded temp file.
func downloadAndVerify(url, expectedChecksum string) (string, error) {
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("downloading %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("downloading %s: HTTP %d", url, resp.StatusCode)
	}

	tmpFile, err := os.CreateTemp("", "warden-download-*")
	if err != nil {
		return "", err
	}

	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmpFile, hasher), resp.Body); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("downloading %s: %w", url, err)
	}
	tmpFile.Close()

	checksum := hex.EncodeToString(hasher.Sum(nil))
	if checksum != expectedChecksum {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("checksum mismatch for %s: got %s, want %s", url, checksum, expectedChecksum)
	}

	return tmpFile.Name(), nil
}

// extractFirecrackerBinary extracts the firecracker binary from the release tarball.
func extractFirecrackerBinary(tarballPath, destPath string) error {
	f, err := os.Open(tarballPath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar: %w", err)
		}
		// Path traversal protection
		clean := filepath.Clean(hdr.Name)
		if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			continue
		}
		if hdr.Name == firecrackerBinName {
			os.MkdirAll(filepath.Dir(destPath), 0o755)
			out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			return out.Close()
		}
	}
	return fmt.Errorf("binary %s not found in tarball", firecrackerBinName)
}

// buildVirtiofsd downloads the virtiofsd source and builds it inside a Docker container.
func buildVirtiofsd(tarballPath, destPath string) error {
	// Extract source to temp dir
	tmpDir, err := os.MkdirTemp("", "warden-virtiofsd-build-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	// Extract tarball
	f, err := os.Open(tarballPath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar: %w", err)
		}

		target := filepath.Join(tmpDir, hdr.Name)
		// Path traversal protection
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(tmpDir)+string(os.PathSeparator)) {
			continue
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			os.MkdirAll(target, 0o755)
		case tar.TypeReg:
			os.MkdirAll(filepath.Dir(target), 0o755)
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY, os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, io.LimitReader(tr, 100*1024*1024)); err != nil {
				out.Close()
				return err
			}
			out.Close()
		}
	}

	// Find the source directory (virtiofsd-v1.13.3/)
	srcDir := filepath.Join(tmpDir, "virtiofsd-"+virtiofsdVersion)

	// Build using Docker with Rust toolchain
	cmd := exec.Command("docker", "run", "--rm",
		"-v", srcDir+":/src",
		"-w", "/src",
		"rust:1.82",
		"cargo", "build", "--release",
	)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("building virtiofsd: %w", err)
	}

	// Copy binary out
	builtBin := filepath.Join(srcDir, "target", "release", "virtiofsd")
	return copyBinary(builtBin, destPath)
}

func copyBinary(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	os.MkdirAll(filepath.Dir(dst), 0o755)
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

// fileMatchesChecksum checks if a file exists and matches the expected SHA256.
func fileMatchesChecksum(path, expectedChecksum string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return false
	}
	return hex.EncodeToString(hasher.Sum(nil)) == expectedChecksum
}

// SetupDirs creates the Firecracker directory structure.
func SetupDirs(homeDir string) error {
	dirs := []string{
		filepath.Join(homeDir, ".warden", "firecracker", "kernel"),
		filepath.Join(homeDir, ".warden", "firecracker", "rootfs"),
		filepath.Join(homeDir, ".warden", "firecracker", "bin"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// DownloadKernelSetup downloads the kernel for setup (with idempotency check).
func DownloadKernelSetup(homeDir string) error {
	dest := defaultKernelPath(homeDir)
	if fileMatchesChecksum(dest, kernelChecksum) {
		return nil // already installed
	}

	fmt.Fprintf(os.Stderr, "  Downloading kernel %s...\n", kernelVersion)
	return downloadKernel(dest)
}

// DownloadFirecracker downloads and extracts the Firecracker binary.
func DownloadFirecracker(homeDir string) error {
	dest := filepath.Join(homeDir, ".warden", "firecracker", "bin", "firecracker")

	// Simple existence check (we don't have a per-binary checksum, just tarball)
	if _, err := os.Stat(dest); err == nil {
		return nil // already installed
	}

	fmt.Fprintf(os.Stderr, "  Downloading Firecracker %s...\n", firecrackerVersion)
	tarball, err := downloadAndVerify(firecrackerURL, firecrackerChecksum)
	if err != nil {
		return err
	}
	defer os.Remove(tarball)

	return extractFirecrackerBinary(tarball, dest)
}

// BuildAndInstallVirtiofsd downloads virtiofsd source and builds via Docker.
func BuildAndInstallVirtiofsd(homeDir string) error {
	dest := filepath.Join(homeDir, ".warden", "firecracker", "bin", "virtiofsd")
	if _, err := os.Stat(dest); err == nil {
		return nil // already installed
	}

	fmt.Fprintf(os.Stderr, "  Downloading virtiofsd %s source...\n", virtiofsdVersion)
	tarball, err := downloadAndVerify(virtiofsdURL, virtiofsdChecksum)
	if err != nil {
		return err
	}
	defer os.Remove(tarball)

	fmt.Fprintln(os.Stderr, "  Building virtiofsd (this may take a few minutes)...")
	return buildVirtiofsd(tarball, dest)
}

// BuildNetsetup builds the warden-netsetup binary.
func BuildNetsetup() error {
	tmpBin := "/tmp/warden-netsetup-build"
	fmt.Fprintln(os.Stderr, "  Building warden-netsetup...")

	cmd := exec.Command("go", "build", "-o", tmpBin, "./cmd/warden-netsetup/")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("building warden-netsetup: %w", err)
	}

	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  warden-netsetup built at", tmpBin)
	fmt.Fprintln(os.Stderr, "  Install with:")
	fmt.Fprintf(os.Stderr, "    sudo install %s /usr/local/bin/warden-netsetup && sudo setcap cap_net_admin+ep /usr/local/bin/warden-netsetup\n", tmpBin)
	return nil
}

// CheckIPForwarding checks if IP forwarding is enabled.
func CheckIPForwarding() {
	data, _ := os.ReadFile("/proc/sys/net/ipv4/ip_forward")
	val := strings.TrimSpace(string(data))
	if val == "1" {
		fmt.Fprintln(os.Stderr, "  IP forwarding: enabled")
	} else {
		fmt.Fprintln(os.Stderr, "  IP forwarding: DISABLED")
		fmt.Fprintln(os.Stderr, "    To enable: sudo sysctl -w net.ipv4.ip_forward=1")
		fmt.Fprintln(os.Stderr, "    To persist: echo 'net.ipv4.ip_forward=1' | sudo tee /etc/sysctl.d/99-warden.conf")
	}
}

// CheckNetsetupCaps checks if warden-netsetup has the required capabilities.
func CheckNetsetupCaps() {
	path := "/usr/local/bin/warden-netsetup"
	if _, err := os.Stat(path); err != nil {
		fmt.Fprintln(os.Stderr, "  warden-netsetup: NOT INSTALLED")
		return
	}
	out, err := exec.Command("getcap", path).Output()
	if err != nil || !strings.Contains(string(out), "cap_net_admin") {
		fmt.Fprintln(os.Stderr, "  warden-netsetup: missing cap_net_admin")
		fmt.Fprintf(os.Stderr, "    Run: sudo setcap cap_net_admin+ep %s\n", path)
	} else {
		fmt.Fprintln(os.Stderr, "  warden-netsetup: OK")
	}
}
