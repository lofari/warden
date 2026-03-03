package container

import (
	"testing"
)

func TestImageTag(t *testing.T) {
	tests := []struct {
		base  string
		tools []string
		want  string
	}{
		{"ubuntu:24.04", nil, "ubuntu:24.04"},
		{"ubuntu:24.04", []string{"node"}, "warden:ubuntu-24.04_node"},
		{"ubuntu:24.04", []string{"go", "node"}, "warden:ubuntu-24.04_go_node"},
		{"ubuntu:24.04", []string{"node", "go"}, "warden:ubuntu-24.04_go_node"}, // sorted
		{"alpine:3.20", []string{"python"}, "warden:alpine-3.20_python"},
	}
	for _, tt := range tests {
		got := ImageTag(tt.base, tt.tools)
		if got != tt.want {
			t.Errorf("ImageTag(%q, %v) = %q, want %q", tt.base, tt.tools, got, tt.want)
		}
	}
}
