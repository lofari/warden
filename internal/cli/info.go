package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/winler/warden/internal/runtime"
	docker "github.com/winler/warden/internal/runtime/docker"
	"github.com/winler/warden/internal/runtime/sandbox"
)

func newInfoCommand() *cobra.Command {
	var jsonOutput bool

	info := &cobra.Command{
		Use:   "info",
		Short: "Show sandbox status for the current workspace",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("warden: cannot determine working directory: %w", err)
			}

			name := sandbox.SandboxName(cwd)

			// Find matching instance across all runtimes
			var found *runtime.RunningInstance
			for _, rtName := range runtime.AllRuntimes() {
				rt, err := runtime.NewRuntime(rtName)
				if err != nil {
					continue
				}
				instances, err := rt.ListRunning()
				if err != nil {
					continue
				}
				for _, inst := range instances {
					if inst.Name == name {
						found = &inst
						break
					}
				}
				if found != nil {
					break
				}
			}

			// Determine template and network from config
			template := ""
			networkEnabled := false
			cfg, err := resolveConfig(resolveOptions{})
			if err == nil {
				template = docker.ImageTag(cfg.Image, cfg.Tools)
				networkEnabled = cfg.Network
			}

			if found == nil {
				formatInfoNotFound(os.Stdout, cwd, jsonOutput)
				os.Exit(1)
			}

			if jsonOutput {
				formatInfoJSON(os.Stdout, found, cwd, template, networkEnabled)
			} else {
				formatInfoText(os.Stdout, found, cwd, template, networkEnabled)
			}
			return nil
		},
	}

	info.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")
	return info
}

func formatInfoText(w io.Writer, inst *runtime.RunningInstance, workspace, template string, network bool) {
	netStr := "disabled"
	if network {
		netStr = "enabled"
	}
	uptime := formatDuration(time.Since(inst.Started))
	fmt.Fprintf(w, "Sandbox:   %s\n", inst.Name)
	fmt.Fprintf(w, "Runtime:   %s\n", inst.Runtime)
	fmt.Fprintf(w, "Status:    running\n")
	fmt.Fprintf(w, "Template:  %s\n", template)
	fmt.Fprintf(w, "Workspace: %s\n", workspace)
	fmt.Fprintf(w, "Uptime:    %s\n", uptime)
	fmt.Fprintf(w, "Network:   %s\n", netStr)
}

type infoJSON struct {
	Name      string `json:"name"`
	Runtime   string `json:"runtime"`
	Status    string `json:"status"`
	Template  string `json:"template"`
	Workspace string `json:"workspace"`
	Uptime    string `json:"uptime"`
	Network   bool   `json:"network"`
}

func formatInfoJSON(w io.Writer, inst *runtime.RunningInstance, workspace, template string, network bool) {
	uptime := formatDuration(time.Since(inst.Started))
	data, _ := json.MarshalIndent(infoJSON{
		Name:      inst.Name,
		Runtime:   inst.Runtime,
		Status:    "running",
		Template:  template,
		Workspace: workspace,
		Uptime:    uptime,
		Network:   network,
	}, "", "  ")
	fmt.Fprintln(w, string(data))
}

func formatInfoNotFound(w io.Writer, workspace string, jsonOutput bool) {
	if jsonOutput {
		data, _ := json.MarshalIndent(map[string]string{
			"status":    "not_found",
			"workspace": workspace,
		}, "", "  ")
		fmt.Fprintln(w, string(data))
	} else {
		fmt.Fprintf(w, "No sandbox found for %s\n", workspace)
	}
}
