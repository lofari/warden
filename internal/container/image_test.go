package container

import (
	"testing"
)

func TestImageTagNoToolsReturnsBase(t *testing.T) {
	got := ImageTag("ubuntu:24.04", nil)
	if got != "warden:base-ubuntu-24.04" {
		t.Errorf("ImageTag with no tools = %q, want %q", got, "warden:base-ubuntu-24.04")
	}
}

func TestImageTag(t *testing.T) {
	tests := []struct {
		base  string
		tools []string
		want  string
	}{
		{"ubuntu:24.04", nil, "warden:base-ubuntu-24.04"},
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
