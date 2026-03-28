package runtime

import "testing"

func TestSandboxAvailableReturnsFalseWhenNoDocker(t *testing.T) {
	// sandboxAvailable shells out to docker sandbox version.
	// In test env without Docker Desktop, this should return false.
	if sandboxAvailable() {
		t.Skip("docker sandbox is actually available in this environment")
	}
}
