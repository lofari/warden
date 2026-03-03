package cli

import (
	"testing"
)

func TestRootCommandHasRunSubcommand(t *testing.T) {
	root := NewRootCommand()
	found := false
	for _, cmd := range root.Commands() {
		if cmd.Name() == "run" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("root command must have a 'run' subcommand")
	}
}

func TestRunCommandRequiresArgs(t *testing.T) {
	root := NewRootCommand()
	root.SetArgs([]string{"run"})
	err := root.Execute()
	if err == nil {
		t.Fatal("run command with no args should return an error")
	}
}
