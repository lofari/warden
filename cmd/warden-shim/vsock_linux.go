//go:build linux

package main

import (
	"net"

	"github.com/mdlayher/vsock"
)

func dialVsock(port uint32) (net.Conn, error) {
	return vsock.Dial(2, port, nil) // CID 2 = host
}
