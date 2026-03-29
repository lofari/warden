package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newShellCommand() *cobra.Command {
	var (
		runtimeFlag string
		tools       string
		network     bool
		noNetwork   bool
		authBroker  bool
		ephemeral   bool
	)

	shell := &cobra.Command{
		Use:   "shell",
		Short: "Drop into an interactive shell in the sandbox",
		Args:  cobra.NoArgs,
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

			return resolveAndRun(opts, []string{"/bin/bash"})
		},
	}

	shell.Flags().StringVar(&runtimeFlag, "runtime", "", "Runtime backend (docker, sandbox, or firecracker)")
	shell.Flags().StringVar(&tools, "tools", "", "Dev tools to install (comma-separated: node,python,go,rust,java)")
	shell.Flags().BoolVar(&network, "network", false, "Enable networking")
	shell.Flags().BoolVar(&noNetwork, "no-network", false, "Disable networking")
	shell.Flags().BoolVar(&authBroker, "auth-broker", false, "Enable auth broker for Claude API proxying")
	shell.Flags().BoolVar(&ephemeral, "ephemeral", false, "Remove sandbox after execution (sandbox runtime only)")

	return shell
}
