package main

import (
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/winler/warden/internal/protocol"
)

func TestHandleConnectionEcho(t *testing.T) {
	guest, host := net.Pipe()
	defer guest.Close()
	defer host.Close()

	// Host sends ExecMessage
	go func() {
		protocol.WriteMessage(host, &protocol.ExecMessage{
			Command: "echo",
			Args:    []string{"hello"},
			Workdir: "/",
			Env:     []string{"PATH=/usr/bin:/bin"},
		})

		// Read responses
		for {
			msg, err := protocol.ReadMessage(host)
			if err != nil {
				return
			}
			switch m := msg.(type) {
			case *protocol.ExitMessage:
				if m.Code != 0 {
					t.Errorf("exit code = %d, want 0", m.Code)
				}
				return
			case *protocol.OutputMessage:
				if m.Type == "stdout" {
					decoded, _ := base64.StdEncoding.DecodeString(m.Data)
					if string(decoded) != "hello\n" {
						t.Errorf("stdout = %q, want %q", decoded, "hello\n")
					}
				}
			}
		}
	}()

	code, err := handleConnection(guest)
	if err != nil {
		t.Fatalf("handleConnection: %v", err)
	}
	if code != 0 {
		t.Errorf("return code = %d, want 0", code)
	}
}

func TestHandleConnectionStdinForwarding(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	done := make(chan int, 1)
	go func() {
		code, _ := handleConnection(serverConn)
		done <- code
	}()

	execMsg := &protocol.ExecMessage{Command: "cat"}
	protocol.WriteMessage(clientConn, execMsg)

	encoded := base64.StdEncoding.EncodeToString([]byte("hello from stdin\n"))
	protocol.WriteMessage(clientConn, &protocol.InputMessage{Data: encoded})
	protocol.WriteMessage(clientConn, &protocol.SignalMessage{Signal: "STDIN_CLOSE"})

	msgCh := make(chan interface{}, 10)
	go func() {
		for {
			raw, err := protocol.ReadMessage(clientConn)
			if err != nil {
				close(msgCh)
				return
			}
			msgCh <- raw
		}
	}()

	var gotOutput string
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timeout waiting for output")
		case raw, ok := <-msgCh:
			if !ok {
				t.Fatal("connection closed without exit message")
			}
			switch msg := raw.(type) {
			case *protocol.OutputMessage:
				decoded, _ := base64.StdEncoding.DecodeString(msg.Data)
				gotOutput += string(decoded)
			case *protocol.ExitMessage:
				if !strings.Contains(gotOutput, "hello from stdin") {
					t.Fatalf("expected stdin echo, got: %q", gotOutput)
				}
				return
			}
		}
	}
}

func TestListenAndServeAcceptsMultipleConnections(t *testing.T) {
	for i := 0; i < 2; i++ {
		clientConn, serverConn := net.Pipe()

		done := make(chan int, 1)
		go func() {
			code, _ := handleConnection(serverConn)
			done <- code
			serverConn.Close()
		}()

		protocol.WriteMessage(clientConn, &protocol.ExecMessage{
			Command: "echo",
			Args:    []string{fmt.Sprintf("iteration-%d", i)},
		})

		for {
			raw, err := protocol.ReadMessage(clientConn)
			if err != nil {
				break
			}
			if _, ok := raw.(*protocol.ExitMessage); ok {
				break
			}
		}

		clientConn.Close()
		code := <-done
		if code != 0 {
			t.Fatalf("iteration %d: expected exit code 0, got %d", i, code)
		}
	}
}

func TestHandleConnectionNotFound(t *testing.T) {
	guest, host := net.Pipe()
	defer guest.Close()
	defer host.Close()

	go func() {
		protocol.WriteMessage(host, &protocol.ExecMessage{
			Command: "/nonexistent/binary",
			Workdir: "/",
			Env:     []string{"PATH=/usr/bin:/bin"},
		})

		for {
			msg, err := protocol.ReadMessage(host)
			if err != nil {
				return
			}
			if exit, ok := msg.(*protocol.ExitMessage); ok {
				if exit.Code != 127 {
					t.Errorf("exit code = %d, want 127", exit.Code)
				}
				return
			}
		}
	}()

	code, _ := handleConnection(guest)
	if code != 127 {
		t.Errorf("return code = %d, want 127", code)
	}
}
