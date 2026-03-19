package firecracker

import (
	"bufio"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

// subnetForIndex computes the gateway and guest IPs for a given allocation index.
// Each index maps to a /30 subnet within 172.16.0.0/12.
func subnetForIndex(index uint32) (gateway, guest string) {
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

type allocEntry struct {
	index uint32
	pid   int
}

// parseAllocFile reads the PID-tracked allocation file.
// Detects old 4-byte binary counter format and resets to empty.
func parseAllocFile(data []byte) []allocEntry {
	// Detect old binary counter format (exactly 4 bytes, not valid text)
	if len(data) == 4 {
		return nil
	}

	var entries []allocEntry
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		idx, err := strconv.ParseUint(parts[0], 10, 32)
		if err != nil {
			continue
		}
		pid, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}
		entries = append(entries, allocEntry{index: uint32(idx), pid: pid})
	}
	return entries
}

func writeAllocFile(f *os.File, entries []allocEntry) error {
	f.Seek(0, 0)
	f.Truncate(0)
	for _, e := range entries {
		fmt.Fprintf(f, "%d:%d\n", e.index, e.pid)
	}
	return nil
}

func isPIDAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	// EPERM means the process exists but we lack permission to signal it
	return err == nil || errors.Is(err, syscall.EPERM)
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

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return "", "", nil, fmt.Errorf("locking allocation file: %w", err)
	}

	// Read from the locked FD, not os.ReadFile (which opens a separate FD)
	f.Seek(0, 0)
	data, _ := io.ReadAll(f)
	entries := parseAllocFile(data)

	// Find reclaimable entries (dead PIDs)
	var alive []allocEntry
	var reclaimable []uint32
	for _, e := range entries {
		if isPIDAlive(e.pid) {
			alive = append(alive, e)
		} else {
			reclaimable = append(reclaimable, e.index)
		}
	}

	// Prefer reclaimable indices (lowest first), then find gaps in alive, then max+1
	var index uint32
	if len(reclaimable) > 0 {
		sort.Slice(reclaimable, func(i, j int) bool { return reclaimable[i] < reclaimable[j] })
		index = reclaimable[0]
	} else if len(alive) > 0 {
		// Sort alive by index to find gaps
		sort.Slice(alive, func(i, j int) bool { return alive[i].index < alive[j].index })
		found := false
		var expected uint32
		for _, e := range alive {
			if e.index > expected {
				index = expected
				found = true
				break
			}
			expected = e.index + 1
		}
		if !found {
			index = expected // max + 1
		}
	}
	// else: empty file, index stays 0

	myPID := os.Getpid()
	alive = append(alive, allocEntry{index: index, pid: myPID})
	writeAllocFile(f, alive)

	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	f.Close()

	gw, g := subnetForIndex(index)

	releaseFunc := func() {
		rf, err := os.OpenFile(allocFile, os.O_RDWR, 0o644)
		if err != nil {
			return
		}
		defer rf.Close()
		if err := syscall.Flock(int(rf.Fd()), syscall.LOCK_EX); err != nil {
			return
		}
		defer syscall.Flock(int(rf.Fd()), syscall.LOCK_UN)

		rf.Seek(0, 0)
		data, _ := io.ReadAll(rf)
		entries := parseAllocFile(data)
		var remaining []allocEntry
		for _, e := range entries {
			if e.pid != myPID || e.index != index {
				remaining = append(remaining, e)
			}
		}
		writeAllocFile(rf, remaining)
	}

	return gw, g, releaseFunc, nil
}

// tapName generates a unique TAP device name.
func tapName() string {
	var buf [4]byte
	rand.Read(buf[:])
	return fmt.Sprintf("warden-fc-%x", buf)
}
