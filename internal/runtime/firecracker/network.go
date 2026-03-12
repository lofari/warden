package firecracker

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// subnetForIndex computes the gateway and guest IPs for a given allocation index.
// Each index maps to a /30 subnet within 172.16.0.0/12.
func subnetForIndex(index uint32) (gateway, guest string) {
	// Base: 172.16.0.0, stride 4 per /30
	base := uint32(0xAC100000) // 172.16.0.0
	offset := index * 4
	netAddr := base + offset

	gwAddr := netAddr + 1
	guestAddr := netAddr + 2

	gw := fmt.Sprintf("%d.%d.%d.%d/30",
		(gwAddr>>24)&0xFF, (gwAddr>>16)&0xFF, (gwAddr>>8)&0xFF, gwAddr&0xFF)
	g := fmt.Sprintf("%d.%d.%d.%d/30",
		(guestAddr>>24)&0xFF, (guestAddr>>16)&0xFF, (guestAddr>>8)&0xFF, guestAddr&0xFF)
	return gw, g
}

// allocateSubnet allocates the next available /30 subnet.
// Returns gateway IP, guest IP, and a release function.
func allocateSubnet(allocFile string) (gateway, guest string, release func(), err error) {
	if err := os.MkdirAll(filepath.Dir(allocFile), 0o755); err != nil {
		return "", "", nil, err
	}

	f, err := os.OpenFile(allocFile, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return "", "", nil, err
	}

	// Lock file
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return "", "", nil, fmt.Errorf("locking allocation file: %w", err)
	}

	// Read current counter
	var counter uint32
	buf := make([]byte, 4)
	if n, _ := f.Read(buf); n == 4 {
		counter = binary.LittleEndian.Uint32(buf)
	}

	// Wrap at ~262K (the usable range within 172.16.0.0/12)
	const maxIndex = 262144
	index := counter % maxIndex

	// Write incremented counter
	binary.LittleEndian.PutUint32(buf, counter+1)
	f.Seek(0, 0)
	f.Write(buf)
	f.Truncate(4)

	// Unlock
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	f.Close()

	gw, g := subnetForIndex(index)
	releaseFunc := func() {
		// Deliberate simplification: counter increments monotonically and wraps at 262K.
	}
	return gw, g, releaseFunc, nil
}

// tapName generates a unique TAP device name.
func tapName() string {
	var buf [4]byte
	rand.Read(buf[:])
	return fmt.Sprintf("warden-fc-%x", buf)
}
