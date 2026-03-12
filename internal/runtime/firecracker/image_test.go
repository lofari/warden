package firecracker

import "testing"

func TestRootfsFilename(t *testing.T) {
	tests := []struct {
		base  string
		tools []string
		want  string
	}{
		{"ubuntu:24.04", nil, "base-ubuntu-24.04.ext4"},
		{"ubuntu:24.04", []string{"node"}, "ubuntu-24.04_node.ext4"},
		{"ubuntu:24.04", []string{"python", "node"}, "ubuntu-24.04_node_python.ext4"},
	}
	for _, tc := range tests {
		got := RootfsFilename(tc.base, tc.tools)
		if got != tc.want {
			t.Errorf("RootfsFilename(%q, %v) = %q, want %q", tc.base, tc.tools, got, tc.want)
		}
	}
}

func TestRootfsPath(t *testing.T) {
	got := rootfsPath("/home/user", "ubuntu:24.04", []string{"node"})
	want := "/home/user/.warden/firecracker/rootfs/ubuntu-24.04_node.ext4"
	if got != want {
		t.Errorf("rootfsPath = %q, want %q", got, want)
	}
}
