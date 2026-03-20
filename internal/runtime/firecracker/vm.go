package firecracker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/winler/warden/internal/config"
)

// parseMemoryMiB parses a memory string (e.g. "512m", "2g", "1024") into MiB.
// Empty string returns the default of 1024 MiB.
func parseMemoryMiB(s string) (int, error) {
	if s == "" {
		return 1024, nil
	}
	s = strings.TrimSpace(s)

	var numStr string
	var multiplier int

	last := strings.ToLower(s[len(s)-1:])
	switch last {
	case "g":
		numStr = s[:len(s)-1]
		multiplier = 1024
	case "m":
		numStr = s[:len(s)-1]
		multiplier = 1
	default:
		numStr = s
		multiplier = 1
	}

	n, err := strconv.Atoi(numStr)
	if err != nil {
		return 0, fmt.Errorf("warden: invalid memory value %q", s)
	}

	result := n * multiplier
	if result <= 0 {
		return 0, fmt.Errorf("warden: memory must be positive, got %q", s)
	}
	return result, nil
}

type vmInstance struct {
	cmd         *exec.Cmd
	socketPath  string
	vsockPath   string
	virtiofs    []*virtiofsInstance
	tapDevice   string
	guestIP     string
	outIface    string
	releaseIP   func()
	overlayPath string // track overlay for cleanup
}

// startVM configures and boots a Firecracker microVM.
func startVM(cfg config.SandboxConfig, command []string) (*vmInstance, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	// Create a temp directory for this VM's API socket
	tmpDir, err := os.MkdirTemp("", "warden-fc-*")
	if err != nil {
		return nil, err
	}
	socketPath := filepath.Join(tmpDir, "firecracker.sock")

	vm := &vmInstance{
		socketPath: socketPath,
	}

	// Resolve kernel
	globalCfgPath := filepath.Join(homeDir, ".warden", "config.yaml")
	globalCfg, err := config.LoadGlobalConfig(globalCfgPath)
	if err != nil {
		return nil, err
	}
	kernelPath, err := resolveKernelPath(globalCfg.Firecracker.Kernel, homeDir)
	if err != nil {
		return nil, err
	}

	// Resolve rootfs
	rootfs := rootfsPath(homeDir, cfg.Image, cfg.Tools)

	// Create overlay for writable rootfs
	overlayDir := filepath.Join(homeDir, ".warden", "firecracker", "overlays")
	os.MkdirAll(overlayDir, 0o755)
	overlayPath := filepath.Join(overlayDir, fmt.Sprintf("overlay-%d.ext4", os.Getpid()))
	vm.overlayPath = overlayPath
	if err := copyFile(rootfs, overlayPath); err != nil {
		return nil, fmt.Errorf("creating rootfs overlay: %w", err)
	}

	// Start virtiofsd for each mount
	for i, m := range cfg.Mounts {
		tag := fmt.Sprintf("mount%d", i)
		vfs, err := startVirtiofs(homeDir, m.Path, tag)
		if err != nil {
			vm.cleanup()
			return nil, err
		}
		vm.virtiofs = append(vm.virtiofs, vfs)
	}

	// Handle networking
	if cfg.Network {
		allocFile := filepath.Join(homeDir, ".warden", "firecracker", "net-alloc")
		gwIP, guestIP, release, err := allocateSubnet(allocFile)
		if err != nil {
			vm.cleanup()
			return nil, err
		}
		vm.releaseIP = release

		tap := tapName()
		vm.tapDevice = tap
		vm.guestIP = guestIP
		outIface := detectOutboundInterface()
		vm.outIface = outIface
		setupCmd := exec.Command("/usr/local/bin/warden-netsetup", "create",
			"--tap", tap,
			"--host-ip", gwIP,
			"--guest-ip", guestIP,
			"--outbound-iface", outIface,
		)
		setupCmd.Stderr = os.Stderr
		if err := setupCmd.Run(); err != nil {
			vm.cleanup()
			return nil, fmt.Errorf("warden: failed to create network interface. Check warden-netsetup capabilities")
		}
	}

	// Start Firecracker process
	fcPath := filepath.Join(homeDir, ".warden", "firecracker", "bin", "firecracker")
	vm.cmd = exec.Command(fcPath,
		"--api-sock", socketPath,
	)
	vm.cmd.Stderr = os.Stderr

	if err := vm.cmd.Start(); err != nil {
		vm.cleanup()
		return nil, fmt.Errorf("starting firecracker: %w", err)
	}

	// Wait for API socket to be ready
	if err := waitForSocket(socketPath, 5*time.Second); err != nil {
		vm.cleanup()
		return nil, err
	}

	// Configure VM via API
	if err := vm.configureVM(kernelPath, overlayPath, cfg); err != nil {
		vm.cleanup()
		return nil, err
	}

	// Boot VM
	if err := vm.boot(); err != nil {
		vm.cleanup()
		return nil, err
	}

	return vm, nil
}

