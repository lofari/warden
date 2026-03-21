package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func (c SandboxConfig) Validate() error {
	if c.Memory != "" {
		mem := strings.TrimSpace(c.Memory)
		if mem == "" {
			return fmt.Errorf("invalid memory value %q", c.Memory)
		}
		last := strings.ToLower(mem[len(mem)-1:])
		numStr := mem
		if last == "g" || last == "m" {
			numStr = mem[:len(mem)-1]
		}
		if _, err := strconv.Atoi(numStr); err != nil {
			return fmt.Errorf("invalid memory value %q", c.Memory)
		}
	}
	if c.CPUs < 0 {
		return fmt.Errorf("cpus must be non-negative, got %d", c.CPUs)
	}
	if c.Timeout != "" {
		if _, err := time.ParseDuration(c.Timeout); err != nil {
			return fmt.Errorf("invalid timeout %q: %w", c.Timeout, err)
		}
	}
	for _, m := range c.Mounts {
		if m.Mode != "" && m.Mode != "ro" && m.Mode != "rw" {
			return fmt.Errorf("invalid mount mode %q for %s (must be ro or rw)", m.Mode, m.Path)
		}
	}
	if c.Runtime != "" && c.Runtime != "docker" && c.Runtime != "firecracker" {
		return fmt.Errorf("unknown runtime %q", c.Runtime)
	}
	if c.Image != "" {
		if strings.ContainsAny(c.Image, " \t\n\r") {
			return fmt.Errorf("invalid image name %q: contains whitespace", c.Image)
		}
	}
	return nil
}
