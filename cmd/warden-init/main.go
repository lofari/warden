package main

import (
	"fmt"
	"os"
	"syscall"
)

func main() {
	// Mount essential filesystems
	if err := mountFilesystems(); err != nil {
		fmt.Fprintf(os.Stderr, "warden-init: mount error: %v\n", err)
		os.Exit(1)
	}

	// TODO: Set up vsock listener, receive ExecMessage, execute command,
	// stream output, return exit code. For now, this is a placeholder
	// that will be completed when the VM lifecycle is implemented.
	fmt.Fprintln(os.Stderr, "warden-init: ready")

	// Block forever (placeholder — real implementation uses vsock event loop)
	select {}
}

func mountFilesystems() error {
	mounts := []struct {
		source string
		target string
		fstype string
		flags  uintptr
	}{
		{"proc", "/proc", "proc", 0},
		{"sysfs", "/sys", "sysfs", 0},
		{"devtmpfs", "/dev", "devtmpfs", 0},
	}

	for _, m := range mounts {
		os.MkdirAll(m.target, 0o755)
		if err := syscall.Mount(m.source, m.target, m.fstype, m.flags, ""); err != nil {
			// Don't fail if already mounted
			if !os.IsExist(err) {
				return fmt.Errorf("mounting %s: %w", m.target, err)
			}
		}
	}
	return nil
}
