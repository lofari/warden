package protocol

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"strings"
	"testing"
)

func intPtr(v int) *int { return &v }

func TestExecMessageRoundTrip(t *testing.T) {
	msg := &ExecMessage{
		Command: "node",
		Args:    []string{"index.js"},
		Workdir: "/home/user/project",
		Env:     []string{"NODE_ENV=dev"},
		UID:     intPtr(1000),
		GID:     intPtr(1000),
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

func TestNetworkConfigMessageRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	msg := &NetworkConfigMessage{GuestIP: "172.16.0.2/30", Gateway: "172.16.0.1", DNS: "8.8.8.8"}
	if err := WriteMessage(&buf, msg); err != nil {
		t.Fatal(err)
	}
	raw, err := ReadMessage(&buf)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := raw.(*NetworkConfigMessage)
	if !ok {
		t.Fatalf("expected NetworkConfigMessage, got %T", raw)
	}
	if got.GuestIP != msg.GuestIP || got.Gateway != msg.Gateway || got.DNS != msg.DNS {
		t.Fatalf("round-trip mismatch: %+v vs %+v", got, msg)
	}
}

func TestInputMessageRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	msg := &InputMessage{Data: base64.StdEncoding.EncodeToString([]byte("hello\n"))}
	if err := WriteMessage(&buf, msg); err != nil {
		t.Fatal(err)
	}
	raw, err := ReadMessage(&buf)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := raw.(*InputMessage)
	if !ok {
		t.Fatalf("expected InputMessage, got %T", raw)
	}
	if got.Data != msg.Data {
		t.Fatalf("data mismatch: %q vs %q", got.Data, msg.Data)
	}
}

func TestResizeMessageRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	msg := &ResizeMessage{Rows: 24, Cols: 80}
	if err := WriteMessage(&buf, msg); err != nil {
		t.Fatal(err)
	}
	raw, err := ReadMessage(&buf)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := raw.(*ResizeMessage)
	if !ok {
		t.Fatalf("expected ResizeMessage, got %T", raw)
	}
	if got.Rows != 24 || got.Cols != 80 {
		t.Fatalf("resize mismatch: %+v", got)
	}
}

func TestDisplayConfigMessageRoundtrip(t *testing.T) {
	var buf bytes.Buffer
	msg := &DisplayConfigMessage{Resolution: "1920x1080x24", VsockPort: 2048}
	if err := WriteMessage(&buf, msg); err != nil {
		t.Fatal(err)
	}
	raw, err := ReadMessage(&buf)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := raw.(*DisplayConfigMessage)
	if !ok {
		t.Fatalf("expected *DisplayConfigMessage, got %T", raw)
	}
	if got.Resolution != "1920x1080x24" || got.VsockPort != 2048 {
		t.Errorf("got %+v", got)
	}
}

func TestDisplayReadyMessageRoundtrip(t *testing.T) {
	var buf bytes.Buffer
	msg := &DisplayReadyMessage{Port: 2048}
	if err := WriteMessage(&buf, msg); err != nil {
		t.Fatal(err)
	}
	raw, err := ReadMessage(&buf)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := raw.(*DisplayReadyMessage)
	if !ok {
		t.Fatalf("expected *DisplayReadyMessage, got %T", raw)
	}
	if got.Port != 2048 {
		t.Errorf("port = %d", got.Port)
	}
}

func TestProxyConfigMessageRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	msg := &ProxyConfigMessage{
		Proxies: []ProxyEntry{
			{Command: "claude", Port: 3000},
			{Command: "cursor", Port: 3001},
		},
	}
	if err := WriteMessage(&buf, msg); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	raw, err := ReadMessage(&buf)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	got, ok := raw.(*ProxyConfigMessage)
	if !ok {
		t.Fatalf("expected *ProxyConfigMessage, got %T", raw)
	}
	if len(got.Proxies) != 2 {
		t.Fatalf("proxies count = %d, want 2", len(got.Proxies))
	}
	if got.Proxies[0].Command != "claude" || got.Proxies[0].Port != 3000 {
		t.Errorf("proxy[0] = %+v, want {claude 3000}", got.Proxies[0])
	}
	if got.Proxies[1].Command != "cursor" || got.Proxies[1].Port != 3001 {
		t.Errorf("proxy[1] = %+v, want {cursor 3001}", got.Proxies[1])
	}
}

func TestAuthBrokerConfigMessageRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	msg := &AuthBrokerConfigMessage{
		Port:            2900,
		FakeCredentials: `{"accessToken":"warden-sandbox-token"}`,
		BaseURL:         "http://localhost:19280",
	}
	if err := WriteMessage(&buf, msg); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	raw, err := ReadMessage(&buf)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	got, ok := raw.(*AuthBrokerConfigMessage)
	if !ok {
		t.Fatalf("expected *AuthBrokerConfigMessage, got %T", raw)
	}
	if got.Port != 2900 {
		t.Errorf("port = %d, want 2900", got.Port)
	}
	if got.FakeCredentials != msg.FakeCredentials {
		t.Errorf("fake creds mismatch")
	}
	if got.BaseURL != "http://localhost:19280" {
		t.Errorf("base_url = %q", got.BaseURL)
	}
}

func TestAuthBrokerReadyMessageRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteMessage(&buf, &AuthBrokerReadyMessage{}); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	raw, err := ReadMessage(&buf)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if _, ok := raw.(*AuthBrokerReadyMessage); !ok {
		t.Fatalf("expected *AuthBrokerReadyMessage, got %T", raw)
	}
}

func TestReadMessageRejectsOversizedPayload(t *testing.T) {
	// Craft a length prefix claiming 32MB payload
	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, uint32(32*1024*1024))
	buf.Write(make([]byte, 64)) // partial payload doesn't matter

	_, err := ReadMessage(&buf)
	if err == nil {
		t.Fatal("expected error for oversized message")
	}
	if !strings.Contains(err.Error(), "exceeds max") {
		t.Fatalf("expected max size error, got: %v", err)
	}
}
