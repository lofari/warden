package main

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"

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

	conn, err := l.Accept()
	if err != nil {
		return 1, fmt.Errorf("vsock accept: %w", err)
	}
	defer conn.Close()

	return handleConnection(conn)
}

func handleConnection(conn io.ReadWriter) (int, error) {
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

	// Set up pipes
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 1, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return 1, fmt.Errorf("stderr pipe: %w", err)
	}

	// Mutex for writes to the connection
	var mu sync.Mutex
	writeMsg := func(msg interface{}) error {
		mu.Lock()
		defer mu.Unlock()
		return protocol.WriteMessage(conn, msg)
	}

	// Start command
	if err := cmd.Start(); err != nil {
		// Command not found → exit code 127
		writeMsg(&protocol.ExitMessage{Code: 127})
		return 127, nil
	}

	// Start signal reader goroutine
	go func() {
		for {
			raw, err := protocol.ReadMessage(conn)
			if err != nil {
				return
			}
			sigMsg, ok := raw.(*protocol.SignalMessage)
			if !ok {
				continue
			}
			sig := parseSignal(sigMsg.Signal)
			if sig != 0 && cmd.Process != nil {
				syscall.Kill(cmd.Process.Pid, sig)
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
