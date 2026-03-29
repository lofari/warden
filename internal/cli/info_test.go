package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/winler/warden/internal/runtime"
)

func TestFormatInfoText(t *testing.T) {
	inst := &runtime.RunningInstance{
		Name:    "warden-abc123",
		Runtime: "sandbox",
		Started: time.Now().Add(-2 * time.Hour),
	}
	var buf bytes.Buffer
	formatInfoText(&buf, inst, "/home/user/project", "warden:base-ubuntu-24.04", false)
	output := buf.String()

	if !strings.Contains(output, "warden-abc123") {
		t.Error("missing sandbox name")
	}
	if !strings.Contains(output, "sandbox") {
		t.Error("missing runtime")
	}
	if !strings.Contains(output, "running") {
		t.Error("missing status")
	}
	if !strings.Contains(output, "/home/user/project") {
		t.Error("missing workspace")
	}
	if !strings.Contains(output, "warden:base-ubuntu-24.04") {
		t.Error("missing template")
	}
	if !strings.Contains(output, "disabled") {
		t.Error("missing network status")
	}
}

func TestFormatInfoJSON(t *testing.T) {
	inst := &runtime.RunningInstance{
		Name:    "warden-abc123",
		Runtime: "sandbox",
		Started: time.Now().Add(-30 * time.Minute),
	}
	var buf bytes.Buffer
	formatInfoJSON(&buf, inst, "/home/user/project", "warden:base-ubuntu-24.04", true)
	output := buf.String()

	if !strings.Contains(output, `"name"`) {
		t.Error("missing name field in JSON")
	}
	if !strings.Contains(output, `"runtime"`) {
		t.Error("missing runtime field in JSON")
	}
	if !strings.Contains(output, `"network": true`) {
		t.Error("missing or incorrect network field")
	}
}

func TestFormatInfoNotFound(t *testing.T) {
	var buf bytes.Buffer
	formatInfoNotFound(&buf, "/home/user/project", false)
	output := buf.String()
	if !strings.Contains(output, "No sandbox found") {
		t.Error("missing not-found message")
	}
}
