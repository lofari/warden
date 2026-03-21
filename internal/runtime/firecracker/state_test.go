package firecracker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStateFileReadWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "running.json")
	entry := stateEntry{
		Name: "warden-fc-test1234", PID: os.Getpid(),
		Command: "bash", Started: time.Now().UTC().Truncate(time.Second),
	}
	if err := registerVM(path, entry); err != nil {
		t.Fatal(err)
	}
	entries, err := readStateFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "warden-fc-test1234" {
		t.Fatalf("unexpected entries: %v", entries)
	}
}

func TestStateFileReapDeadPIDs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "running.json")
	entries := []stateEntry{
		{Name: "warden-fc-dead", PID: 99999999, Command: "bash", Started: time.Now().UTC()},
		{Name: "warden-fc-alive", PID: os.Getpid(), Command: "zsh", Started: time.Now().UTC()},
	}
	data, _ := json.Marshal(entries)
	os.WriteFile(path, data, 0o644)

	alive, err := readAndReapStateFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(alive) != 1 || alive[0].Name != "warden-fc-alive" {
		t.Fatalf("expected 1 alive, got %v", alive)
	}
	// Verify file was rewritten
	raw, _ := os.ReadFile(path)
	var remaining []stateEntry
	json.Unmarshal(raw, &remaining)
	if len(remaining) != 1 {
		t.Errorf("file should have 1 entry, got %d", len(remaining))
	}
}

func TestDeregisterVM(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "running.json")
	registerVM(path, stateEntry{Name: "warden-fc-remove", PID: os.Getpid(), Command: "bash", Started: time.Now().UTC()})
	deregisterVM(path, "warden-fc-remove")
	entries, _ := readStateFile(path)
	if len(entries) != 0 {
		t.Errorf("expected 0, got %d", len(entries))
	}
}

func TestDeregisterMissingEntryIsNoop(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "running.json")
	deregisterVM(path, "warden-fc-nonexistent") // should not panic
}
