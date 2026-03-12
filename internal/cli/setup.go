package cli

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	goruntime "runtime"

	"github.com/spf13/cobra"
)

func newSetupCommand() *cobra.Command {
	setup := &cobra.Command{
		Use:   "setup",
		Short: "Set up optional runtime backends",
	}

	fc := &cobra.Command{
		Use:   "firecracker",
		Short: "Set up Firecracker microVM runtime (requires sudo)",
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

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	binDir := filepath.Join(homeDir, ".warden", "firecracker", "bin")
	os.MkdirAll(binDir, 0o755)

	fmt.Println("Setting up Firecracker runtime...")

	// Step 1: Check /dev/kvm
	fmt.Print("  Checking /dev/kvm... ")
	if _, err := os.Stat("/dev/kvm"); err != nil {
		fmt.Println("NOT FOUND")
		return fmt.Errorf("warden: /dev/kvm not available. Ensure KVM is enabled in your kernel")
	}
	fmt.Println("OK")

	// Step 2: Add user to kvm group
	fmt.Print("  Adding user to kvm group... ")
	u, _ := user.Current()
	if err := exec.Command("sudo", "usermod", "-aG", "kvm", u.Username).Run(); err != nil {
		fmt.Println("FAILED")
		return fmt.Errorf("adding user to kvm group: %w (run with sudo)", err)
	}
	fmt.Println("OK")

	// Step 3: Download Firecracker binary
	fmt.Print("  Downloading firecracker binary... ")
	fcPath := filepath.Join(binDir, "firecracker")
	if _, err := os.Stat(fcPath); err != nil {
		fmt.Println("SKIPPED (manual download required)")
		fmt.Printf("    Download from https://github.com/firecracker-microvm/firecracker/releases\n")
		fmt.Printf("    Place binary at: %s\n", fcPath)
	} else {
		fmt.Println("OK (already exists)")
	}

	// Step 4: Download virtiofsd
	fmt.Print("  Downloading virtiofsd... ")
	vfsPath := filepath.Join(binDir, "virtiofsd")
	if _, err := os.Stat(vfsPath); err != nil {
		fmt.Println("SKIPPED (manual download required)")
		fmt.Printf("    Place binary at: %s\n", vfsPath)
	} else {
		fmt.Println("OK (already exists)")
	}

	// Step 5: Build and install warden-netsetup
	fmt.Print("  Installing warden-netsetup... ")
	netsetupPath := "/usr/local/bin/warden-netsetup"
	if _, err := os.Stat(netsetupPath); err != nil {
		fmt.Println("SKIPPED")
		fmt.Println("    Build: go build -o warden-netsetup ./cmd/warden-netsetup/")
		fmt.Println("    Install: sudo cp warden-netsetup /usr/local/bin/")
		fmt.Println("    Set cap: sudo setcap cap_net_admin+ep /usr/local/bin/warden-netsetup")
	} else {
		fmt.Println("OK (already exists)")
	}

	// Step 6: Enable IP forwarding
	fmt.Print("  Checking IP forwarding... ")
	out, _ := os.ReadFile("/proc/sys/net/ipv4/ip_forward")
	if string(out) == "1\n" || string(out) == "1" {
		fmt.Println("OK (already enabled)")
	} else {
		fmt.Println("DISABLED")
		fmt.Println("    To enable: sudo sysctl -w net.ipv4.ip_forward=1")
		fmt.Println("    To persist: echo 'net.ipv4.ip_forward=1' | sudo tee /etc/sysctl.d/99-warden.conf")
	}

	// Step 7: Verify
	fmt.Println("\nSetup summary:")
	fmt.Println("  Some components may need manual installation.")
	fmt.Println("  Re-run 'warden setup firecracker' to check status.")
	fmt.Println("  You may need to log out and back in for the kvm group change to take effect.")

	return nil
}
