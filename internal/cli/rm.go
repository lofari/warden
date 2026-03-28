package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/winler/warden/internal/runtime"
)

func newRmCommand() *cobra.Command {
	var all bool

	rm := &cobra.Command{
		Use:   "rm [name]",
		Short: "Remove a warden sandbox",
		Long:  "Remove a sandbox. If no name given, derives it from the current directory. Use -a to remove all.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if all {
				return removeAllSandboxes()
			}

			name, err := resolveSandboxName(args)
			if err != nil {
				return err
			}

			for _, rtName := range runtime.AllRuntimes() {
				rt, err := runtime.NewRuntime(rtName)
				if err != nil {
					continue
				}
				if err := rt.Remove(name); err == nil {
					fmt.Fprintf(os.Stderr, "warden: removed %s\n", name)
					return nil
				}
			}
			return fmt.Errorf("warden: sandbox %q not found or could not be removed", name)
		},
	}

	rm.Flags().BoolVarP(&all, "all", "a", false, "Remove all warden sandboxes")
	return rm
}

func removeAllSandboxes() error {
	removed := 0
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
			if !strings.HasPrefix(inst.Name, "warden-") {
				continue
			}
			if err := rt.Stop(inst.Name); err != nil {
				fmt.Fprintf(os.Stderr, "warden: failed to stop %s: %v\n", inst.Name, err)
			}
			if err := rt.Remove(inst.Name); err != nil {
				fmt.Fprintf(os.Stderr, "warden: failed to remove %s: %v\n", inst.Name, err)
			} else {
				removed++
			}
		}
	}
	if removed > 0 {
		fmt.Fprintf(os.Stderr, "warden: removed %d sandbox(es)\n", removed)
	} else {
		fmt.Fprintln(os.Stderr, "warden: no sandboxes to remove")
	}
	return nil
}
