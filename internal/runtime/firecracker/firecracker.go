package firecracker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/winler/warden/internal/config"
	"github.com/winler/warden/internal/runtime"
	"github.com/winler/warden/internal/runtime/shared"
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

// Run executes a command in a Firecracker microVM.
func (f *FirecrackerRuntime) Run(cfg config.SandboxConfig, command []string) (int, error) {
	vm, err := startVM(cfg, command)
	if err != nil {
		return 1, err
	}
	defer vm.cleanup()

	// TODO(vsock): Connect to guest init agent via vsock, send ExecMessage
	// with command, stream output, handle signals, return exit code.

	timeout, err := shared.ParseTimeout(cfg.Timeout)
	if err != nil {
		return 1, err
	}

	ctx := context.Background()
	var cancel context.CancelFunc = func() {}
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	// Signal handling
	cleanup := shared.SignalHandler(
		func(sig os.Signal) {
			// TODO: forward signal to guest via vsock
		},
		func() {
			vm.cleanup()
		},
	)
	defer cleanup()

	// Timeout watchdog
	if timeout > 0 {
		go func() {
			<-ctx.Done()
			if ctx.Err() == context.DeadlineExceeded {
				fmt.Fprintf(os.Stderr, "warden: killed (timeout after %s)\n", cfg.Timeout)
				vm.cleanup()
			}
		}()
	}

	// Wait for VM process
	if err := vm.cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code := exitErr.ExitCode()
			if msg := shared.ExitCodeMessage(code, cfg.Memory); msg != "" {
				fmt.Fprintln(os.Stderr, msg)
			}
			return code, nil
		}
		return 1, err
	}

	return 0, nil
}

// DryRun prints the VM configuration.
func (f *FirecrackerRuntime) DryRun(cfg config.SandboxConfig, command []string) error {
	homeDir, _ := os.UserHomeDir()
	kernelPath := defaultKernelPath(homeDir)
	rootfs := rootfsPath(homeDir, cfg.Image, cfg.Tools)

	vmConfig := map[string]interface{}{
		"runtime": "firecracker",
		"kernel":  kernelPath,
		"rootfs":  rootfs,
		"vcpus":   cfg.CPUs,
		"memory":  cfg.Memory,
		"network": cfg.Network,
		"mounts":  cfg.Mounts,
		"workdir": cfg.Workdir,
		"command": command,
	}

	data, _ := json.MarshalIndent(vmConfig, "", "  ")
	fmt.Println(string(data))
	return nil
}

