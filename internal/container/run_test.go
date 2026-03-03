package container

import (
	"strings"
	"testing"
)

func TestContainerName(t *testing.T) {
	name := ContainerName()
	if !strings.HasPrefix(name, "warden-") {
		t.Errorf("container name %q should start with warden-", name)
	}
}
