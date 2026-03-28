package sandbox

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os/exec"
	"strings"
)

// SandboxName returns a deterministic sandbox name for a workspace path.
// Format: warden-<sha256(path)[:12]>
// Exported so CLI commands (stop, rm) can derive names without duplicating logic.
func SandboxName(workdir string) string {
	h := sha256.Sum256([]byte(workdir))
	return "warden-" + hex.EncodeToString(h[:6])
}

// sandboxExists checks if a Docker sandbox with the given name exists.
func sandboxExists(name string) bool {
	cmd := exec.Command("docker", "sandbox", "inspect", name)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil
}

// ensureSandbox creates a sandbox if it doesn't already exist.
// Uses the shell agent type since warden manages its own commands.
func ensureSandbox(name, imageTag, workdir string) error {
	if sandboxExists(name) {
		return nil
	}
	args := []string{"sandbox", "create", "--template", imageTag, "--name", name, "shell", workdir}
	cmd := exec.Command("docker", args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("creating sandbox: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// inspectTemplate returns the template image used to create a sandbox.
// Returns empty string if the sandbox doesn't exist or template can't be determined.
func inspectTemplate(name string) string {
	out, err := exec.Command("docker", "sandbox", "inspect", name).CombinedOutput()
	if err != nil {
		return ""
	}
	// Parse inspect output for template field
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Template:") || strings.HasPrefix(line, "template:") {
			return strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
		}
	}
	return ""
}

// removeSandbox removes a sandbox by name.
func removeSandbox(name string) error {
	out, err := exec.Command("docker", "sandbox", "rm", name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("removing sandbox: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// stopSandbox stops a running sandbox by name.
func stopSandbox(name string) error {
	out, err := exec.Command("docker", "sandbox", "stop", name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("stopping sandbox: %s", strings.TrimSpace(string(out)))
	}
	return nil
}
