package firecracker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/winler/warden/internal/config"
	"github.com/winler/warden/internal/fileserver"
	"github.com/winler/warden/internal/protocol"
	"github.com/winler/warden/internal/runtime"
	"github.com/winler/warden/internal/runtime/shared"
	"golang.org/x/term"
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

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("warden: cannot determine home directory: %w", err)
	}
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

	// Set raw terminal mode if we have a TTY
	if shared.IsTerminal() {
		oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
		if err == nil {
			defer term.Restore(int(os.Stdin.Fd()), oldState)
		}
	}

	// Connect to guest agent via vsock UDS
	conn, err := dialGuest(vm.vsockPath, 1024, 5*time.Second)
	if err != nil {
		return 1, err
	}
	defer conn.Close()

	// Send NetworkConfigMessage if networking is enabled
	if cfg.Network && vm.gatewayIP != "" {
		gwIP := strings.Split(vm.gatewayIP, "/")[0]
		netMsg := &protocol.NetworkConfigMessage{
			GuestIP: vm.guestIP,
			Gateway: gwIP,
			DNS:     "8.8.8.8",
		}
		if err := protocol.WriteMessage(conn, netMsg); err != nil {
			return 1, fmt.Errorf("sending network config: %w", err)
		}
	}

	// Set up file sharing for mounts
	if len(cfg.Mounts) > 0 {
		var mountInfos []protocol.MountInfo
		for i, m := range cfg.Mounts {
			mountInfos = append(mountInfos, protocol.MountInfo{
				GuestPath: m.Path,
				VsockPort: uint32(1025 + i),
				Mode:      m.Mode,
			})
		}

		if err := protocol.WriteMessage(conn, &protocol.MountConfigMessage{Mounts: mountInfos}); err != nil {
			return 1, fmt.Errorf("sending mount config: %w", err)
		}

		// Wait for guest to signal ready
		raw, err := protocol.ReadMessage(conn)
		if err != nil {
			return 1, fmt.Errorf("waiting for mounts ready: %w", err)
		}
		if _, ok := raw.(*protocol.MountsReadyMessage); !ok {
			return 1, fmt.Errorf("expected MountsReadyMessage, got %T", raw)
		}

		// Connect to each mount port and start file servers
		for i, m := range cfg.Mounts {
			port := uint32(1025 + i)
			go func(mountPath string, p uint32, m config.Mount) {
				fsConn, err := dialGuest(vm.vsockPath, p, 10*time.Second)
				if err != nil {
					fmt.Fprintf(os.Stderr, "warden: file server for %s failed: %v\n", mountPath, err)
					return
				}
				defer fsConn.Close()
				readOnly := m.Mode == "ro"
				ac := fileserver.NewAccessControl(m.DenyExtra, m.DenyOverride, m.ReadOnly)
				srv := fileserver.NewServer(mountPath, readOnly, ac)
				srv.Serve(fsConn)
			}(m.Path, port, m)
		}
	}

	// Send ExecMessage
	execMsg := &protocol.ExecMessage{
		Command: command[0],
		Workdir: cfg.Workdir,
		Env:     cfg.Env,
		TTY:     shared.IsTerminal(),
	}
	if len(command) > 1 {
		execMsg.Args = command[1:]
	}

	// Forward host UID/GID so guest doesn't run as root
	if u, err := user.Current(); err == nil {
		if uid, err := strconv.Atoi(u.Uid); err == nil {
			execMsg.UID = &uid
		}
		if gid, err := strconv.Atoi(u.Gid); err == nil {
			execMsg.GID = &gid
		}
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

	// Forward stdin to guest
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				encoded := base64.StdEncoding.EncodeToString(buf[:n])
				mu.Lock()
				protocol.WriteMessage(conn, &protocol.InputMessage{Data: encoded})
				mu.Unlock()
			}
			if err != nil {
				mu.Lock()
				protocol.WriteMessage(conn, &protocol.SignalMessage{Signal: "STDIN_CLOSE"})
				mu.Unlock()
				return
			}
		}
	}()

	// Timeout watchdog
	var timedOut atomic.Bool
	if timeout > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		go func() {
			<-ctx.Done()
			if ctx.Err() == context.DeadlineExceeded {
				timedOut.Store(true)
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
			if timedOut.Load() {
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
			if timedOut.Load() {
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
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("warden: cannot determine home directory: %w", err)
	}
	kernelPath := defaultKernelPath(homeDir)
	rootfs := rootfsPath(homeDir, cfg.Image, cfg.Tools)

	var warnings []string
	if _, err := os.Stat(kernelPath); err != nil {
		warnings = append(warnings, fmt.Sprintf("  warning: kernel not found at %s", kernelPath))
	}
	if _, err := os.Stat(rootfs); err != nil {
		warnings = append(warnings, fmt.Sprintf("  warning: rootfs not found at %s", rootfs))
	}

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

	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, w)
	}
	return nil
}

// ListRunning returns currently running sandboxes for this runtime.
// Returns nil, nil if the runtime is not available.
func (f *FirecrackerRuntime) ListRunning() ([]runtime.RunningInstance, error) {
	return nil, nil
}
