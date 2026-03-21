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

// Silence unused import warnings — strconv and strings are used in Task 5 additions.
var _ = strconv.Itoa
var _ = strings.TrimSpace

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
