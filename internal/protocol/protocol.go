package protocol

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

// MaxMessageSize is the maximum allowed size for a single protocol message (16 MiB).
// This prevents OOM from malicious or buggy length prefixes.
const MaxMessageSize = 16 * 1024 * 1024 // 16 MiB

// Message types for vsock protocol.
// Host -> Guest: ExecMessage, SignalMessage
// Guest -> Host: OutputMessage, ExitMessage

type ExecMessage struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Workdir string   `json:"workdir"`
	Env     []string `json:"env"`
	UID     int      `json:"uid"`
	GID     int      `json:"gid"`
	TTY     bool     `json:"tty"`
}

type SignalMessage struct {
	Signal string `json:"signal"`
}

type OutputMessage struct {
	Type string `json:"type"` // "stdout" or "stderr"
	Data string `json:"data"` // base64-encoded bytes
}

type ExitMessage struct {
	Code int `json:"code"`
}

// envelope wraps any message with a type discriminator for serialization.
type envelope struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// WriteMessage writes a length-prefixed JSON message to w.
func WriteMessage(w io.Writer, msg interface{}) error {
	var typeName string
	switch msg.(type) {
	case *ExecMessage:
		typeName = "exec"
	case *SignalMessage:
		typeName = "signal"
	case *OutputMessage:
		typeName = "output"
	case *ExitMessage:
		typeName = "exit"
	default:
		return fmt.Errorf("unknown message type: %T", msg)
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	env := envelope{Type: typeName, Data: data}
	payload, err := json.Marshal(env)
	if err != nil {
		return err
	}

	// 4-byte little-endian length prefix
	var lenBuf [4]byte
	binary.LittleEndian.PutUint32(lenBuf[:], uint32(len(payload)))
	if _, err := w.Write(lenBuf[:]); err != nil {
		return err
	}
	_, err = w.Write(payload)
	return err
}

// ReadMessage reads a length-prefixed JSON message from r.
func ReadMessage(r io.Reader) (interface{}, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return nil, err
	}
	length := binary.LittleEndian.Uint32(lenBuf[:])
	if length > MaxMessageSize {
		return nil, fmt.Errorf("message size %d exceeds max %d", length, MaxMessageSize)
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}

	var env envelope
	if err := json.Unmarshal(payload, &env); err != nil {
		return nil, err
	}

	switch env.Type {
	case "exec":
		var m ExecMessage
		if err := json.Unmarshal(env.Data, &m); err != nil {
			return nil, err
		}
		return &m, nil
	case "signal":
		var m SignalMessage
		if err := json.Unmarshal(env.Data, &m); err != nil {
			return nil, err
		}
		return &m, nil
	case "output":
		var m OutputMessage
		if err := json.Unmarshal(env.Data, &m); err != nil {
			return nil, err
		}
		return &m, nil
	case "exit":
		var m ExitMessage
		if err := json.Unmarshal(env.Data, &m); err != nil {
			return nil, err
		}
		return &m, nil
	default:
		return nil, fmt.Errorf("unknown message type: %q", env.Type)
	}
}
