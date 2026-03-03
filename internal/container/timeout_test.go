package container

import (
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input string
		want  time.Duration
	}{
		{"30m", 30 * time.Minute},
		{"1h", 1 * time.Hour},
		{"2h30m", 2*time.Hour + 30*time.Minute},
		{"90s", 90 * time.Second},
		{"", 0},
	}
	for _, tt := range tests {
		got, err := ParseTimeout(tt.input)
		if err != nil {
			t.Errorf("ParseTimeout(%q) error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseTimeout(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestExitCodeMessage(t *testing.T) {
	tests := []struct {
		code   int
		memory string
		want   string
	}{
		{0, "8g", ""},
		{1, "8g", ""},
		{137, "8g", "warden: killed (out of memory, limit was 8g)"},
	}
	for _, tt := range tests {
		got := ExitCodeMessage(tt.code, tt.memory)
		if got != tt.want {
			t.Errorf("ExitCodeMessage(%d, %q) = %q, want %q", tt.code, tt.memory, got, tt.want)
		}
	}
}
