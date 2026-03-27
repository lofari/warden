package proxy

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"strings"
	"testing"
)

func TestHandlerEchoCommand(t *testing.T) {
	h := &Handler{
		Command:  "echo",
		HostPath: "/usr/bin/echo",
	}

	shimConn, hostConn := net.Pipe()
	defer shimConn.Close()

	done := make(chan error, 1)
	go func() {
		done <- h.HandleConnection(hostConn)
	}()

	// Send handshake
	handshake := ProxyHandshake{
		Command: "echo",
		Args:    []string{"hello", "proxy"},
		TTY:     false,
	}
	sendJSON(t, shimConn, handshake)

	// Read ProxyReady
	var ready ProxyReady
	readJSON(t, shimConn, &ready)
	if !ready.OK {
		t.Fatalf("ready.OK = false, error: %s", ready.Error)
	}

	// Read frames until exit
	var stdout []byte
	for {
		frameType, payload, err := ReadFrame(shimConn)
		if err != nil {
			t.Fatalf("ReadFrame: %v", err)
		}
		switch frameType {
		case FrameStdout:
			stdout = append(stdout, payload...)
		case FrameStderr:
			// ignore
		case FrameExit:
			code := int32(binary.LittleEndian.Uint32(payload))
			if code != 0 {
				t.Errorf("exit code = %d, want 0", code)
			}
			if string(stdout) != "hello proxy\n" {
				t.Errorf("stdout = %q, want %q", stdout, "hello proxy\n")
			}
			return
		}
	}
}

func TestHandlerCommandNotFound(t *testing.T) {
	h := &Handler{
		Command:  "nonexistent-cmd-xyz",
		HostPath: "/nonexistent-cmd-xyz",
	}

	shimConn, hostConn := net.Pipe()
	defer shimConn.Close()

	done := make(chan error, 1)
	go func() {
		done <- h.HandleConnection(hostConn)
	}()

	sendJSON(t, shimConn, ProxyHandshake{Command: "nonexistent-cmd-xyz"})

	var ready ProxyReady
	readJSON(t, shimConn, &ready)
	if ready.OK {
		t.Fatal("expected ready.OK = false for missing command")
	}
}

func TestHandlerStdinForwarding(t *testing.T) {
	h := &Handler{
		Command:  "cat",
		HostPath: "/usr/bin/cat",
	}

	shimConn, hostConn := net.Pipe()
	defer shimConn.Close()

	done := make(chan error, 1)
	go func() {
		done <- h.HandleConnection(hostConn)
	}()

	sendJSON(t, shimConn, ProxyHandshake{Command: "cat", TTY: false})

	var ready ProxyReady
	readJSON(t, shimConn, &ready)
	if !ready.OK {
		t.Fatalf("ready.OK = false: %s", ready.Error)
	}

	// Send stdin then EOF
	WriteFrame(shimConn, FrameStdin, []byte("test input"))
	WriteFrame(shimConn, FrameStdin, nil) // EOF

	// Read stdout
	var stdout []byte
	for {
		frameType, payload, err := ReadFrame(shimConn)
		if err != nil {
			t.Fatalf("ReadFrame: %v", err)
		}
		switch frameType {
		case FrameStdout:
			stdout = append(stdout, payload...)
		case FrameExit:
			code := int32(binary.LittleEndian.Uint32(payload))
			if code != 0 {
				t.Errorf("exit code = %d, want 0", code)
			}
			if string(stdout) != "test input" {
				t.Errorf("stdout = %q, want %q", stdout, "test input")
			}
			return
		}
	}
}

func TestHandlerExitCode(t *testing.T) {
	h := &Handler{
		Command:  "false",
		HostPath: "/usr/bin/false",
	}

	shimConn, hostConn := net.Pipe()
	defer shimConn.Close()

	done := make(chan error, 1)
	go func() {
		done <- h.HandleConnection(hostConn)
	}()

	sendJSON(t, shimConn, ProxyHandshake{Command: "false", TTY: false})

	var ready ProxyReady
	readJSON(t, shimConn, &ready)
	if !ready.OK {
		t.Fatalf("ready.OK = false: %s", ready.Error)
	}

	for {
		frameType, payload, err := ReadFrame(shimConn)
		if err != nil {
			t.Fatalf("ReadFrame: %v", err)
		}
		if frameType == FrameExit {
			code := int32(binary.LittleEndian.Uint32(payload))
			if code != 1 {
				t.Errorf("exit code = %d, want 1", code)
			}
			return
		}
	}
}

func TestHandlerSignalForwarding(t *testing.T) {
	h := &Handler{
		Command:  "sleep",
		HostPath: "/usr/bin/sleep",
	}

	shimConn, hostConn := net.Pipe()
	defer shimConn.Close()

	done := make(chan error, 1)
	go func() {
		done <- h.HandleConnection(hostConn)
	}()

	sendJSON(t, shimConn, ProxyHandshake{Command: "sleep", Args: []string{"60"}, TTY: false})

	var ready ProxyReady
	readJSON(t, shimConn, &ready)
	if !ready.OK {
		t.Fatalf("ready.OK = false: %s", ready.Error)
	}

	// Send SIGTERM (15)
	sigBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(sigBuf, 15)
	WriteFrame(shimConn, FrameSignal, sigBuf)

	// Read frames until exit
	for {
		frameType, payload, err := ReadFrame(shimConn)
		if err != nil {
			t.Fatalf("ReadFrame: %v", err)
		}
		if frameType == FrameExit {
			code := int32(binary.LittleEndian.Uint32(payload))
			if code == 0 {
				t.Errorf("exit code = 0, want non-zero (process killed by signal)")
			}
			return
		}
	}
}

func TestHandlerEnvMerging(t *testing.T) {
	h := &Handler{
		Command:  "env",
		HostPath: "/usr/bin/env",
	}

	shimConn, hostConn := net.Pipe()
	defer shimConn.Close()

	done := make(chan error, 1)
	go func() {
		done <- h.HandleConnection(hostConn)
	}()

	sendJSON(t, shimConn, ProxyHandshake{
		Command: "env",
		TTY:     false,
		Env:     []string{"WARDEN_TEST_VAR=hello123"},
	})

	var ready ProxyReady
	readJSON(t, shimConn, &ready)
	if !ready.OK {
		t.Fatalf("ready.OK = false: %s", ready.Error)
	}

	var stdout []byte
	for {
		frameType, payload, err := ReadFrame(shimConn)
		if err != nil {
			t.Fatalf("ReadFrame: %v", err)
		}
		switch frameType {
		case FrameStdout:
			stdout = append(stdout, payload...)
		case FrameExit:
			if !strings.Contains(string(stdout), "WARDEN_TEST_VAR=hello123") {
				t.Errorf("stdout does not contain WARDEN_TEST_VAR=hello123\nstdout: %s", stdout)
			}
			<-done
			return
		}
	}
}

// sendJSON writes length-prefixed JSON to the connection.
func sendJSON(t *testing.T, conn net.Conn, v interface{}) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var lenBuf [4]byte
	binary.LittleEndian.PutUint32(lenBuf[:], uint32(len(data)))
	conn.Write(lenBuf[:])
	conn.Write(data)
}

// readJSON reads length-prefixed JSON from the connection.
func readJSON(t *testing.T, conn net.Conn, v interface{}) {
	t.Helper()
	var lenBuf [4]byte
	if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
		t.Fatalf("read length: %v", err)
	}
	length := binary.LittleEndian.Uint32(lenBuf[:])
	data := make([]byte, length)
	if _, err := io.ReadFull(conn, data); err != nil {
		t.Fatalf("read data: %v", err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
}
