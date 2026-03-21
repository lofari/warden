package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/winler/warden/internal/runtime"
)

func TestFormatTable(t *testing.T) {
	instances := []runtime.RunningInstance{
		{
			Name:    "warden-abc123",
			Runtime: "docker",
			Command: "bash",
			Started: time.Now().Add(-5 * time.Minute),
			CPU:     2.3,
			Memory:  128 * 1024 * 1024,
		},
		{
			Name:    "warden-fc-def456",
			Runtime: "firecracker",
			Command: "claude",
			Started: time.Now().Add(-12 * time.Minute),
			CPU:     -1,
			Memory:  -1,
		},
	}

	var buf bytes.Buffer
	formatTable(&buf, instances)
	output := buf.String()

	if !strings.Contains(output, "warden-abc123") {
		t.Error("missing container name")
	}
	if !strings.Contains(output, "docker") {
		t.Error("missing runtime")
	}
	if !strings.Contains(output, "2.3%") {
		t.Error("missing CPU%")
	}
	if !strings.Contains(output, "128 MiB") {
		t.Error("missing memory")
	}
	if !strings.Contains(output, "\u2014") {
		t.Error("missing em-dash for unavailable stats")
	}
}

func TestFormatTableEmpty(t *testing.T) {
	var buf bytes.Buffer
	formatTable(&buf, nil)
	if !strings.Contains(buf.String(), "No running warden sandboxes") {
		t.Error("expected empty message")
	}
}

func TestFormatJSON(t *testing.T) {
	instances := []runtime.RunningInstance{
		{
			Name:    "warden-test",
			Runtime: "docker",
			Command: "bash",
			Started: time.Date(2026, 3, 21, 10, 0, 0, 0, time.UTC),
			CPU:     1.5,
			Memory:  256 * 1024 * 1024,
		},
	}

	var buf bytes.Buffer
	formatJSON(&buf, instances)

	var result []map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}
	if result[0]["name"] != "warden-test" {
		t.Errorf("name = %v", result[0]["name"])
	}
	if result[0]["started"] == nil {
		t.Error("missing started field")
	}
	if result[0]["uptime"] == nil {
		t.Error("missing uptime field")
	}
}

func TestFormatJSONEmpty(t *testing.T) {
	var buf bytes.Buffer
	formatJSON(&buf, nil)
	if strings.TrimSpace(buf.String()) != "[]" {
		t.Errorf("expected [], got %q", buf.String())
	}
}
