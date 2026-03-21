package protocol

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

// MaxMessageSize is the maximum allowed size for a single protocol message (6 MiB).
// Must exceed 4 MiB maxReadSize + base64 overhead.
const MaxMessageSize = 6 * 1024 * 1024 // 6 MiB

// Message types for vsock protocol.
// Host -> Guest: ExecMessage, SignalMessage
// Guest -> Host: OutputMessage, ExitMessage

type ExecMessage struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Workdir string   `json:"workdir"`
	Env     []string `json:"env"`
	UID     *int     `json:"uid,omitempty"`
	GID     *int     `json:"gid,omitempty"`
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

type NetworkConfigMessage struct {
	GuestIP string `json:"guest_ip"`
	Gateway string `json:"gateway"`
	DNS     string `json:"dns"`
}

// InputMessage sends stdin data from host to guest.
type InputMessage struct {
	Data string `json:"data"` // base64-encoded
}

type MountConfigMessage struct {
	Mounts []MountInfo `json:"mounts"`
}

type MountInfo struct {
	GuestPath string `json:"guest_path"`
	VsockPort uint32 `json:"vsock_port"`
	Mode      string `json:"mode"`
}

type MountsReadyMessage struct{}

// ResizeMessage notifies the guest of terminal size changes.
type ResizeMessage struct {
	Rows int `json:"rows"`
	Cols int `json:"cols"`
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
	case *NetworkConfigMessage:
		typeName = "network_config"
	case *InputMessage:
		typeName = "input"
	case *ResizeMessage:
		typeName = "resize"
	case *FileRequest:
		typeName = "file_request"
	case *FileResponse:
		typeName = "file_response"
	case *MountConfigMessage:
		typeName = "mount_config"
	case *MountsReadyMessage:
		typeName = "mounts_ready"
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
	case "network_config":
		var m NetworkConfigMessage
		if err := json.Unmarshal(env.Data, &m); err != nil {
			return nil, err
		}
		return &m, nil
	case "input":
		var m InputMessage
		if err := json.Unmarshal(env.Data, &m); err != nil {
			return nil, err
		}
		return &m, nil
	case "resize":
		var m ResizeMessage
		if err := json.Unmarshal(env.Data, &m); err != nil {
			return nil, err
		}
		return &m, nil
	case "file_request":
		var m FileRequest
		if err := json.Unmarshal(env.Data, &m); err != nil {
			return nil, err
		}
		return &m, nil
	case "file_response":
		var m FileResponse
		if err := json.Unmarshal(env.Data, &m); err != nil {
			return nil, err
		}
		return &m, nil
	case "mount_config":
		var m MountConfigMessage
		if err := json.Unmarshal(env.Data, &m); err != nil {
			return nil, err
		}
		return &m, nil
	case "mounts_ready":
		return &MountsReadyMessage{}, nil
	default:
		return nil, fmt.Errorf("unknown message type: %q", env.Type)
	}
}
