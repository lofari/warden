package main

import (
	"encoding/base64"
	"net"
	"testing"

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
