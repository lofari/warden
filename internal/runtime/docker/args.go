package docker

import (
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/winler/warden/internal/config"
)

// buildArgs translates a SandboxConfig into docker run arguments.
func buildArgs(cfg config.SandboxConfig, command []string, proxyDir string, authSetup *authBrokerSetup) []string {
	args := []string{"run", "--rm"}

	// Security hardening
	args = append(args,
		"--security-opt", "no-new-privileges",
		"--cap-drop", "ALL",
		"--pids-limit", "4096",
	)

	u, err := user.Current()
	if err == nil {
		args = append(args, "--user", u.Uid+":"+u.Gid)
	}

	if !cfg.Network {
		args = append(args, "--network", "none")
	}

	if cfg.Memory != "" {
		args = append(args, "--memory", cfg.Memory)
	}
	if cfg.CPUs > 0 {
		args = append(args, "--cpus", strconv.Itoa(cfg.CPUs))
	}

	for _, m := range cfg.Mounts {
		args = append(args, "-v", m.Path+":"+m.Path+":"+m.Mode)
	}

	for _, e := range cfg.Env {
		args = append(args, "-e", e)
	}

	// Proxy mounts: socket directory + shim binary per proxied command
	if proxyDir != "" {
		args = append(args, "-v", proxyDir+":/run/warden-proxy:ro")
		homeDir, _ := os.UserHomeDir()
		shimPath := filepath.Join(homeDir, ".warden", "bin", "warden-shim")
		for _, cmd := range cfg.Proxy {
			args = append(args, "-v", shimPath+":/usr/local/bin/"+cmd+":ro")
		}
	}

	// Auth broker mounts
	if authSetup != nil {
		homeDir, _ := os.UserHomeDir()

		// Writable tmpfs for home dir (container doesn't have /home/<user>)
		args = append(args, "--tmpfs", homeDir+":uid=1000,gid=1000")

		// Mount host's .claude dir and .claude.json for persistent settings
		args = append(args, "-v", homeDir+"/.claude:"+homeDir+"/.claude:rw")
		args = append(args, "-v", authSetup.fakePath+":"+homeDir+"/.claude/.credentials.json:ro")
		claudeJSON := homeDir + "/.claude.json"
		if _, err := os.Stat(claudeJSON); err == nil {
			args = append(args, "-v", claudeJSON+":"+claudeJSON+":rw")
		}

		// Mount proxy socket directory
		args = append(args, "-v", authSetup.dir+":/run/warden/auth:ro")

		// Mount bridge binary
		bridgePath := filepath.Join(homeDir, ".warden", "bin", "warden-bridge")
		args = append(args, "-v", bridgePath+":/usr/local/bin/warden-bridge:ro")

		// Set HOME so Claude finds credentials, and ANTHROPIC_BASE_URL for the broker
		args = append(args, "-e", "HOME="+homeDir)
		args = append(args, "-e", "ANTHROPIC_BASE_URL=http://localhost:19280")
	}

	if cfg.Workdir != "" {
		args = append(args, "-w", cfg.Workdir)
	}

	args = append(args, cfg.Image)

	if authSetup != nil {
		// Start bridge in background, poll for readiness, then exec user command
		args = append(args, "/bin/sh", "-c",
			"warden-bridge uds /run/warden/auth/proxy.sock & "+
				"i=0; while [ $i -lt 50 ]; do "+
				"if command -v nc >/dev/null 2>&1; then nc -z 127.0.0.1 19280 2>/dev/null && break; "+
				"else (echo >/dev/tcp/127.0.0.1/19280) 2>/dev/null && break; fi; "+
				"sleep 0.02; i=$((i+1)); done && "+
				"exec "+shellEscape(command))
	} else {
		args = append(args, command...)
	}
	return args
}

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
