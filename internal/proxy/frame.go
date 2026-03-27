package proxy

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Frame types for the shim <-> host wire protocol.
const (
	FrameStdin  byte = 0x01 // shim -> host
	FrameStdout byte = 0x02 // host -> shim
	FrameStderr byte = 0x03 // host -> shim
	FrameExit   byte = 0x04 // host -> shim: 4-byte LE int32
	FrameResize byte = 0x05 // shim -> host: JSON {"cols":N,"rows":N}
	FrameSignal byte = 0x06 // shim -> host: 4-byte signal number
)

// MaxFrameSize is the maximum payload size for a single frame (4 MiB).
const MaxFrameSize = 4 * 1024 * 1024

// WriteFrame writes a length-prefixed frame: [1 byte type][4 bytes LE length][payload].
func WriteFrame(w io.Writer, frameType byte, payload []byte) error {
	var header [5]byte
	header[0] = frameType
	binary.LittleEndian.PutUint32(header[1:], uint32(len(payload)))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	if len(payload) > 0 {
		_, err := w.Write(payload)
		return err
	}
	return nil
}

// ReadFrame reads a length-prefixed frame, returning the type and payload.
func ReadFrame(r io.Reader) (byte, []byte, error) {
	var header [5]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return 0, nil, err
	}
	frameType := header[0]
	length := binary.LittleEndian.Uint32(header[1:])
	if length > MaxFrameSize {
		return 0, nil, fmt.Errorf("frame size %d exceeds max %d", length, MaxFrameSize)
	}
	payload := make([]byte, length)
	if length > 0 {
		if _, err := io.ReadFull(r, payload); err != nil {
			return 0, nil, err
		}
	}
	return frameType, payload, nil
}
