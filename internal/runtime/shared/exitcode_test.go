package shared

import "testing"

func TestExitCodeMessage137(t *testing.T) {
	msg := ExitCodeMessage(137, "8g")
	if msg == "" {
		t.Fatal("expected message for exit code 137")
	}
}

func TestExitCodeMessageNormal(t *testing.T) {
	msg := ExitCodeMessage(0, "8g")
	if msg != "" {
		t.Errorf("expected empty message for exit 0, got %q", msg)
	}
}
