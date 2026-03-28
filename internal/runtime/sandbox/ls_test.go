package sandbox

import (
	"testing"
	"time"
)

func TestParseSandboxLsLine(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		wantN   string
		wantS   string
		wantErr bool
	}{
		{
			name:  "running sandbox",
			line:  "warden-a1b2c3d4e5f6\trunning\t2026-03-28T10:00:00Z",
			wantN: "warden-a1b2c3d4e5f6",
			wantS: "running",
		},
		{
			name:  "stopped sandbox",
			line:  "warden-deadbeef1234\tstopped\t2026-03-27T08:30:00Z",
			wantN: "warden-deadbeef1234",
			wantS: "stopped",
		},
		{
			name:    "malformed line",
			line:    "incomplete",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			name, status, _, err := parseSandboxLsLine(tc.line)
			if tc.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if name != tc.wantN {
				t.Errorf("name: got %q, want %q", name, tc.wantN)
			}
			if status != tc.wantS {
				t.Errorf("status: got %q, want %q", status, tc.wantS)
			}
		})
	}
}

func TestParseSandboxLsLineTime(t *testing.T) {
	_, _, created, err := parseSandboxLsLine("warden-abc123\trunning\t2026-03-28T10:00:00Z")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := time.Date(2026, 3, 28, 10, 0, 0, 0, time.UTC)
	if !created.Equal(expected) {
		t.Errorf("created: got %v, want %v", created, expected)
	}
}
