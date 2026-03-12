package shared

import (
	"testing"
	"time"
)

func TestParseTimeout(t *testing.T) {
	tests := []struct {
		input string
		want  time.Duration
	}{
		{"", 0},
		{"30m", 30 * time.Minute},
		{"1h", time.Hour},
		{"2h30m", 2*time.Hour + 30*time.Minute},
	}
	for _, tc := range tests {
		got, err := ParseTimeout(tc.input)
		if err != nil {
			t.Errorf("ParseTimeout(%q) error: %v", tc.input, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseTimeout(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestParseTimeoutInvalid(t *testing.T) {
	_, err := ParseTimeout("abc")
	if err == nil {
		t.Fatal("expected error for invalid timeout")
	}
}
