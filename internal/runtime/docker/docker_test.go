package docker

import "testing"

func TestContainerName(t *testing.T) {
	name := containerName()
	if name == "" {
		t.Error("container name should not be empty")
	}
}
