package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/winler/warden/internal/proxy"
	"golang.org/x/term"
)

func main() {
	os.Exit(run())
}

func run() int {
	cmdName := filepath.Base(os.Args[0])

	conn, err := connect(cmdName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warden-shim: %v\n", err)
		return 127
	}
	defer conn.Close()

	isTTY := term.IsTerminal(int(os.Stdin.Fd()))

	handshake := proxy.ProxyHandshake{
		Command: cmdName,
		Args:    os.Args[1:],
		TTY:     isTTY,
	}
	if err := sendHandshake(conn, &handshake); err != nil {
		fmt.Fprintf(os.Stderr, "warden-shim: handshake failed: %v\n", err)
		return 127
	}

	ready, err := readReady(conn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warden-shim: %v\n", err)
		return 127
	}
	if !ready.OK {
		fmt.Fprintf(os.Stderr, "warden-shim: host error: %s\n", ready.Error)
		return 127
	}

	if isTTY {
		oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
		if err == nil {
			defer term.Restore(int(os.Stdin.Fd()), oldState)
		}
	}

	var mu sync.Mutex
	writeFrame := func(ft byte, p []byte) {
		mu.Lock()
		defer mu.Unlock()
		proxy.WriteFrame(conn, ft, p)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		for sig := range sigCh {
			sigNum := uint32(sig.(syscall.Signal))
			payload := make([]byte, 4)
			binary.LittleEndian.PutUint32(payload, sigNum)
			writeFrame(proxy.FrameSignal, payload)
		}
	}()

	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				writeFrame(proxy.FrameStdin, buf[:n])
			}
			if err != nil {
				writeFrame(proxy.FrameStdin, nil) // EOF
				return
			}
		}
	}()

	exitCode := 1
	for {
		frameType, payload, err := proxy.ReadFrame(conn)
		if err != nil {
			if err == io.EOF {
				break
			}
			fmt.Fprintf(os.Stderr, "warden-shim: read error: %v\n", err)
			break
		}
		switch frameType {
		case proxy.FrameStdout:
			os.Stdout.Write(payload)
		case proxy.FrameStderr:
			os.Stderr.Write(payload)
		case proxy.FrameExit:
			if len(payload) == 4 {
				exitCode = int(int32(binary.LittleEndian.Uint32(payload)))
			}
			return exitCode
		}
	}
	return exitCode
}

func connect(cmdName string) (net.Conn, error) {
	sockPath := filepath.Join("/run/warden-proxy", cmdName+".sock")
	if _, err := os.Stat(sockPath); err == nil {
		return net.Dial("unix", sockPath)
	}

	portPath := filepath.Join("/run/warden-proxy", cmdName+".port")
	data, err := os.ReadFile(portPath)
	if err != nil {
		return nil, fmt.Errorf("no transport found for %q (checked %s and %s)", cmdName, sockPath, portPath)
	}
	port, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return nil, fmt.Errorf("invalid port in %s: %w", portPath, err)
	}

	return dialVsock(uint32(port))
}

func sendHandshake(conn net.Conn, h *proxy.ProxyHandshake) error {
	data, err := json.Marshal(h)
	if err != nil {
		return err
	}
	var lenBuf [4]byte
	binary.LittleEndian.PutUint32(lenBuf[:], uint32(len(data)))
	if _, err := conn.Write(lenBuf[:]); err != nil {
		return err
	}
	_, err = conn.Write(data)
	return err
}

func readReady(conn net.Conn) (*proxy.ProxyReady, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
		return nil, fmt.Errorf("reading ready length: %w", err)
	}
	length := binary.LittleEndian.Uint32(lenBuf[:])
	if length > 1024*1024 {
		return nil, fmt.Errorf("ready message too large: %d", length)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(conn, payload); err != nil {
		return nil, fmt.Errorf("reading ready: %w", err)
	}
	var ready proxy.ProxyReady
	if err := json.Unmarshal(payload, &ready); err != nil {
		return nil, err
	}
	return &ready, nil
}
