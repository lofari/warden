package guest

import (
	"os/exec"
)

// ConfigureNetwork sets up the guest network interface if present.
func ConfigureNetwork(guestIP, gateway string) error {
	// Find the network interface (typically eth0 in Firecracker guests)
	iface := "eth0"

	if err := exec.Command("ip", "addr", "add", guestIP, "dev", iface).Run(); err != nil {
		return err
	}
	if err := exec.Command("ip", "link", "set", iface, "up").Run(); err != nil {
		return err
	}
	if err := exec.Command("ip", "route", "add", "default", "via", gateway).Run(); err != nil {
		return err
	}
	return nil
}
