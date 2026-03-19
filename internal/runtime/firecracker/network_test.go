package firecracker

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestSubnetForIndex(t *testing.T) {
	tests := []struct {
		index   uint32
		gateway string
		guest   string
	}{
		{0, "172.16.0.1/30", "172.16.0.2/30"},
		{1, "172.16.0.5/30", "172.16.0.6/30"},
		{2, "172.16.0.9/30", "172.16.0.10/30"},
		{64, "172.16.1.1/30", "172.16.1.2/30"},
	}
	for _, tc := range tests {
		gw, guest := subnetForIndex(tc.index)
		if gw != tc.gateway {
			t.Errorf("index %d: gateway = %q, want %q", tc.index, gw, tc.gateway)
		}
		if guest != tc.guest {
			t.Errorf("index %d: guest = %q, want %q", tc.index, guest, tc.guest)
		}
	}
}

func TestAllocateAndRelease(t *testing.T) {
	tmpDir := t.TempDir()
	allocFile := filepath.Join(tmpDir, "net-alloc")

	gw1, guest1, release1, err := allocateSubnet(allocFile)
	if err != nil {
		t.Fatalf("first alloc: %v", err)
	}
	if gw1 == "" || guest1 == "" {
		t.Fatal("empty allocation")
	}

	gw2, guest2, release2, err := allocateSubnet(allocFile)
	if err != nil {
		t.Fatalf("second alloc: %v", err)
	}
	if gw1 == gw2 {
		t.Errorf("duplicate allocation: %s", gw1)
	}

	// Release first, verify reclamation
	release1()

	// Third allocation should reclaim index 0 (released by release1)
	gw3, _, release3, err := allocateSubnet(allocFile)
	if err != nil {
		t.Fatalf("third alloc: %v", err)
	}
	if gw3 != gw1 {
		t.Errorf("expected reclaimed gw %s, got %s", gw1, gw3)
	}

	release2()
	release3()
	_ = guest1
	_ = guest2
}

func TestAllocateReclainsDeadPIDs(t *testing.T) {
	tmpDir := t.TempDir()
	allocFile := filepath.Join(tmpDir, "net-alloc")

	// Write an entry with a dead PID (PID 999999999 should not exist)
	os.WriteFile(allocFile, []byte("5:999999999\n"), 0o644)

	gw, _, release, err := allocateSubnet(allocFile)
	if err != nil {
		t.Fatalf("alloc: %v", err)
	}

	// Should reclaim index 5 from dead PID
	expectedGw, _ := subnetForIndex(5)
	if gw != expectedGw {
		t.Errorf("expected reclaimed gw %s, got %s", expectedGw, gw)
	}

	release()
}

func TestAllocateOldFormatMigration(t *testing.T) {
	tmpDir := t.TempDir()
	allocFile := filepath.Join(tmpDir, "net-alloc")

	// Write old 4-byte binary counter format
	os.WriteFile(allocFile, []byte{0x05, 0x00, 0x00, 0x00}, 0o644)

	// Should detect old format, reset, and allocate index 0
	gw, _, release, err := allocateSubnet(allocFile)
	if err != nil {
		t.Fatalf("alloc: %v", err)
	}
	expectedGw, _ := subnetForIndex(0)
	if gw != expectedGw {
		t.Errorf("expected gw %s after migration, got %s", expectedGw, gw)
	}

	// Verify file is now in new format (check before release removes the entry)
	data, _ := os.ReadFile(allocFile)
	expected := fmt.Sprintf("0:%d\n", os.Getpid())
	if string(data) != expected {
		t.Errorf("file content = %q, want %q", string(data), expected)
	}

	release()

	// After release, file should be empty (our entry removed)
	dataAfter, _ := os.ReadFile(allocFile)
	if len(dataAfter) != 0 {
		t.Errorf("file content after release = %q, want empty", dataAfter)
	}
}

func TestAllocateEmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	allocFile := filepath.Join(tmpDir, "net-alloc")

	gw, _, release, err := allocateSubnet(allocFile)
	if err != nil {
		t.Fatalf("alloc: %v", err)
	}
	expectedGw, _ := subnetForIndex(0)
	if gw != expectedGw {
		t.Errorf("expected gw %s, got %s", expectedGw, gw)
	}
	release()
}
