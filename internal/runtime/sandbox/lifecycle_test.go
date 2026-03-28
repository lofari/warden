package sandbox

import (
	"strings"
	"testing"
)

func TestSandboxNameDeterministic(t *testing.T) {
	name1 := SandboxName("/home/user/project")
	name2 := SandboxName("/home/user/project")
	if name1 != name2 {
		t.Errorf("expected deterministic names, got %q and %q", name1, name2)
	}
}

func TestSandboxNamePrefix(t *testing.T) {
	name := SandboxName("/home/user/project")
	if !strings.HasPrefix(name, "warden-") {
		t.Errorf("expected warden- prefix, got %q", name)
	}
}

func TestSandboxNameLength(t *testing.T) {
	name := SandboxName("/home/user/project")
	// "warden-" (7) + 12 hex chars = 19
	if len(name) != 19 {
		t.Errorf("expected length 19, got %d for %q", len(name), name)
	}
}

func TestSandboxNameDifferentPaths(t *testing.T) {
	name1 := SandboxName("/home/user/project-a")
	name2 := SandboxName("/home/user/project-b")
	if name1 == name2 {
		t.Errorf("different paths should produce different names, both got %q", name1)
	}
}

func TestSandboxNameHexChars(t *testing.T) {
	name := SandboxName("/home/user/project")
	suffix := strings.TrimPrefix(name, "warden-")
	for _, c := range suffix {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("expected hex chars, got %c in %q", c, suffix)
		}
	}
}
