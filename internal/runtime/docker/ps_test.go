package docker

import (
	"testing"
)

func TestParseDockerPsLine(t *testing.T) {
	line := "warden-a1b2c3d4\t\"bash -c echo\"\t2026-03-21 10:00:00 +0000 UTC"
	name, cmd, started, err := parseDockerPsLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "warden-a1b2c3d4" {
		t.Errorf("name = %q, want %q", name, "warden-a1b2c3d4")
	}
	if cmd != "bash" {
		t.Errorf("cmd = %q, want %q", cmd, "bash")
	}
	if started.Year() != 2026 {
		t.Errorf("started.Year() = %d, want 2026", started.Year())
	}
}

func TestParseDockerStatsLine(t *testing.T) {
	cases := []struct {
		name      string
		line      string
		wantName  string
		wantCPU   float64
		wantMem   int64
	}{
		{
			name:     "MiB",
			line:     "warden-abc\t1.50%\t128MiB / 2GiB",
			wantName: "warden-abc",
			wantCPU:  1.50,
			wantMem:  128 * 1024 * 1024,
		},
		{
			name:     "GiB",
			line:     "warden-def\t0.25%\t1GiB / 8GiB",
			wantName: "warden-def",
			wantCPU:  0.25,
			wantMem:  1024 * 1024 * 1024,
		},
		{
			name:     "MB",
			line:     "warden-ghi\t3.00%\t256MB / 4GB",
			wantName: "warden-ghi",
			wantCPU:  3.00,
			wantMem:  256 * 1000 * 1000,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			name, cpu, mem, err := parseDockerStatsLine(tc.line)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if name != tc.wantName {
				t.Errorf("name = %q, want %q", name, tc.wantName)
			}
			if cpu != tc.wantCPU {
				t.Errorf("cpu = %f, want %f", cpu, tc.wantCPU)
			}
			if mem != tc.wantMem {
				t.Errorf("mem = %d, want %d", mem, tc.wantMem)
			}
		})
	}
}

func TestParseMemoryString(t *testing.T) {
	cases := []struct {
		input string
		want  int64
	}{
		{"100B", 100},
		{"2kB", 2000},
		{"2KiB", 2048},
		{"10MB", 10 * 1000 * 1000},
		{"10MiB", 10 * 1024 * 1024},
		{"1GB", 1000 * 1000 * 1000},
		{"1GiB", 1024 * 1024 * 1024},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := parseMemoryString(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("parseMemoryString(%q) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

