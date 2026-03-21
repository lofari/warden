package docker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/winler/warden/internal/config"
	"github.com/winler/warden/internal/runtime"
	"github.com/winler/warden/internal/runtime/shared"
)

// DockerRuntime implements runtime.Runtime using Docker containers.
type DockerRuntime struct{}

func init() {
	runtime.Register("docker", func() runtime.Runtime {
		return &DockerRuntime{}
	})
}

// containerName generates a unique container name.
func containerName() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		panic(fmt.Sprintf("warden: crypto/rand failed: %v", err))
	}
	return "warden-" + hex.EncodeToString(buf[:])
}

// Preflight verifies docker is installed and the daemon is running.
func (d *DockerRuntime) Preflight() error {
	path, err := exec.LookPath("docker")
	if err != nil {
		return fmt.Errorf("warden: docker is not installed")
	}
	out, err := exec.Command(path, "info").CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Cannot connect") || strings.Contains(string(out), "permission denied") {
			return fmt.Errorf("warden: docker daemon is not running")
		}
		return fmt.Errorf("warden: docker check failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// DryRun prints the docker command that would be executed.
func (d *DockerRuntime) DryRun(cfg config.SandboxConfig, command []string) error {
	cfg.Image = ImageTag(cfg.Image, cfg.Tools)
	args := buildArgs(cfg, command)
	name := containerName()
	extra := []string{"--name", name}
	fullArgs := make([]string, 0, len(args)+len(extra))
	fullArgs = append(fullArgs, args[0], args[1]) // "run", "--rm"
	fullArgs = append(fullArgs, extra...)
	fullArgs = append(fullArgs, args[2:]...)
	fmt.Println("docker " + joinArgs(fullArgs))
	return nil
}

// Run executes a command in a Docker container.
func (d *DockerRuntime) Run(cfg config.SandboxConfig, command []string) (int, error) {
	name := containerName()
	args := buildArgs(cfg, command)

	extra := []string{"--name", name}
	if shared.IsTerminal() {
		extra = append(extra, "-it")
	}
	fullArgs := make([]string, 0, len(args)+len(extra))
	fullArgs = append(fullArgs, args[0], args[1]) // "run", "--rm"
	fullArgs = append(fullArgs, extra...)
	fullArgs = append(fullArgs, args[2:]...)

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

	cmd := exec.Command("docker", fullArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	cleanup := shared.SignalHandler(
		func(sig os.Signal) {
			if cmd.Process != nil {
				cmd.Process.Signal(sig)
			}
		},
		func() {
			exec.Command("docker", "kill", name).Run()
		},
	)
	defer cleanup()

	if err := cmd.Start(); err != nil {
		return 1, fmt.Errorf("starting container: %w", err)
	}

	if timeout > 0 {
		go func() {
			<-ctx.Done()
			if ctx.Err() == context.DeadlineExceeded {
				exec.Command("docker", "stop", "--time", "10", name).Run()
			}
		}()
	}

	err = cmd.Wait()

	wasTimeout := timeout > 0 && ctx.Err() == context.DeadlineExceeded
	cancel()

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
		return 1, fmt.Errorf("running container: %w", err)
	}

	return 0, nil
}

// ListImages returns cached Docker warden images.
func (d *DockerRuntime) ListImages() ([]runtime.ImageInfo, error) {
	out, err := exec.Command("docker", "images", "--format",
		"{{.Repository}}:{{.Tag}}\t{{.Size}}\t{{.CreatedSince}}", "warden").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("listing images: %w", err)
	}
	output := strings.TrimSpace(string(out))
	if output == "" {
		return nil, nil
	}
	var images []runtime.ImageInfo
	for _, line := range strings.Split(output, "\n") {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) >= 1 {
			images = append(images, runtime.ImageInfo{
				Tag:     parts[0],
				Runtime: "docker",
			})
		}
	}
	return images, nil
}

// PruneImages removes all cached Docker warden images.
func (d *DockerRuntime) PruneImages() error {
	out, err := exec.Command("docker", "images", "--format",
		"{{.Repository}}:{{.Tag}}", "warden").CombinedOutput()
	if err != nil {
		return fmt.Errorf("listing images: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	removed := 0
	for _, img := range lines {
		img = strings.TrimSpace(img)
		if img == "" {
			continue
		}
		cmd := exec.Command("docker", "rmi", img)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if cmd.Run() == nil {
			removed++
		}
	}
	if removed > 0 {
		fmt.Printf("Removed %d warden image(s).\n", removed)
	} else {
		fmt.Println("No warden images to remove.")
	}
	return nil
}

func joinArgs(args []string) string {
	result := ""
	for i, a := range args {
		if i > 0 {
			result += " "
		}
		if strings.Contains(a, " ") || strings.Contains(a, "'") || strings.Contains(a, "\"") {
			result += "'" + strings.ReplaceAll(a, "'", "'\\''") + "'"
		} else {
			result += a
		}
	}
	return result
}

// ListRunning returns currently running sandboxes for this runtime.
// Returns nil, nil if the runtime is not available.
func (d *DockerRuntime) ListRunning() ([]runtime.RunningInstance, error) {
	return nil, nil
}
