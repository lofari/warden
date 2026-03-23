package firecracker

import (
	"net"
	"testing"
)

func TestProxyConnBidirectional(t *testing.T) {
	clientConn, proxyClientSide := net.Pipe()
	proxyServerSide, serverConn := net.Pipe()

	go proxyConn(proxyClientSide, proxyServerSide)

	// Client sends data
	go func() {
		clientConn.Write([]byte("hello from client"))
	}()

	buf := make([]byte, 64)
	n, err := serverConn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != "hello from client" {
		t.Errorf("server got %q", string(buf[:n]))
	}

	// Server sends data back
	go func() {
		serverConn.Write([]byte("hello from server"))
	}()

	n, err = clientConn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != "hello from server" {
		t.Errorf("client got %q", string(buf[:n]))
	}

	clientConn.Close()
	serverConn.Close()
}

func TestPickFreePort(t *testing.T) {
	listener, port, err := pickFreePort()
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if port <= 0 {
		t.Errorf("expected positive port, got %d", port)
	}
}