func (vm *vmInstance) configureVM(kernelPath, rootfsPath string, cfg config.SandboxConfig) error {
	// Set kernel
	if err := vm.apiPut("/boot-source", map[string]interface{}{
		"kernel_image_path": kernelPath,
		"boot_args":         "console=ttyS0 reboot=k panic=1 pci=off",
	}); err != nil {
		return fmt.Errorf("setting kernel: %w", err)
	}

	// Set rootfs
	if err := vm.apiPut("/drives/rootfs", map[string]interface{}{
		"drive_id":       "rootfs",
		"path_on_host":   rootfsPath,
		"is_root_device": true,
		"is_read_only":   false,
	}); err != nil {
		return fmt.Errorf("setting rootfs: %w", err)
	}

	// Set machine config
	cpus := cfg.CPUs
	if cpus == 0 {
		cpus = 1
	}
	mem, err := parseMemoryMiB(cfg.Memory)
	if err != nil {
		return err
	}
	if err := vm.apiPut("/machine-config", map[string]interface{}{
		"vcpu_count":   cpus,
		"mem_size_mib": mem,
	}); err != nil {
		return fmt.Errorf("setting machine config: %w", err)
	}

	// Set network if TAP device exists
	if vm.tapDevice != "" {
		if err := vm.apiPut("/network-interfaces/eth0", map[string]interface{}{
			"iface_id":      "eth0",
			"host_dev_name": vm.tapDevice,
			"guest_mac":     "AA:FC:00:00:00:01",
		}); err != nil {
			return fmt.Errorf("setting network: %w", err)
		}
	}

	// Configure vsock device for host-guest communication
	vsockPath := filepath.Join(filepath.Dir(vm.socketPath), "vsock.sock")
	vm.vsockPath = vsockPath
	if err := vm.apiPut("/vsock", map[string]interface{}{
		"vsock_id":  "vsock0",
		"guest_cid": 3,
		"uds_path":  vsockPath,
	}); err != nil {
		return fmt.Errorf("setting vsock: %w", err)
	}

	return nil
}

func (vm *vmInstance) boot() error {
	return vm.apiPut("/actions", map[string]interface{}{
		"action_type": "InstanceStart",
	})
}

func (vm *vmInstance) cleanup() {
	// Remove overlay rootfs copy
	if vm.overlayPath != "" {
		os.Remove(vm.overlayPath)
	}

	// Stop virtiofsd instances
	for _, vfs := range vm.virtiofs {
		vfs.stop()
	}

	// Destroy TAP device and remove iptables rule
	if vm.tapDevice != "" {
		destroyArgs := []string{"destroy", "--tap", vm.tapDevice}
		if vm.guestIP != "" {
			destroyArgs = append(destroyArgs, "--guest-ip", vm.guestIP, "--outbound-iface", vm.outIface)
		}
		exec.Command("/usr/local/bin/warden-netsetup", destroyArgs...).Run()
	}

	// Release IP
	if vm.releaseIP != nil {
		vm.releaseIP()
	}

	// Kill Firecracker process
	if vm.cmd != nil && vm.cmd.Process != nil {
		vm.cmd.Process.Kill()
		vm.cmd.Wait()
	}

	// Clean up socket directory
	if vm.socketPath != "" {
		os.RemoveAll(filepath.Dir(vm.socketPath))
	}
}

func (vm *vmInstance) apiPut(path string, body interface{}) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", vm.socketPath)
			},
		},
	}

	req, err := http.NewRequest("PUT", "http://localhost"+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var buf bytes.Buffer
		buf.ReadFrom(resp.Body)
		return fmt.Errorf("firecracker API %s: %s — %s", path, resp.Status, buf.String())
	}

	return nil
}

func waitForSocket(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.Dial("unix", path)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("warden: firecracker API socket not ready after %s", timeout)
}

func detectOutboundInterface() string {
	out, err := exec.Command("ip", "route", "show", "default").Output()
	if err != nil {
		return "eth0" // fallback
	}
	fields := bytes.Fields(out)
	for i, f := range fields {
		if string(f) == "dev" && i+1 < len(fields) {
			return string(fields[i+1])
		}
	}
	return "eth0"
}

func copyFile(src, dst string) error {
	// Try reflink (copy-on-write) first for instant, space-efficient copies
	if err := exec.Command("cp", "--reflink=auto", src, dst).Run(); err != nil {
		// Fallback to buffered io.Copy
		in, err := os.Open(src)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(dst)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			os.Remove(dst)
			return err
		}
		if err := out.Close(); err != nil {
			os.Remove(dst)
			return err
		}
	}
	return nil
}
