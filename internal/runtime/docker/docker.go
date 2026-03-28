package docker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/winler/warden/internal/authbroker"
	"github.com/winler/warden/internal/config"
	"github.com/winler/warden/internal/proxy"
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
	args := buildArgs(cfg, command, "", nil)
	name := containerName()
	extra := []string{"--name", name}
	fullArgs := make([]string, 0, len(args)+len(extra))
	fullArgs = append(fullArgs, args[0], args[1]) // "run", "--rm"
	fullArgs = append(fullArgs, extra...)
	fullArgs = append(fullArgs, args[2:]...)
	fmt.Println("docker " + joinArgs(fullArgs))
	return nil
}

type authBrokerSetup struct {
	broker   *authbroker.Broker
	dir      string
	fakePath string
	sockPath string
}

func setupDockerAuthBroker(cfg *config.AuthBrokerConfig) (*authBrokerSetup, error) {
	credsPath := cfg.Credentials
	if credsPath == "" {
		homeDir, _ := os.UserHomeDir()
		credsPath = filepath.Join(homeDir, ".claude", ".credentials.json")
	} else if strings.HasPrefix(credsPath, "~/") {
		homeDir, _ := os.UserHomeDir()
		credsPath = filepath.Join(homeDir, credsPath[2:])
	}

	target := cfg.Target
	if target == "" {
		target = "api.anthropic.com"
	}

	store, err := authbroker.NewCredentialStore(credsPath)
	if err != nil {
		return nil, fmt.Errorf("reading credentials: %w", err)
	}

	fakeCreds, err := authbroker.GenerateFakeCredentials(store.RawJSON(), store.EnvelopeKey())
	if err != nil {
		return nil, fmt.Errorf("generating fake credentials: %w", err)
	}

	dir, err := os.MkdirTemp("", "warden-auth-*")
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}

	// Create a subdirectory for Claude's .claude/ home mount
	claudeDir := filepath.Join(dir, "claude-home")
	if err := os.Mkdir(claudeDir, 0o700); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	fakePath := filepath.Join(claudeDir, ".credentials.json")
	if err := os.WriteFile(fakePath, fakeCreds, 0o600); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}

	sockPath := filepath.Join(dir, "proxy.sock")
	l, err := net.Listen("unix", sockPath)
	if err != nil {
		os.RemoveAll(dir)
		return nil, fmt.Errorf("creating broker socket: %w", err)
	}

	homeDir, _ := os.UserHomeDir()
	bridgePath := filepath.Join(homeDir, ".warden", "bin", "warden-bridge")
	if _, err := os.Stat(bridgePath); err != nil {
		l.Close()
		os.RemoveAll(dir)
		return nil, fmt.Errorf("warden-bridge not found at %s. Build with: CGO_ENABLED=0 go build -o %s ./cmd/warden-bridge/", bridgePath, bridgePath)
	}

	broker := authbroker.NewBroker(store, "https://"+target, l, nil)
	go broker.Serve()

	return &authBrokerSetup{
		broker:   broker,
		dir:      dir,
		fakePath: fakePath,
		sockPath: sockPath,
	}, nil
}

func (a *authBrokerSetup) Close() {
	if a.broker != nil {
		a.broker.Close()
	}
	os.RemoveAll(a.dir)
}

// setupDockerProxy creates a temp directory with one Unix socket per proxied command.
func setupDockerProxy(proxyCmds []string) (string, []*proxy.Handler, error) {
	dir, err := os.MkdirTemp("", "warden-proxy-*")
	if err != nil {
		return "", nil, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		os.RemoveAll(dir)
		return "", nil, err
	}

	homeDir, _ := os.UserHomeDir()
	shimPath := filepath.Join(homeDir, ".warden", "bin", "warden-shim")
	if _, err := os.Stat(shimPath); err != nil {
		os.RemoveAll(dir)
		return "", nil, fmt.Errorf("warden-shim not found at %s. Build with: CGO_ENABLED=0 go build -o %s ./cmd/warden-shim/", shimPath, shimPath)
	}

	var handlers []*proxy.Handler
	for _, cmd := range proxyCmds {
		hostPath, err := exec.LookPath(cmd)
		if err != nil {
			for _, h := range handlers {
				h.Close()
			}
			os.RemoveAll(dir)
			return "", nil, fmt.Errorf("proxied command %q not found on host: %w", cmd, err)
		}

		sockPath := filepath.Join(dir, cmd+".sock")
		l, err := net.Listen("unix", sockPath)
		if err != nil {
			for _, h := range handlers {
				h.Close()
			}
			os.RemoveAll(dir)
			return "", nil, fmt.Errorf("creating socket for %q: %w", cmd, err)
		}

		handlers = append(handlers, &proxy.Handler{
			Command:  cmd,
			HostPath: hostPath,
			Listener: l,
		})
	}

	return dir, handlers, nil
}

// Run executes a command in a Docker container.
func (d *DockerRuntime) Run(cfg config.SandboxConfig, command []string) (int, error) {
	cfg.Image = ImageTag(cfg.Image, cfg.Tools)
	// Auth broker requires networking so Claude can reach the local bridge
	// and pass its connectivity check. The broker controls API access.
	if cfg.AuthBroker != nil && cfg.AuthBroker.Enabled {
		cfg.Network = true
	}
	name := containerName()

	// Set up proxy listeners if configured
	var handlers []*proxy.Handler
	var proxyDir string
	if len(cfg.Proxy) > 0 {
		var err error
		proxyDir, handlers, err = setupDockerProxy(cfg.Proxy)
		if err != nil {
			return 1, fmt.Errorf("proxy setup: %w", err)
		}
		defer func() {
			for _, h := range handlers {
				h.Close()
			}
			os.RemoveAll(proxyDir)
		}()
		for _, h := range handlers {
			go h.Serve()
		}
	}

	var authSetup *authBrokerSetup
	if cfg.AuthBroker != nil && cfg.AuthBroker.Enabled {
		var authErr error
		authSetup, authErr = setupDockerAuthBroker(cfg.AuthBroker)
		if authErr != nil {
			return 1, fmt.Errorf("auth broker setup: %w", authErr)
		}
		defer authSetup.Close()
	}

	args := buildArgs(cfg, command, proxyDir, authSetup)

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
	return listDockerContainers()
}
