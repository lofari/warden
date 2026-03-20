package main

import (
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"github.com/creack/pty"
	"github.com/mdlayher/vsock"
	"github.com/winler/warden/internal/guest"
	"github.com/winler/warden/internal/protocol"
)

const vsockPort = 1024

func main() {
	if err := mountFilesystems(); err != nil {
		fmt.Fprintf(os.Stderr, "warden-init: mount error: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintln(os.Stderr, "warden-init: ready, listening on vsock port", vsockPort)

	exitCode, err := listenAndServe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warden-init: %v\n", err)
		os.Exit(1)
	}
	os.Exit(exitCode)
}

func listenAndServe() (int, error) {
	l, err := vsock.Listen(vsockPort, nil)
	if err != nil {
		return 1, fmt.Errorf("vsock listen: %w", err)
	}
	defer l.Close()

	lastExitCode := 0
	for {
		conn, err := l.Accept()
		if err != nil {
			return lastExitCode, nil
		}

		code, err := handleConnection(conn)
		conn.Close()

		if err != nil {
			fmt.Fprintf(os.Stderr, "warden-init: connection error: %v\n", err)
			continue
		}
		lastExitCode = code

		fmt.Fprintf(os.Stderr, "warden-init: command exited with code %d, waiting for next connection\n", code)
	}
}

func handleConnection(conn io.ReadWriter) (int, error) {
	writeMsg := func(msg interface{}) error {
		return protocol.WriteMessage(conn, msg)
	}

	var execMsg *protocol.ExecMessage
	for {
		raw, err := protocol.ReadMessage(conn)
		if err != nil {
			return 1, fmt.Errorf("reading message: %w", err)
		}
		switch msg := raw.(type) {
		case *protocol.NetworkConfigMessage:
			if err := guest.ConfigureNetwork(msg.GuestIP, msg.Gateway, msg.DNS); err != nil {
				fmt.Fprintf(os.Stderr, "warden-init: network config warning: %v\n", err)
			} else {
				fmt.Fprintln(os.Stderr, "warden-init: network configured")
			}
		case *protocol.MountConfigMessage:
			if err := setupMounts(conn, msg.Mounts, writeMsg); err != nil {
				fmt.Fprintf(os.Stderr, "warden-init: mount setup warning: %v\n", err)
			}
		case *protocol.ExecMessage:
			execMsg = msg
		default:
			return 1, fmt.Errorf("unexpected message type: %T", raw)
		}
		if execMsg != nil {
			break
		}
	}

	// Build command
	cmd := exec.Command(execMsg.Command, execMsg.Args...)
	cmd.Dir = execMsg.Workdir
	cmd.Env = execMsg.Env
	if len(cmd.Env) == 0 {
		cmd.Env = []string{
			"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
			"HOME=/root",
			"TERM=xterm-256color",
			"LANG=en_US.UTF-8",
		}
	}

	// Set UID/GID if specified
	if execMsg.UID != nil || execMsg.GID != nil {
		cred := &syscall.Credential{}
		if execMsg.UID != nil {
			cred.Uid = uint32(*execMsg.UID)
		}
		if execMsg.GID != nil {
			cred.Gid = uint32(*execMsg.GID)
		}
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: cred}
	}

	// Mutex for writes to the connection (replace simple writeMsg with a mutex-protected one)
	var mu sync.Mutex
	writeMsg = func(msg interface{}) error {
		mu.Lock()
		defer mu.Unlock()
		return protocol.WriteMessage(conn, msg)
	}

	// Use PTY mode if requested
	if execMsg.TTY {
		return handleConnectionTTY(conn, cmd, writeMsg)
	}

	// Set up pipes
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return 1, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 1, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return 1, fmt.Errorf("stderr pipe: %w", err)
	}

	// Start command
	if err := cmd.Start(); err != nil {
		// Command not found → exit code 127
		writeMsg(&protocol.ExitMessage{Code: 127})
		return 127, nil
	}

	// Start message reader goroutine (handles stdin data and signals)
	go func() {
		for {
			raw, err := protocol.ReadMessage(conn)
			if err != nil {
				stdinPipe.Close()
				return
			}
			switch msg := raw.(type) {
			case *protocol.InputMessage:
				data, err := base64.StdEncoding.DecodeString(msg.Data)
				if err != nil {
					continue
				}
				stdinPipe.Write(data)
			case *protocol.SignalMessage:
				if msg.Signal == "STDIN_CLOSE" {
					stdinPipe.Close()
					continue
				}
				sig := parseSignal(msg.Signal)
				if sig != 0 && cmd.Process != nil {
					syscall.Kill(cmd.Process.Pid, sig)
				}
			}
		}
	}()

	// Stream stdout and stderr
	var wg sync.WaitGroup
	streamOutput := func(r io.Reader, streamType string) {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				encoded := base64.StdEncoding.EncodeToString(buf[:n])
				writeMsg(&protocol.OutputMessage{
					Type: streamType,
					Data: encoded,
				})
			}
			if err != nil {
				return
			}
		}
	}

	wg.Add(2)
	go streamOutput(stdout, "stdout")
	go streamOutput(stderr, "stderr")

	// Wait for output streams to drain, then for command to exit
	wg.Wait()
	exitCode := 0
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	writeMsg(&protocol.ExitMessage{Code: exitCode})
	return exitCode, nil
}

