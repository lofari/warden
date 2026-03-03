package container

import (
	"fmt"
	"time"
)

// ParseTimeout parses a human-friendly duration string.
func ParseTimeout(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid timeout %q: %w", s, err)
	}
	return d, nil
}

// ExitCodeMessage returns a human-readable message for special exit codes.
// Returns empty string for normal exit codes.
func ExitCodeMessage(code int, memory string) string {
	switch code {
	case 137:
		return fmt.Sprintf("warden: killed (out of memory, limit was %s)", memory)
	default:
		return ""
	}
}
