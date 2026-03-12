package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

var tapNamePattern = regexp.MustCompile(`^warden-fc-[0-9a-f]{8}$`)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: warden-netsetup <create|destroy|verify> [flags]\n")
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "create":
		err = runCreate(os.Args[2:])
	case "destroy":
		err = runDestroy(os.Args[2:])
	case "verify":
		err = runVerify()
	default:
		err = fmt.Errorf("unknown command: %s", os.Args[1])
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "warden-netsetup: %v\n", err)
		os.Exit(1)
	}
}

func runCreate(args []string) error {
	var tapDevice, hostIP, guestIP, outIface string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--tap":
			i++
			tapDevice = args[i]
		case "--host-ip":
			i++
			hostIP = args[i]
		case "--guest-ip":
			i++
			guestIP = args[i]
		case "--outbound-iface":
			i++
			outIface = args[i]
		}
	}

	if err := validateTapName(tapDevice); err != nil {
		return err
	}
	if err := validateIP(hostIP); err != nil {
		return fmt.Errorf("invalid host-ip: %w", err)
	}
	if err := validateIP(guestIP); err != nil {
		return fmt.Errorf("invalid guest-ip: %w", err)
	}

	// Create TAP device
	if err := run("ip", "tuntap", "add", "dev", tapDevice, "mode", "tap"); err != nil {
		return fmt.Errorf("creating TAP: %w", err)
	}
	if err := run("ip", "addr", "add", hostIP, "dev", tapDevice); err != nil {
		return fmt.Errorf("assigning IP: %w", err)
	}
	if err := run("ip", "link", "set", tapDevice, "up"); err != nil {
		return fmt.Errorf("bringing up TAP: %w", err)
	}

	// Add iptables MASQUERADE rule
	if outIface != "" {
		if err := run("iptables", "-t", "nat", "-A", "POSTROUTING",
			"-o", outIface, "-s", guestIP, "-j", "MASQUERADE"); err != nil {
			return fmt.Errorf("adding NAT rule: %w", err)
		}
	}

	return nil
}

func runDestroy(args []string) error {
	var tapDevice, guestIP, outIface string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--tap":
			i++
			tapDevice = args[i]
		case "--guest-ip":
			i++
			guestIP = args[i]
		case "--outbound-iface":
			i++
			outIface = args[i]
		}
	}
	if err := validateTapName(tapDevice); err != nil {
		return err
	}

	// Remove iptables MASQUERADE rule (must happen before TAP deletion)
	if outIface != "" && guestIP != "" {
		run("iptables", "-t", "nat", "-D", "POSTROUTING",
			"-o", outIface, "-s", guestIP, "-j", "MASQUERADE")
	}

	// Remove TAP device (also removes associated routes)
	run("ip", "link", "del", tapDevice)
	return nil
}

func runVerify() error {
	// Check we can create and destroy a test TAP device
	testName := "warden-fc-00000000"
	if err := run("ip", "tuntap", "add", "dev", testName, "mode", "tap"); err != nil {
		return fmt.Errorf("cannot create TAP devices — check capabilities: %w", err)
	}
	run("ip", "link", "del", testName)
	fmt.Println("warden-netsetup: capabilities OK")
	return nil
}

func validateTapName(name string) error {
	if !tapNamePattern.MatchString(name) {
		return fmt.Errorf("invalid TAP name: %q (must match warden-fc-XXXXXXXX)", name)
	}
	return nil
}

func validateIP(ip string) error {
	parts := strings.SplitN(ip, "/", 2)
	if net.ParseIP(parts[0]) == nil {
		return fmt.Errorf("invalid IP: %q", ip)
	}
	// Verify within 172.16.0.0/12
	parsed := net.ParseIP(parts[0])
	network := net.IPNet{
		IP:   net.ParseIP("172.16.0.0"),
		Mask: net.CIDRMask(12, 32),
	}
	if !network.Contains(parsed) {
		return fmt.Errorf("IP %s not in 172.16.0.0/12 range", ip)
	}
	return nil
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
