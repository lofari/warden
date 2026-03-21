package docker

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/winler/warden/internal/runtime"
)

// parseDockerPsLine parses a line from docker ps tab-delimited output.
// Expected format: name\t"command"\tstarted
func parseDockerPsLine(line string) (name, command string, started time.Time, err error) {
	parts := strings.SplitN(line, "\t", 3)
	if len(parts) != 3 {
		return "", "", time.Time{}, fmt.Errorf("ps: unexpected format %q", line)
	}
	name = parts[0]
	// Strip surrounding quotes from command
	cmd := strings.Trim(parts[1], "\"")
	// Take first word as the command name
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		command = ""
	} else {
		command = fields[0]
	}
	started, err = time.Parse("2006-01-02 15:04:05 -0700 MST", parts[2])
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("ps: parsing time %q: %w", parts[2], err)
	}
	return name, command, started, nil
}

// parseDockerStatsLine parses a line from docker stats tab-delimited output.
// Expected format: name\tcpu%\tmemUsed / memTotal
func parseDockerStatsLine(line string) (name string, cpu float64, memory int64, err error) {
	parts := strings.SplitN(line, "\t", 3)
	if len(parts) != 3 {
		return "", 0, 0, fmt.Errorf("stats: unexpected format %q", line)
	}
	name = parts[0]
	cpuStr := strings.TrimSuffix(strings.TrimSpace(parts[1]), "%")
	cpu, err = strconv.ParseFloat(cpuStr, 64)
	if err != nil {
		return "", 0, 0, fmt.Errorf("stats: parsing cpu %q: %w", cpuStr, err)
	}
	memParts := strings.SplitN(parts[2], " / ", 2)
	if len(memParts) < 1 {
		return "", 0, 0, fmt.Errorf("stats: unexpected memory format %q", parts[2])
	}
	memory, err = parseMemoryString(strings.TrimSpace(memParts[0]))
	if err != nil {
		return "", 0, 0, fmt.Errorf("stats: parsing memory %q: %w", memParts[0], err)
	}
	return name, cpu, memory, nil
}

// parseMemoryString converts a memory string like "128MiB" to bytes.
func parseMemoryString(s string) (int64, error) {
	s = strings.TrimSpace(s)
	units := []struct {
		suffix     string
		multiplier int64
	}{
		{"GiB", 1024 * 1024 * 1024},
		{"GB", 1000 * 1000 * 1000},
		{"MiB", 1024 * 1024},
		{"MB", 1000 * 1000},
		{"KiB", 1024},
		{"kB", 1000},
		{"B", 1},
	}
	for _, u := range units {
		if strings.HasSuffix(s, u.suffix) {
			numStr := strings.TrimSuffix(s, u.suffix)
			val, err := strconv.ParseFloat(strings.TrimSpace(numStr), 64)
			if err != nil {
				return 0, fmt.Errorf("parseMemoryString: invalid number in %q: %w", s, err)
			}
			return int64(val * float64(u.multiplier)), nil
		}
	}
	return 0, fmt.Errorf("parseMemoryString: unrecognized unit in %q", s)
}

// dockerStats holds CPU and memory stats for a container.
type dockerStats struct {
	CPU    float64
	Memory int64
}

// listDockerContainers queries docker ps and returns running warden instances.
// Returns nil, nil if docker is not found.
func listDockerContainers() ([]runtime.RunningInstance, error) {
	dockerPath, err := exec.LookPath("docker")
	if err != nil {
		return nil, nil
	}
	out, err := exec.Command(dockerPath, "ps",
		"--filter", "name=warden-",
		"--no-trunc",
		"--format", `{{.Names}}	{{.Command}}	{{.CreatedAt}}`,
	).Output()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w", err)
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
		name, cmd, started, err := parseDockerPsLine(line)
		if err != nil {
			continue
		}
		instances = append(instances, runtime.RunningInstance{
			Name:    name,
			Runtime: "docker",
			Command: cmd,
			Started: started,
			CPU:     -1,
			Memory:  -1,
		})
	}

	// Collect names for stats query
	var names []string
	for _, inst := range instances {
		names = append(names, inst.Name)
	}

	// Merge stats
	stats := fetchDockerStats(names)
	for i, inst := range instances {
		if s, ok := stats[inst.Name]; ok {
			instances[i].CPU = s.CPU
			instances[i].Memory = s.Memory
		}
	}

	return instances, nil
}

// fetchDockerStats runs docker stats --no-stream for the given container names
// and returns a map of container name -> stats.
func fetchDockerStats(names []string) map[string]dockerStats {
	if len(names) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	args := []string{"stats", "--no-stream", "--format", "{{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}"}
	args = append(args, names...)

	out, err := exec.CommandContext(ctx, "docker", args...).Output()
	if err != nil {
		return nil
	}

	result := map[string]dockerStats{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		name, cpu, mem, err := parseDockerStatsLine(line)
		if err != nil {
			continue
		}
		result[name] = dockerStats{CPU: cpu, Memory: mem}
	}
	return result
}
