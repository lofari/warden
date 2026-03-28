package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/winler/warden/internal/runtime"
	"github.com/winler/warden/internal/runtime/sandbox"
)

func newStopCommand() *cobra.Command {
	stop := &cobra.Command{
		Use:   "stop [name]",
		Short: "Stop a running warden sandbox",
		Long:  "Stop a running sandbox. If no name given, derives it from the current directory.",
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := resolveSandboxName(args)
			if err != nil {
				return err
			}

			// Try each runtime that supports Stop
			for _, rtName := range runtime.AllRuntimes() {
				rt, err := runtime.NewRuntime(rtName)
				if err != nil {
					continue
				}
				if err := rt.Stop(name); err == nil {
					fmt.Fprintf(os.Stderr, "warden: stopped %s\n", name)
					return nil
				}
			}
			return fmt.Errorf("warden: sandbox %q not found or could not be stopped", name)
		},
	}
	return stop
}

// resolveSandboxName returns the sandbox name from args or derives from cwd.
func resolveSandboxName(args []string) (string, error) {
	if len(args) > 0 {
		return args[0], nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("warden: cannot determine working directory: %w", err)
	}
	return sandbox.SandboxName(cwd), nil
}
