//go:build !linux

package main

import (
	"fmt"
	"net"
)

func dialVsock(port uint32) (net.Conn, error) {
	return nil, fmt.Errorf("vsock not supported on this platform")
}
