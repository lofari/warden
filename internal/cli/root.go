package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "warden",
		Short: "Secure sandbox for AI coding agents",
	}

	run := &cobra.Command{
		Use:   "run -- <command...>",
		Short: "Run a command in a sandboxed container",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("not implemented")
			return nil
		},
	}

	root.AddCommand(run)
	return root
}
