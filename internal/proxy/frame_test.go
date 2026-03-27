package proxy

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestWriteReadFrameRoundTrip(t *testing.T) {
	tests := []struct {
		name      string
		frameType byte
		payload   []byte
	}{
		{"stdin", FrameStdin, []byte("hello stdin")},
		{"stdout", FrameStdout, []byte("hello stdout")},
		{"stderr", FrameStderr, []byte("error message")},
		{"exit code", FrameExit, exitPayload(0)},
		{"exit code nonzero", FrameExit, exitPayload(42)},
		{"resize", FrameResize, []byte(`{"cols":80,"rows":24}`)},
		{"signal", FrameSignal, signalPayload(2)},
		{"empty payload", FrameStdout, []byte{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := WriteFrame(&buf, tt.frameType, tt.payload); err != nil {
				t.Fatalf("WriteFrame: %v", err)
			}
			gotType, gotPayload, err := ReadFrame(&buf)
			if err != nil {
				t.Fatalf("ReadFrame: %v", err)
			}
			if gotType != tt.frameType {
				t.Errorf("type = 0x%02x, want 0x%02x", gotType, tt.frameType)
			}
			if !bytes.Equal(gotPayload, tt.payload) {
				t.Errorf("payload = %q, want %q", gotPayload, tt.payload)
			}
		})
	}
}

func TestReadFrameOversized(t *testing.T) {
	var buf bytes.Buffer
	var header [5]byte
	header[0] = FrameStdout
	binary.LittleEndian.PutUint32(header[1:], MaxFrameSize+1)
	buf.Write(header[:])
	_, _, err := ReadFrame(&buf)
	if err == nil {
		t.Fatal("expected error for oversized frame")
	}
}

func TestMultipleFrames(t *testing.T) {
	var buf bytes.Buffer
	WriteFrame(&buf, FrameStdout, []byte("first"))
	WriteFrame(&buf, FrameStderr, []byte("second"))
	WriteFrame(&buf, FrameExit, exitPayload(0))

	typ1, p1, _ := ReadFrame(&buf)
	typ2, p2, _ := ReadFrame(&buf)
	typ3, p3, _ := ReadFrame(&buf)

	if typ1 != FrameStdout || string(p1) != "first" {
		t.Errorf("frame 1: type=0x%02x payload=%q", typ1, p1)
	}
	if typ2 != FrameStderr || string(p2) != "second" {
		t.Errorf("frame 2: type=0x%02x payload=%q", typ2, p2)
	}
	if typ3 != FrameExit {
		t.Errorf("frame 3: type=0x%02x, want FrameExit", typ3)
	}
	code := int32(binary.LittleEndian.Uint32(p3))
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

func exitPayload(code int32) []byte {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, uint32(code))
	return buf
}

func signalPayload(sig uint32) []byte {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, sig)
	return buf
}
