package sandbox

import (
	"strings"

	"github.com/winler/warden/internal/config"
)

// buildExecArgs builds the argument list for docker sandbox exec.
func buildExecArgs(cfg config.SandboxConfig, sandboxName string, command []string, isTTY bool) []string {
	args := []string{"sandbox", "exec"}

	if isTTY {
		args = append(args, "-it")
	} else {
		args = append(args, "-i")
	}

	for _, e := range cfg.Env {
		args = append(args, "-e", e)
	}

	// Auth broker: set ANTHROPIC_BASE_URL to local bridge
	if cfg.AuthBroker != nil && cfg.AuthBroker.Enabled {
		args = append(args, "-e", "ANTHROPIC_BASE_URL=http://localhost:19280")
	}

	if cfg.Workdir != "" {
		args = append(args, "-w", cfg.Workdir)
	}

	args = append(args, sandboxName)
	args = append(args, command...)

	return args
}

// shellEscape quotes arguments for safe shell execution.
func shellEscape(args []string) string {
	var parts []string
	for _, a := range args {
		if strings.ContainsAny(a, " \t\n\"'\\$`!") {
			parts = append(parts, "'"+strings.ReplaceAll(a, "'", "'\\''")+"'")
		} else {
			parts = append(parts, a)
		}
	}
	return strings.Join(parts, " ")
}
