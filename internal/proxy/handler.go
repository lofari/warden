package proxy

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"github.com/creack/pty"
)

// Handler manages a proxy listener for a single command.
type Handler struct {
	Command  string       // command name (e.g. "claude")
	HostPath string       // resolved host binary path
	Listener net.Listener // underlying listener (UDS or vsock)
}

// Serve accepts connections sequentially, one at a time.
func (h *Handler) Serve() {
	for {
		conn, err := h.Listener.Accept()
		if err != nil {
			return // listener closed
		}
		h.HandleConnection(conn)
		conn.Close()
	}
}

// Close closes the underlying listener.
func (h *Handler) Close() error {
	if h.Listener != nil {
		return h.Listener.Close()
	}
	return nil
}

// HandleConnection processes a single shim connection:
// read handshake, spawn command, relay stdio, send exit code.
func (h *Handler) HandleConnection(conn net.Conn) error {
	// Read handshake (length-prefixed JSON)
	var lenBuf [4]byte
	if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
		return fmt.Errorf("reading handshake length: %w", err)
	}
	length := binary.LittleEndian.Uint32(lenBuf[:])
	if length > 1024*1024 {
		return fmt.Errorf("handshake too large: %d bytes", length)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(conn, payload); err != nil {
		return fmt.Errorf("reading handshake: %w", err)
	}

	var handshake ProxyHandshake
	if err := json.Unmarshal(payload, &handshake); err != nil {
		_ = sendReady(conn, false, fmt.Sprintf("invalid handshake: %v", err))
		return err
	}

	// Build command
	cmd := exec.Command(h.HostPath, handshake.Args...)
	cmd.Env = os.Environ()
	for _, e := range handshake.Env {
		cmd.Env = append(cmd.Env, e)
	}

	if handshake.TTY {
		return h.handleTTY(conn, cmd)
	}
	return h.handlePipes(conn, cmd)
}

func (h *Handler) handlePipes(conn net.Conn, cmd *exec.Cmd) error {
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		_ = sendReady(conn, false, err.Error())
		return err
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		_ = sendReady(conn, false, err.Error())
		return err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		_ = sendReady(conn, false, err.Error())
		return err
	}

	if err := cmd.Start(); err != nil {
		_ = sendReady(conn, false, err.Error())
		return err
	}

	if err := sendReady(conn, true, ""); err != nil {
		cmd.Process.Kill()
		return err
	}

	var mu sync.Mutex
	writeFrame := func(ft byte, p []byte) {
		mu.Lock()
		defer mu.Unlock()
		WriteFrame(conn, ft, p)
	}

	// Read stdin frames from shim
	go func() {
		defer stdinPipe.Close()
		for {
			frameType, payload, err := ReadFrame(conn)
			if err != nil {
				return
			}
			switch frameType {
			case FrameStdin:
				if len(payload) == 0 {
					return // EOF signal
				}
				if _, err := stdinPipe.Write(payload); err != nil {
					return
				}
			case FrameSignal:
				if len(payload) == 4 {
					sig := syscall.Signal(binary.LittleEndian.Uint32(payload))
					if cmd.Process != nil {
						cmd.Process.Signal(sig)
					}
				}
			}
		}
	}()

	// Stream stdout and stderr to shim
	var wg sync.WaitGroup
	relay := func(r io.Reader, ft byte) {
		defer wg.Done()
		buf := make([]byte, 32*1024)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				writeFrame(ft, buf[:n])
			}
			if err != nil {
				return
			}
		}
	}
	wg.Add(2)
	go relay(stdoutPipe, FrameStdout)
	go relay(stderrPipe, FrameStderr)

	wg.Wait()
	exitCode := 0
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	writeFrame(FrameExit, exitPayload(int32(exitCode)))
	return nil
}

func (h *Handler) handleTTY(conn net.Conn, cmd *exec.Cmd) error {
	ptmx, err := pty.Start(cmd)
	if err != nil {
		_ = sendReady(conn, false, err.Error())
		return err
	}
	defer ptmx.Close()

	if err := sendReady(conn, true, ""); err != nil {
		cmd.Process.Kill()
		return err
	}

	var mu sync.Mutex
	writeFrame := func(ft byte, p []byte) {
		mu.Lock()
		defer mu.Unlock()
		WriteFrame(conn, ft, p)
	}

	// Read frames from shim
	go func() {
		for {
			frameType, payload, err := ReadFrame(conn)
			if err != nil {
				return
			}
			switch frameType {
			case FrameStdin:
				if _, err := ptmx.Write(payload); err != nil {
					return
				}
			case FrameSignal:
				if len(payload) == 4 {
					sig := syscall.Signal(binary.LittleEndian.Uint32(payload))
					if cmd.Process != nil {
						cmd.Process.Signal(sig)
					}
				}
			case FrameResize:
				var size struct {
					Cols int `json:"cols"`
					Rows int `json:"rows"`
				}
				if json.Unmarshal(payload, &size) == nil {
					pty.Setsize(ptmx, &pty.Winsize{
						Rows: uint16(size.Rows),
						Cols: uint16(size.Cols),
					})
				}
			}
		}
	}()

	// Stream PTY output — track with WaitGroup so we drain before sending FrameExit
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 32*1024)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				writeFrame(FrameStdout, buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	exitCode := 0
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	wg.Wait()
	writeFrame(FrameExit, exitPayload(int32(exitCode)))
	return nil
}

func sendReady(conn net.Conn, ok bool, errMsg string) error {
	ready := ProxyReady{OK: ok, Error: errMsg}
	data, _ := json.Marshal(ready)
	var lenBuf [4]byte
	binary.LittleEndian.PutUint32(lenBuf[:], uint32(len(data)))
	if _, err := conn.Write(lenBuf[:]); err != nil {
		return err
	}
	if _, err := conn.Write(data); err != nil {
		return err
	}
	return nil
}

func exitPayload(code int32) []byte {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, uint32(code))
	return buf
}
