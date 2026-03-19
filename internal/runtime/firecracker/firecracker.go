package firecracker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/winler/warden/internal/config"
	"github.com/winler/warden/internal/protocol"
	"github.com/winler/warden/internal/runtime"
	"github.com/winler/warden/internal/runtime/shared"
)

// FirecrackerRuntime implements runtime.Runtime using Firecracker microVMs.
type FirecrackerRuntime struct{}

func init() {
	runtime.Register("firecracker", func() runtime.Runtime {
		return &FirecrackerRuntime{}
	})
}

// Preflight verifies /dev/kvm, firecracker binary, and virtiofsd are available.
func (f *FirecrackerRuntime) Preflight() error {
	if _, err := os.Stat("/dev/kvm"); err != nil {
		return fmt.Errorf("warden: /dev/kvm not accessible. Run 'warden setup firecracker'")
	}
	file, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("warden: /dev/kvm not accessible. Run 'warden setup firecracker'")
	}
	file.Close()

	homeDir, _ := os.UserHomeDir()
	fcPath := filepath.Join(homeDir, ".warden", "firecracker", "bin", "firecracker")
	if _, err := os.Stat(fcPath); err != nil {
		return fmt.Errorf("warden: firecracker not found. Run 'warden setup firecracker'")
	}
	vfsPath := filepath.Join(homeDir, ".warden", "firecracker", "bin", "virtiofsd")
	if _, err := os.Stat(vfsPath); err != nil {
		return fmt.Errorf("warden: virtiofsd not found. Run 'warden setup firecracker'")
	}
	return nil
}

// Run executes a command in a Firecracker microVM.
func (f *FirecrackerRuntime) Run(cfg config.SandboxConfig, command []string) (int, error) {
	vm, err := startVM(cfg, command)
	if err != nil {
		return 1, err
	}
	defer vm.cleanup()

	timeout, err := shared.ParseTimeout(cfg.Timeout)
	if err != nil {
		return 1, err
	}

	// Connect to guest agent via vsock UDS
	conn, err := dialGuest(vm.vsockPath, 1024, 5*time.Second)
	if err != nil {
		return 1, err
	}
	defer conn.Close()

	// Send ExecMessage
	execMsg := &protocol.ExecMessage{
		Command: command[0],
		Workdir: cfg.Workdir,
		Env:     cfg.Env,
	}
	if len(command) > 1 {
		execMsg.Args = command[1:]
	}
	if err := protocol.WriteMessage(conn, execMsg); err != nil {
		return 1, fmt.Errorf("sending exec message: %w", err)
	}

	// Set up signal handling
	var mu sync.Mutex
	writeSignal := func(sigName string) {
		mu.Lock()
		defer mu.Unlock()
		protocol.WriteMessage(conn, &protocol.SignalMessage{Signal: sigName})
	}

	// Map os.Signal to protocol signal names
	signalName := func(sig os.Signal) string {
		switch sig {
		case syscall.SIGTERM:
			return "SIGTERM"
		case syscall.SIGINT:
			return "SIGINT"
		case syscall.SIGKILL:
			return "SIGKILL"
		default:
			return sig.String()
		}
	}

	cleanup := shared.SignalHandler(
		func(sig os.Signal) {
			writeSignal(signalName(sig))
		},
		func() {
			if vm.cmd != nil && vm.cmd.Process != nil {
				vm.cmd.Process.Kill()
			}
		},
	)
	defer cleanup()

	// Timeout watchdog
	timedOut := false
	if timeout > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		go func() {
			<-ctx.Done()
			if ctx.Err() == context.DeadlineExceeded {
				timedOut = true
				fmt.Fprintf(os.Stderr, "warden: killed (timeout after %s)\n", cfg.Timeout)
				writeSignal("SIGTERM")
				time.Sleep(10 * time.Second)
				if vm.cmd != nil && vm.cmd.Process != nil {
					vm.cmd.Process.Kill()
				}
			}
		}()
	}

	// Read loop: dispatch Output and Exit messages
	exitCode := 0
	for {
		raw, err := protocol.ReadMessage(conn)
		if err != nil {
			// Connection closed or error — VM likely died
			if timedOut {
				return shared.TimeoutExitCode, nil
			}
			return 1, fmt.Errorf("reading from guest: %w", err)
		}
		switch msg := raw.(type) {
		case *protocol.OutputMessage:
			decoded, err := base64.StdEncoding.DecodeString(msg.Data)
			if err != nil {
				continue
			}
			if msg.Type == "stdout" {
				os.Stdout.Write(decoded)
			} else {
				os.Stderr.Write(decoded)
			}
		case *protocol.ExitMessage:
			exitCode = msg.Code
			if timedOut {
				return shared.TimeoutExitCode, nil
			}
			if m := shared.ExitCodeMessage(exitCode, cfg.Memory); m != "" {
				fmt.Fprintln(os.Stderr, m)
			}
			return exitCode, nil
		}
	}
}

// dialGuest connects to the guest agent via the vsock UDS.
// Polls every 10ms until connection succeeds or timeout.
func dialGuest(vsockUDS string, port uint32, timeout time.Duration) (net.Conn, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.Dial("unix", vsockUDS)
		if err == nil {
			// Send connect request for the vsock port
			// Firecracker's vsock UDS expects "CONNECT <port>\n"
			fmt.Fprintf(conn, "CONNECT %d\n", port)
			buf := make([]byte, 64)
			n, err := conn.Read(buf)
			if err == nil && n > 0 && string(buf[:2]) == "OK" {
				return conn, nil
			}
			conn.Close()
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil, fmt.Errorf("warden: guest agent did not start within %s", timeout)
}

// DryRun prints the VM configuration.
func (f *FirecrackerRuntime) DryRun(cfg config.SandboxConfig, command []string) error {
	homeDir, _ := os.UserHomeDir()
	kernelPath := defaultKernelPath(homeDir)
	rootfs := rootfsPath(homeDir, cfg.Image, cfg.Tools)

	vmConfig := map[string]interface{}{
		"runtime": "firecracker",
		"kernel":  kernelPath,
		"rootfs":  rootfs,
		"vcpus":   cfg.CPUs,
		"memory":  cfg.Memory,
		"network": cfg.Network,
		"mounts":  cfg.Mounts,
		"workdir": cfg.Workdir,
		"command": command,
	}

	data, _ := json.MarshalIndent(vmConfig, "", "  ")
	fmt.Println(string(data))
	return nil
}
