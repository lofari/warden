package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newExecCommand() *cobra.Command {
	var (
		runtimeFlag string
		tools       string
		network     bool
		noNetwork   bool
		authBroker  bool
		ephemeral   bool
	)

	exec := &cobra.Command{
		Use:   "exec [flags] <command> [args...]",
		Short: "Run a command in the sandbox",
		Long:  "Run a one-off command in the sandbox for the current workspace. No -- separator needed.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var net *bool
			if cmd.Flags().Changed("network") && cmd.Flags().Changed("no-network") {
				return fmt.Errorf("--network and --no-network are mutually exclusive")
			}
			if cmd.Flags().Changed("network") {
				v := true
				net = &v
			}
			if cmd.Flags().Changed("no-network") {
				v := false
				net = &v
			}

			opts := resolveOptions{
				network: net,
			}
			if cmd.Flags().Changed("runtime") {
				opts.runtime = runtimeFlag
			}
			if cmd.Flags().Changed("tools") {
				opts.tools = tools
			}
			if cmd.Flags().Changed("auth-broker") && authBroker {
				opts.authBroker = true
			}
			if cmd.Flags().Changed("ephemeral") {
				opts.ephemeral = ephemeral
			}

			return resolveAndRun(opts, args)
		},
	}

	exec.Flags().StringVar(&runtimeFlag, "runtime", "", "Runtime backend (docker, sandbox, or firecracker)")
	exec.Flags().StringVar(&tools, "tools", "", "Dev tools to install (comma-separated: node,python,go,rust,java)")
	exec.Flags().BoolVar(&network, "network", false, "Enable networking")
	exec.Flags().BoolVar(&noNetwork, "no-network", false, "Disable networking")
	exec.Flags().BoolVar(&authBroker, "auth-broker", false, "Enable auth broker for Claude API proxying")
	exec.Flags().BoolVar(&ephemeral, "ephemeral", false, "Remove sandbox after execution (sandbox runtime only)")

	return exec
}
