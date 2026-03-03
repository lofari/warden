package cli

import (
	"testing"
)

func TestParseImageTag(t *testing.T) {
	tests := []struct {
		tag      string
		isWarden bool
	}{
		{"warden:ubuntu-24.04_node", true},
		{"warden:alpine-3.20_go_python", true},
		{"ubuntu:24.04", false},
		{"nginx:latest", false},
	}
	for _, tt := range tests {
		got := isWardenImage(tt.tag)
		if got != tt.isWarden {
			t.Errorf("isWardenImage(%q) = %v, want %v", tt.tag, got, tt.isWarden)
		}
	}
}
