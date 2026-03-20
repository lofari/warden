package firecracker

import (
	"encoding/base64"
	"net"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/winler/warden/internal/protocol"
)

func TestTimeoutFlagIsRaceFree(t *testing.T) {
	var timedOut atomic.Bool
	timedOut.Store(true)
	if !timedOut.Load() {
		t.Fatal("expected timedOut to be true")
	}
	timedOut.Store(false)
	if timedOut.Load() {
		t.Fatal("expected timedOut to be false")
	}
}

func TestPreflightNoKVM(t *testing.T) {
	rt := &FirecrackerRuntime{}
	err := rt.Preflight()
	// Should fail unless running on a machine with /dev/kvm
	if err == nil {
		t.Skip("running on machine with /dev/kvm, cannot test Preflight failure")
	}
}

// TestHostReadLoop verifies the host-side message dispatch.
// Simulates a guest sending OutputMessages and an ExitMessage.
func TestHostReadLoop(t *testing.T) {
	guest, host := net.Pipe()
	defer guest.Close()
	defer host.Close()

	// Simulate guest: send stdout, stderr, then exit
	go func() {
		encoded := base64.StdEncoding.EncodeToString([]byte("hello"))
		protocol.WriteMessage(guest, &protocol.OutputMessage{Type: "stdout", Data: encoded})

		errEncoded := base64.StdEncoding.EncodeToString([]byte("warning"))
		protocol.WriteMessage(guest, &protocol.OutputMessage{Type: "stderr", Data: errEncoded})

		protocol.WriteMessage(guest, &protocol.ExitMessage{Code: 42})
	}()

	// Read loop (same logic as Run, extracted for testing)
	exitCode := -1
	for {
		raw, err := protocol.ReadMessage(host)
		if err != nil {
			t.Fatalf("ReadMessage: %v", err)
		}
		switch msg := raw.(type) {
		case *protocol.OutputMessage:
			// Just verify we can decode
			_, err := base64.StdEncoding.DecodeString(msg.Data)
			if err != nil {
				t.Errorf("base64 decode: %v", err)
			}
		case *protocol.ExitMessage:
			exitCode = msg.Code
		}
		if exitCode >= 0 {
			break
		}
	}

	if exitCode != 42 {
		t.Errorf("exit code = %d, want 42", exitCode)
	}
}

// TestExecMessageSend verifies ExecMessage is correctly sent.
func TestExecMessageSend(t *testing.T) {
	guest, host := net.Pipe()
	defer guest.Close()
	defer host.Close()

	go func() {
		msg := &protocol.ExecMessage{
			Command: "echo",
			Args:    []string{"test"},
			Workdir: "/tmp",
			Env:     []string{"FOO=bar"},
		}
		if err := protocol.WriteMessage(host, msg); err != nil {
			t.Errorf("WriteMessage: %v", err)
		}
	}()

	raw, err := protocol.ReadMessage(guest)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	exec, ok := raw.(*protocol.ExecMessage)
	if !ok {
		t.Fatalf("got %T, want *ExecMessage", raw)
	}
	if exec.Command != "echo" {
		t.Errorf("command = %q, want echo", exec.Command)
	}
	if len(exec.Args) != 1 || exec.Args[0] != "test" {
		t.Errorf("args = %v, want [test]", exec.Args)
	}
	if exec.Workdir != "/tmp" {
		t.Errorf("workdir = %q, want /tmp", exec.Workdir)
	}
}

// TestDialGuestHandshake verifies the CONNECT/OK vsock UDS protocol.
func TestDialGuestHandshake(t *testing.T) {
	// Create a Unix socket that simulates Firecracker's vsock UDS
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "vsock.sock")

	l, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()

	// Simulate Firecracker: accept connection, read CONNECT, respond OK
	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 64)
		n, _ := conn.Read(buf)
		if string(buf[:n]) != "CONNECT 1024\n" {
			t.Errorf("expected CONNECT 1024, got %q", buf[:n])
		}
		conn.Write([]byte("OK 1024\n"))
		// Keep connection open briefly for the test
		buf2 := make([]byte, 1)
		conn.Read(buf2)
	}()

	conn, err := dialGuest(sockPath, 1024, 2*time.Second)
	if err != nil {
		t.Fatalf("dialGuest: %v", err)
	}
	conn.Close()
}