func handleConnectionTTY(conn io.ReadWriter, cmd *exec.Cmd, writeMsg func(interface{}) error) (int, error) {
	ptmx, err := pty.Start(cmd)
	if err != nil {
		writeMsg(&protocol.ExitMessage{Code: 127})
		return 127, nil
	}
	defer ptmx.Close()

	// Message reader: stdin, signals, window resize
	go func() {
		for {
			raw, err := protocol.ReadMessage(conn)
			if err != nil {
				return
			}
			switch msg := raw.(type) {
			case *protocol.InputMessage:
				data, _ := base64.StdEncoding.DecodeString(msg.Data)
				ptmx.Write(data)
			case *protocol.SignalMessage:
				if msg.Signal == "STDIN_CLOSE" {
					continue
				}
				sig := parseSignal(msg.Signal)
				if sig != 0 && cmd.Process != nil {
					syscall.Kill(cmd.Process.Pid, sig)
				}
			case *protocol.ResizeMessage:
				pty.Setsize(ptmx, &pty.Winsize{
					Rows: uint16(msg.Rows),
					Cols: uint16(msg.Cols),
				})
			}
		}
	}()

	// Stream PTY output (combined stdout/stderr)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				encoded := base64.StdEncoding.EncodeToString(buf[:n])
				writeMsg(&protocol.OutputMessage{Type: "stdout", Data: encoded})
			}
			if err != nil {
				return
			}
		}
	}()

	exitCode := 0
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	writeMsg(&protocol.ExitMessage{Code: exitCode})
	return exitCode, nil
}

func parseSignal(name string) syscall.Signal {
	switch name {
	case "SIGTERM":
		return syscall.SIGTERM
	case "SIGINT":
		return syscall.SIGINT
	case "SIGKILL":
		return syscall.SIGKILL
	case "SIGHUP":
		return syscall.SIGHUP
	default:
		return 0
	}
}

var fuseCleanups []func()

func setupMounts(cmdConn io.ReadWriter, mounts []protocol.MountInfo, writeMsg func(interface{}) error) error {
	// Phase 1: Start all listeners
	listeners := make([]*vsock.Listener, 0, len(mounts))
	for _, m := range mounts {
		l, err := vsock.Listen(m.VsockPort, nil)
		if err != nil {
			for _, prev := range listeners {
				prev.Close()
			}
			return fmt.Errorf("vsock listen port %d: %w", m.VsockPort, err)
		}
		listeners = append(listeners, l)
	}

	// Signal to host that all ports are ready
	writeMsg(&protocol.MountsReadyMessage{})

	// Phase 2: Accept ALL connections first
	conns := make([]net.Conn, len(mounts))
	for i := range mounts {
		conn, err := listeners[i].Accept()
		listeners[i].Close()
		if err != nil {
			// Close already-accepted connections
			for j := 0; j < i; j++ {
				conns[j].Close()
			}
			return fmt.Errorf("accept port %d: %w", mounts[i].VsockPort, err)
		}
		conns[i] = conn
	}

	// Phase 3: Set up FUSE mounts (now that all connections are established)
	for i, m := range mounts {
		if err := os.MkdirAll(m.GuestPath, 0o755); err != nil {
			conns[i].Close()
			return fmt.Errorf("mkdir %s: %w", m.GuestPath, err)
		}

		client := guest.NewFileClient(conns[i])
		cleanup, err := guest.MountFUSE(m.GuestPath, client)
		if err != nil {
			conns[i].Close()
			return fmt.Errorf("FUSE mount %s: %w", m.GuestPath, err)
		}
		fuseCleanups = append(fuseCleanups, cleanup)
		fmt.Fprintf(os.Stderr, "warden-init: mounted %s\n", m.GuestPath)
	}
	_ = cmdConn
	return nil
}

func mountFilesystems() error {
	mounts := []struct {
		source string
		target string
		fstype string
		flags  uintptr
	}{
		{"proc", "/proc", "proc", 0},
		{"sysfs", "/sys", "sysfs", 0},
		{"devtmpfs", "/dev", "devtmpfs", 0},
	}

	for _, m := range mounts {
		os.MkdirAll(m.target, 0o755)
		if err := syscall.Mount(m.source, m.target, m.fstype, m.flags, ""); err != nil {
			if !os.IsExist(err) {
				return fmt.Errorf("mounting %s: %w", m.target, err)
			}
		}
	}
	return nil
}
