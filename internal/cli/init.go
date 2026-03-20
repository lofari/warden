package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

const initTemplate = `# Warden sandbox configuration
# Docs: https://github.com/winler/warden

default:
  runtime: docker  # or: firecracker
  image: ubuntu:24.04
  tools: []
  mounts:
    - path: .
      mode: rw
      # deny_extra:        # additional files to block (added to built-in defaults)
      #   - secrets/
      #   - "*.secret"
      # deny_override:     # replace built-in deny defaults entirely
      #   - .env
      # read_only:         # paths that are read-only within this rw mount
      #   - .git/hooks
      #   - .github/workflows
  network: false
  memory: 8g
`

func newInitCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Generate a starter .warden.yaml",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit()
		},
	}
}

func runInit() error {
	path := ".warden.yaml"
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("warden: %s already exists", path)
	}
	if err := os.WriteFile(path, []byte(initTemplate), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	fmt.Println("Created .warden.yaml")
	return nil
}
