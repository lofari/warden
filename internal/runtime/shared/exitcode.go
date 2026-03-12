package shared

import "fmt"

const TimeoutExitCode = 124

// ExitCodeMessage returns a human-readable message for special exit codes.
func ExitCodeMessage(code int, memory string) string {
	switch code {
	case 137:
		return fmt.Sprintf("warden: killed (out of memory, limit was %s)", memory)
	default:
		return ""
	}
}
