package firecracker

import (
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

	release1()
	release2()

	_ = guest1
	_ = guest2
}
