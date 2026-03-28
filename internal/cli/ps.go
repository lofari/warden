package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/winler/warden/internal/runtime"
	_ "github.com/winler/warden/internal/runtime/docker"
	_ "github.com/winler/warden/internal/runtime/firecracker"
	_ "github.com/winler/warden/internal/runtime/sandbox"
)

func newPsCommand() *cobra.Command {
	var jsonOutput bool

	ps := &cobra.Command{
		Use:   "ps",
		Short: "List running warden sandboxes",
		RunE: func(cmd *cobra.Command, args []string) error {
			var all []runtime.RunningInstance
			for _, name := range runtime.AllRuntimes() {
				rt, err := runtime.NewRuntime(name)
				if err != nil {
					continue
				}
				instances, err := rt.ListRunning()
				if err != nil {
					fmt.Fprintf(os.Stderr, "warden: %s: %v\n", name, err)
					continue
				}
				all = append(all, instances...)
			}

			if jsonOutput {
				formatJSON(os.Stdout, all)
			} else {
				formatTable(os.Stdout, all)
			}
			return nil
		},
	}

	ps.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")
	return ps
}

func formatTable(w io.Writer, instances []runtime.RunningInstance) {
	if len(instances) == 0 {
		fmt.Fprintln(w, "No running warden sandboxes.")
		return
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tRUNTIME\tCOMMAND\tCPU%\tMEMORY\tUPTIME")
	for _, inst := range instances {
		cpu := "\u2014"
		if inst.CPU >= 0 {
			cpu = fmt.Sprintf("%.1f%%", inst.CPU)
		}
		mem := "\u2014"
		if inst.Memory >= 0 {
			mem = formatBytes(inst.Memory)
		}
		uptime := formatDuration(time.Since(inst.Started))
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			inst.Name, inst.Runtime, inst.Command, cpu, mem, uptime)
	}
	tw.Flush()
}

type jsonInstance struct {
	Name    string  `json:"name"`
	Runtime string  `json:"runtime"`
	Command string  `json:"command"`
	CPU     float64 `json:"cpu"`
	Memory  int64   `json:"memory"`
	Started string  `json:"started"`
	Uptime  string  `json:"uptime"`
}

func formatJSON(w io.Writer, instances []runtime.RunningInstance) {
	if instances == nil {
		instances = []runtime.RunningInstance{}
	}
	out := make([]jsonInstance, len(instances))
	for i, inst := range instances {
		out[i] = jsonInstance{
			Name:    inst.Name,
			Runtime: inst.Runtime,
			Command: inst.Command,
			CPU:     inst.CPU,
			Memory:  inst.Memory,
			Started: inst.Started.UTC().Format(time.RFC3339),
			Uptime:  formatDuration(time.Since(inst.Started)),
		}
	}
	data, _ := json.MarshalIndent(out, "", "  ")
	fmt.Fprintln(w, string(data))
}

func formatBytes(b int64) string {
	const (
		mib = 1024 * 1024
		gib = 1024 * 1024 * 1024
	)
	switch {
	case b >= gib:
		return fmt.Sprintf("%.1f GiB", float64(b)/float64(gib))
	case b >= mib:
		return fmt.Sprintf("%d MiB", b/mib)
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func formatDuration(d time.Duration) string {
	d = d.Truncate(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}
