package container

import (
	"fmt"
	"os/exec"
	"strings"
)

// DockerBinaryPath finds the docker binary in PATH.
func DockerBinaryPath() (string, error) {
	path, err := exec.LookPath("docker")
	if err != nil {
		return "", fmt.Errorf("warden: docker is not installed")
	}
	return path, nil
}

// CheckDockerReady verifies docker is installed and the daemon is running.
func CheckDockerReady() error {
	dockerPath, err := DockerBinaryPath()
	if err != nil {
		return err
	}
	out, err := exec.Command(dockerPath, "info").CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Cannot connect") || strings.Contains(string(out), "permission denied") {
			return fmt.Errorf("warden: docker daemon is not running")
		}
		return fmt.Errorf("warden: docker check failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}
