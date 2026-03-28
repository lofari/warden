package sandbox

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/winler/warden/internal/runtime"
)

// parseSandboxLsLine parses a line from docker sandbox ls output.
// Expected tab-delimited format: name\tstatus\tcreated
func parseSandboxLsLine(line string) (name, status string, created time.Time, err error) {
	parts := strings.SplitN(line, "\t", 3)
	if len(parts) != 3 {
		return "", "", time.Time{}, fmt.Errorf("sandbox ls: unexpected format %q", line)
	}
	name = strings.TrimSpace(parts[0])
	status = strings.TrimSpace(parts[1])
	created, err = time.Parse(time.RFC3339, strings.TrimSpace(parts[2]))
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("sandbox ls: parsing time %q: %w", parts[2], err)
	}
	return name, status, created, nil
}

// listSandboxes queries docker sandbox ls and returns running warden instances.
// Returns nil, nil if docker sandbox is not available.
func listSandboxes() ([]runtime.RunningInstance, error) {
	out, err := exec.Command("docker", "sandbox", "ls", "--format", "{{.Name}}\t{{.Status}}\t{{.CreatedAt}}").CombinedOutput()
	if err != nil {
		return nil, nil
	}
	output := strings.TrimSpace(string(out))
	if output == "" {
		return nil, nil
	}

	var instances []runtime.RunningInstance
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, status, created, err := parseSandboxLsLine(line)
		if err != nil {
			continue
		}
		// Only include warden-managed sandboxes
		if !strings.HasPrefix(name, "warden-") {
			continue
		}
		_ = status // future: filter by status
		instances = append(instances, runtime.RunningInstance{
			Name:    name,
			Runtime: "sandbox",
			Command: "",
			Started: created,
			CPU:     -1,
			Memory:  -1,
		})
	}
	return instances, nil
}
