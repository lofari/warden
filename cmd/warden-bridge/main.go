package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"sync"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "usage: warden-bridge <uds|vsock> <path-or-port>\n")
		os.Exit(1)
	}

	mode := os.Args[1]
	target := os.Args[2]

	l, err := net.Listen("tcp", "127.0.0.1:19280")
	if err != nil {
		fmt.Fprintf(os.Stderr, "warden-bridge: listen: %v\n", err)
		os.Exit(1)
	}
	defer l.Close()

	fmt.Fprintf(os.Stderr, "warden-bridge: listening on 127.0.0.1:19280 -> %s %s\n", mode, target)

	for {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		go func() {
			defer conn.Close()
			var upstream net.Conn
			var dialErr error

			switch mode {
			case "uds":
				upstream, dialErr = net.Dial("unix", target)
			case "vsock":
				port, parseErr := strconv.ParseUint(target, 10, 32)
				if parseErr != nil {
					fmt.Fprintf(os.Stderr, "warden-bridge: invalid port: %v\n", parseErr)
					return
				}
				upstream, dialErr = dialVsock(uint32(port))
			default:
				fmt.Fprintf(os.Stderr, "warden-bridge: unknown mode: %s\n", mode)
				return
			}

			if dialErr != nil {
				fmt.Fprintf(os.Stderr, "warden-bridge: dial: %v\n", dialErr)
				return
			}
			defer upstream.Close()

			var wg sync.WaitGroup
			wg.Add(2)
			go func() { defer wg.Done(); io.Copy(upstream, conn) }()
			go func() { defer wg.Done(); io.Copy(conn, upstream) }()
			wg.Wait()
		}()
	}
}
