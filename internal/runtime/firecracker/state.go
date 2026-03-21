package firecracker

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type stateEntry struct {
	Name    string    `json:"name"`
	PID     int       `json:"pid"`
	Command string    `json:"command"`
	Started time.Time `json:"started"`
}

// flockReadWrite opens a file under flock, reads entries, applies a transform, and writes back.
// If transform is nil, performs read-only (still under flock).
// All I/O goes through the locked FD to avoid TOCTOU races.
func flockReadWrite(statePath string, create bool, transform func([]stateEntry) []stateEntry) ([]stateEntry, error) {
	if create {
		if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
			return nil, err
		}
	}
	flags := os.O_RDWR
	if create {
		flags |= os.O_CREATE
	}
	f, err := os.OpenFile(statePath, flags, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return nil, fmt.Errorf("locking state file: %w", err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

	// Read through the locked FD
	f.Seek(0, 0)
	data, _ := io.ReadAll(f)

	var entries []stateEntry
	if len(data) > 0 {
		json.Unmarshal(data, &entries)
	}

	if transform == nil {
		return entries, nil
	}

	result := transform(entries)

	// Write through the locked FD
	out, _ := json.Marshal(result)
	f.Seek(0, 0)
	f.Truncate(0)
	f.Write(out)

	return result, nil
}

func registerVM(statePath string, entry stateEntry) error {
	_, err := flockReadWrite(statePath, true, func(entries []stateEntry) []stateEntry {
		return append(entries, entry)
	})
	return err
}

func deregisterVM(statePath, name string) {
	flockReadWrite(statePath, false, func(entries []stateEntry) []stateEntry {
		var remaining []stateEntry
		for _, e := range entries {
			if e.Name != name {
				remaining = append(remaining, e)
			}
		}
		return remaining
	})
}

func readStateFile(statePath string) ([]stateEntry, error) {
	return flockReadWrite(statePath, false, nil)
}

func readAndReapStateFile(statePath string) ([]stateEntry, error) {
	return flockReadWrite(statePath, false, func(entries []stateEntry) []stateEntry {
		var alive []stateEntry
		for _, e := range entries {
			if isPIDAlive(e.PID) {
				alive = append(alive, e)
			}
		}
		return alive
	})
}

// parseProcStatCPU extracts utime and stime (fields 14, 15) from /proc/<pid>/stat.
func parseProcStatCPU(line string) (utime, stime uint64, err error) {
	closeParen := strings.LastIndex(line, ")")
	if closeParen < 0 {
		return 0, 0, fmt.Errorf("invalid /proc/stat line")
	}
	fields := strings.Fields(line[closeParen+2:])
	if len(fields) < 13 {
		return 0, 0, fmt.Errorf("too few fields in /proc/stat")
	}
	utime, err = strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return 0, 0, err
	}
	stime, err = strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return 0, 0, err
	}
	return utime, stime, nil
}

// parseProcStatmRSS extracts RSS (field 1) from /proc/<pid>/statm and returns bytes.
func parseProcStatmRSS(line string) (int64, error) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0, fmt.Errorf("too few fields in /proc/statm")
	}
	pages, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return 0, err
	}
	return pages * 4096, nil
}

// readProcStats reads CPU% and memory for a PID from /proc.
func readProcStats(pid int, started time.Time) (cpu float64, memory int64) {
	cpu = -1
	memory = -1

	statmPath := fmt.Sprintf("/proc/%d/statm", pid)
	if data, err := os.ReadFile(statmPath); err == nil {
		if rss, err := parseProcStatmRSS(strings.TrimSpace(string(data))); err == nil {
			memory = rss
		}
	}

	statPath := fmt.Sprintf("/proc/%d/stat", pid)
	if data, err := os.ReadFile(statPath); err == nil {
		if utime, stime, err := parseProcStatCPU(strings.TrimSpace(string(data))); err == nil {
			elapsed := time.Since(started).Seconds()
			if elapsed > 0 {
				ticksPerSec := float64(100)
				totalCPUSeconds := float64(utime+stime) / ticksPerSec
				cpu = (totalCPUSeconds / elapsed) * 100.0
			}
		}
	}

	return cpu, memory
}
