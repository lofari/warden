package guest

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ConfigureNetwork sets up the guest network interface.
func ConfigureNetwork(guestIP, gateway, dns string) error {
	gwIP := strings.Split(gateway, "/")[0]

	commands := [][]string{
		{"ip", "addr", "add", guestIP, "dev", "eth0"},
		{"ip", "link", "set", "eth0", "up"},
		{"ip", "route", "add", "default", "via", gwIP},
	}
	for _, args := range commands {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("running %s: %w", strings.Join(args, " "), err)
		}
	}

	nameserver := dns
	if nameserver == "" {
		nameserver = "8.8.8.8"
	}
	if err := os.WriteFile("/etc/resolv.conf", []byte("nameserver "+nameserver+"\n"), 0o644); err != nil {
		return fmt.Errorf("writing resolv.conf: %w", err)
	}
	return nil
}
