package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/winler/warden/internal/config"
	"github.com/winler/warden/internal/runtime/shared"
)

// Run executes a command inside a Docker sandbox.
func (s *SandboxRuntime) Run(cfg config.SandboxConfig, command []string) (int, error) {
	// Auth broker requires networking
	if cfg.AuthBroker != nil && cfg.AuthBroker.Enabled {
		cfg.Network = true
	}

	// Determine workspace and sandbox name
	workdir := cfg.Workdir
	if workdir == "" {
		for _, m := range cfg.Mounts {
			if m.Mode == "rw" {
				workdir = m.Path
				break
			}
		}
	}
	name := SandboxName(workdir)

	// Build exec args
	isTTY := shared.IsTerminal()
	args := buildExecArgs(cfg, name, command, isTTY)

	// Parse timeout
	timeout, err := shared.ParseTimeout(cfg.Timeout)
	if err != nil {
		return 1, err
	}

	ctx := context.Background()
	var cancel context.CancelFunc = func() {}
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	cmd := exec.Command("docker", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Signal handling: forward first, force-stop on second
	cleanup := shared.SignalHandler(
		func(sig os.Signal) {
			if cmd.Process != nil {
				cmd.Process.Signal(sig)
			}
		},
		func() {
			exec.Command("docker", "sandbox", "stop", name).Run()
		},
	)
	defer cleanup()

	if err := cmd.Start(); err != nil {
		return 1, fmt.Errorf("starting sandbox exec: %w", err)
	}

	// Timeout watchdog
	if timeout > 0 {
		go func() {
			<-ctx.Done()
			if ctx.Err() == context.DeadlineExceeded {
				exec.Command("docker", "sandbox", "stop", name).Run()
			}
		}()
	}

	err = cmd.Wait()

	wasTimeout := timeout > 0 && ctx.Err() == context.DeadlineExceeded
	cancel()

	// Ephemeral mode: remove sandbox after exec
	if cfg.Ephemeral {
		removeSandbox(name)
	}

	if wasTimeout {
		fmt.Fprintf(os.Stderr, "warden: killed (timeout after %s)\n", cfg.Timeout)
		return shared.TimeoutExitCode, nil
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code := exitErr.ExitCode()
			if msg := shared.ExitCodeMessage(code, cfg.Memory); msg != "" {
				fmt.Fprintln(os.Stderr, msg)
			}
			return code, nil
		}
		return 1, fmt.Errorf("sandbox exec: %w", err)
	}

	return 0, nil
}

// Stop halts a running sandbox.
func (s *SandboxRuntime) Stop(name string) error {
	return stopSandbox(name)
}

// Remove deletes a sandbox.
func (s *SandboxRuntime) Remove(name string) error {
	return removeSandbox(name)
}
