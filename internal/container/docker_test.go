package container

import (
	"testing"
)

func TestDockerBinaryPath(t *testing.T) {
	path, err := DockerBinaryPath()
	if err != nil {
		t.Skipf("docker not in PATH: %v", err)
	}
	if path == "" {
		t.Fatal("path should not be empty when err is nil")
	}
}
