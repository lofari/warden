package cli

import (
	"fmt"
	"os"
	"os/exec"
	goruntime "runtime"

	"github.com/spf13/cobra"
	"github.com/winler/warden/internal/runtime/firecracker"
)

func newSetupCommand() *cobra.Command {
	setup := &cobra.Command{
		Use:   "setup",
		Short: "Set up optional runtime backends",
	}

	fc := &cobra.Command{
		Use:   "firecracker",
		Short: "Set up Firecracker microVM runtime",
		RunE: func(cmd *cobra.Command, args []string) error {
			return setupFirecracker()
		},
	}

	setup.AddCommand(fc)
	return setup
}

func setupFirecracker() error {
	if goruntime.GOOS != "linux" {
		return fmt.Errorf("warden: firecracker is only supported on Linux")
	}

	// Check /dev/kvm
	fmt.Fprintln(os.Stderr, "Setting up Firecracker runtime...")
	fmt.Fprint(os.Stderr, "  Checking /dev/kvm... ")
	if _, err := os.Stat("/dev/kvm"); err != nil {
		fmt.Fprintln(os.Stderr, "NOT FOUND")
		return fmt.Errorf("warden: /dev/kvm not available. Ensure KVM is enabled")
	}
	fmt.Fprintln(os.Stderr, "OK")

	// Check Docker (needed for virtiofsd build and rootfs building)
	fmt.Fprint(os.Stderr, "  Checking Docker... ")
	if _, err := exec.LookPath("docker"); err != nil {
		fmt.Fprintln(os.Stderr, "NOT FOUND")
		return fmt.Errorf("warden: docker is required for Firecracker setup (virtiofsd build, rootfs building)")
	}
	fmt.Fprintln(os.Stderr, "OK")

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	// Create directory structure
	if err := firecracker.SetupDirs(homeDir); err != nil {
		return fmt.Errorf("creating directories: %w", err)
	}

	// Download kernel
	if err := firecracker.DownloadKernelSetup(homeDir); err != nil {
		return fmt.Errorf("kernel: %w", err)
	}

	// Download Firecracker binary
	if err := firecracker.DownloadFirecracker(homeDir); err != nil {
		return fmt.Errorf("firecracker: %w", err)
	}

	// Build virtiofsd
	if err := firecracker.BuildAndInstallVirtiofsd(homeDir); err != nil {
		return fmt.Errorf("virtiofsd: %w", err)
	}

	// Build warden-netsetup
	if err := firecracker.BuildNetsetup(); err != nil {
		return fmt.Errorf("warden-netsetup: %w", err)
	}

	// System checks
	fmt.Fprintln(os.Stderr, "\nSystem configuration:")
	firecracker.CheckIPForwarding()
	firecracker.CheckNetsetupCaps()

	// Verification
	fmt.Fprintln(os.Stderr, "\nVerification:")
	rt := &firecracker.FirecrackerRuntime{}
	if err := rt.Preflight(); err != nil {
		fmt.Fprintf(os.Stderr, "  Preflight check: FAILED — %v\n", err)
		fmt.Fprintln(os.Stderr, "  Some components may need manual installation. Re-run to check status.")
	} else {
		fmt.Fprintln(os.Stderr, "  Preflight check: PASSED")
		fmt.Fprintln(os.Stderr, "\nFirecracker runtime is ready! Use --runtime firecracker to use it.")
	}

	return nil
}
