package firecracker

import (
	"fmt"
	"io"
	"net"
	"time"
)

const vncVsockPort = uint32(2048)
const defaultResolution = "1280x1024x24"

// proxyVNC accepts TCP connections and proxies them to the guest vsock port.
// Loops to support VNC client reconnection. Returns when listener is closed.
func proxyVNC(listener net.Listener, vsockPath string) {
	for {
		tcpConn, err := listener.Accept()
		if err != nil {
			return
		}
		go func() {
			defer tcpConn.Close()
			vsockConn, err := dialGuest(vsockPath, vncVsockPort, 10*time.Second)
			if err != nil {
				return
			}
			defer vsockConn.Close()
			proxyConn(tcpConn, vsockConn)
		}()
	}
}

// proxyConn copies bytes bidirectionally between two connections.
func proxyConn(a, b net.Conn) {
	go io.Copy(b, a)
	io.Copy(a, b)
}

// pickFreePort finds a free TCP port on localhost.
func pickFreePort() (net.Listener, int, error) {
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return nil, 0, fmt.Errorf("picking free port: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	return listener, port, nil
}
