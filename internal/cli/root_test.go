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

func TestRunFlagParsing(t *testing.T) {
	root := NewRootCommand()
	root.SetArgs([]string{
		"run",
		"--mount", "/tmp:rw",
		"--no-network",
		"--memory", "4g",
		"--cpus", "2",
		"--timeout", "30m",
		"--image", "alpine:3.20",
		"--dry-run",
		"--", "echo", "hello",
	})
	err := root.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
