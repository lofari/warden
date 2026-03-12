package protocol

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func TestExecMessageRoundTrip(t *testing.T) {
	msg := &ExecMessage{
		Command: "node",
		Args:    []string{"index.js"},
		Workdir: "/home/user/project",
		Env:     []string{"NODE_ENV=dev"},
		UID:     1000,
		GID:     1000,
		TTY:     true,
	}
	var buf bytes.Buffer
	if err := WriteMessage(&buf, msg); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	got, err := ReadMessage(&buf)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	exec, ok := got.(*ExecMessage)
	if !ok {
		t.Fatalf("got type %T, want *ExecMessage", got)
	}
	if exec.Command != "node" {
		t.Errorf("command = %q, want node", exec.Command)
	}
	if exec.TTY != true {
		t.Error("tty should be true")
	}
}

func TestSignalMessageRoundTrip(t *testing.T) {
	msg := &SignalMessage{Signal: "SIGINT"}
	var buf bytes.Buffer
	if err := WriteMessage(&buf, msg); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	got, err := ReadMessage(&buf)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	sig, ok := got.(*SignalMessage)
	if !ok {
		t.Fatalf("got type %T, want *SignalMessage", got)
	}
	if sig.Signal != "SIGINT" {
		t.Errorf("signal = %q, want SIGINT", sig.Signal)
	}
}

func TestOutputMessageRoundTrip(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("hello world"))
	msg := &OutputMessage{Type: "stdout", Data: encoded}
	var buf bytes.Buffer
	if err := WriteMessage(&buf, msg); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	got, err := ReadMessage(&buf)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	out, ok := got.(*OutputMessage)
	if !ok {
		t.Fatalf("got type %T, want *OutputMessage", got)
	}
	decoded, err := base64.StdEncoding.DecodeString(out.Data)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	if string(decoded) != "hello world" {
		t.Errorf("data = %q, want hello world", decoded)
	}
}

func TestExitMessageRoundTrip(t *testing.T) {
	msg := &ExitMessage{Code: 42}
	var buf bytes.Buffer
	if err := WriteMessage(&buf, msg); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	got, err := ReadMessage(&buf)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	exit, ok := got.(*ExitMessage)
	if !ok {
		t.Fatalf("got type %T, want *ExitMessage", got)
	}
	if exit.Code != 42 {
		t.Errorf("code = %d, want 42", exit.Code)
	}
}
