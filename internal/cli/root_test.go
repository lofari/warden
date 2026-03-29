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

func TestNetworkFlagsMutuallyExclusive(t *testing.T) {
	root := NewRootCommand()
	root.SetArgs([]string{"run", "--network", "--no-network", "--dry-run", "--", "echo", "hi"})
	err := root.Execute()
	if err == nil {
		t.Error("expected error when both --network and --no-network are set")
	}
}

func TestShellCommandExists(t *testing.T) {
	root := NewRootCommand()
	found := false
	for _, cmd := range root.Commands() {
		if cmd.Name() == "shell" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("root command must have a 'shell' subcommand")
	}
}

func TestExecCommandExists(t *testing.T) {
	root := NewRootCommand()
	found := false
	for _, cmd := range root.Commands() {
		if cmd.Name() == "exec" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("root command must have an 'exec' subcommand")
	}
}

func TestExecCommandRequiresArgs(t *testing.T) {
	root := NewRootCommand()
	root.SetArgs([]string{"exec"})
	err := root.Execute()
	if err == nil {
		t.Fatal("exec command with no args should return an error")
	}
}

func TestInfoCommandExists(t *testing.T) {
	root := NewRootCommand()
	found := false
	for _, cmd := range root.Commands() {
		if cmd.Name() == "info" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("root command must have an 'info' subcommand")
	}
}

func TestDisplayWithDockerReturnsError(t *testing.T) {
	root := NewRootCommand()
	root.SetArgs([]string{"run", "--display", "--runtime", "docker", "--dry-run", "--", "echo", "hi"})
	err := root.Execute()
	if err == nil {
		t.Error("expected error when --display used with docker runtime")
	}
}
