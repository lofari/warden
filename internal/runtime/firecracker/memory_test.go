package firecracker

import "testing"

func TestParseMemoryMiB(t *testing.T) {
	tests := []struct {
		input   string
		want    int
		wantErr bool
	}{
		{"", 1024, false},
		{"512m", 512, false},
		{"512M", 512, false},
		{"2g", 2048, false},
		{"2G", 2048, false},
		{"4096", 4096, false},
		{"1024m", 1024, false},
		{"0", 0, true},
		{"-1", 0, true},
		{"abc", 0, true},
		{"5x", 0, true},
	}
	for _, tc := range tests {
		got, err := parseMemoryMiB(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseMemoryMiB(%q) = %d, want error", tc.input, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseMemoryMiB(%q) error: %v", tc.input, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseMemoryMiB(%q) = %d, want %d", tc.input, got, tc.want)
		}
	}
}
