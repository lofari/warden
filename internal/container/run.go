package container

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/winler/warden/internal/config"
)

const timeoutExitCode = 124

// ContainerName generates a unique container name.
func ContainerName() string {
	return fmt.Sprintf("warden-%d", rand.Int63())
}

// RunConfig holds everything needed to run a container.
type RunConfig struct {
	Sandbox config.SandboxConfig
	Command []string
	DryRun  bool
}

// Run executes the sandboxed command. Returns the exit code.
func Run(rc RunConfig) (int, error) {
	// Build docker args first (needed for both dry-run and real execution)
	resolved := rc.Sandbox

	if rc.DryRun {
		// Dry-run: just print the command without requiring Docker
		args := BuildDockerArgs(resolved, rc.Command)
		name := ContainerName()
		extra := []string{"--name", name}
		fullArgs := make([]string, 0, len(args)+len(extra))
		fullArgs = append(fullArgs, args[0], args[1]) // "run", "--rm"
		fullArgs = append(fullArgs, extra...)
		fullArgs = append(fullArgs, args[2:]...)
		fmt.Println("docker " + joinArgs(fullArgs))
		return 0, nil
	}

	// Check Docker is available
	if err := CheckDockerReady(); err != nil {
		return 1, err
	}

	// Resolve image (build if tools requested)
	image := resolved.Image
	if len(resolved.Tools) > 0 {
		built, err := BuildImage(resolved.Image, resolved.Tools)
		if err != nil {
			return 1, err
		}
		image = built
	}
	resolved.Image = image

	// Build docker args
	name := ContainerName()
	args := BuildDockerArgs(resolved, rc.Command)

	// Insert container name and TTY flags after "run" and "--rm"
	extra := []string{"--name", name}
	if isTerminal() {
		extra = append(extra, "-it")
	}
	fullArgs := make([]string, 0, len(args)+len(extra))
	fullArgs = append(fullArgs, args[0], args[1]) // "run", "--rm"
	fullArgs = append(fullArgs, extra...)
	fullArgs = append(fullArgs, args[2:]...)

	// Parse timeout
	timeout, err := ParseTimeout(resolved.Timeout)
	if err != nil {
		return 1, err
	}

	// Set up context with timeout
	ctx := context.Background()
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	// Run docker
	cmd := exec.CommandContext(ctx, "docker", fullArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Forward signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sigCount := 0
		for sig := range sigCh {
			sigCount++
			if sigCount >= 2 {
				// Force kill on second signal
				exec.Command("docker", "kill", name).Run()
				return
			}
			// Forward first signal — docker run in -it mode handles this
			if cmd.Process != nil {
				cmd.Process.Signal(sig)
			}
		}
	}()
	defer signal.Stop(sigCh)

	err = cmd.Run()

	// Handle timeout
	if ctx.Err() == context.DeadlineExceeded {
		// Kill the container
		exec.Command("docker", "kill", name).Run()
		fmt.Fprintf(os.Stderr, "warden: killed (timeout after %s)\n", resolved.Timeout)
		return timeoutExitCode, nil
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code := exitErr.ExitCode()
			if msg := ExitCodeMessage(code, resolved.Memory); msg != "" {
				fmt.Fprintln(os.Stderr, msg)
			}
			return code, nil
		}
		return 1, fmt.Errorf("running container: %w", err)
	}

	return 0, nil
}

func isTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func joinArgs(args []string) string {
	result := ""
	for i, a := range args {
		if i > 0 {
			result += " "
		}
		result += a
	}
	return result
}
