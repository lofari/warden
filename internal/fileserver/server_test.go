package fileserver

import (
	"encoding/base64"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/winler/warden/internal/protocol"
)

func TestServerStatFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0o644)
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	srv := NewServer(dir, false)
	go srv.Serve(serverConn)
	req := &protocol.FileRequest{ID: 1, Op: protocol.OpStat, Path: "test.txt"}
	protocol.WriteMessage(clientConn, req)
	raw, _ := protocol.ReadMessage(clientConn)
	resp := raw.(*protocol.FileResponse)
	if resp.Error != "" {
		t.Fatal(resp.Error)
	}
	if resp.Stat.Size != 5 {
		t.Fatalf("expected size 5, got %d", resp.Stat.Size)
	}
}

func TestServerReadWrite(t *testing.T) {
	dir := t.TempDir()
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	srv := NewServer(dir, false)
	go srv.Serve(serverConn)

	// Create
	protocol.WriteMessage(clientConn, &protocol.FileRequest{ID: 1, Op: protocol.OpCreate, Path: "new.txt", Mode: 0o644})
	raw, _ := protocol.ReadMessage(clientConn)
	resp := raw.(*protocol.FileResponse)
	if resp.Error != "" {
		t.Fatal(resp.Error)
	}
	handle := resp.Handle

	// Write
	data := base64.StdEncoding.EncodeToString([]byte("file contents"))
	protocol.WriteMessage(clientConn, &protocol.FileRequest{ID: 2, Op: protocol.OpWrite, Handle: handle, Data: data})
	raw, _ = protocol.ReadMessage(clientConn)
	resp = raw.(*protocol.FileResponse)
	if resp.Written != 13 {
		t.Fatalf("expected 13 bytes, got %d", resp.Written)
	}

	// Close
	protocol.WriteMessage(clientConn, &protocol.FileRequest{ID: 3, Op: protocol.OpClose, Handle: handle})
	protocol.ReadMessage(clientConn)

	// Verify on disk
	got, _ := os.ReadFile(filepath.Join(dir, "new.txt"))
	if string(got) != "file contents" {
		t.Fatalf("mismatch: %q", got)
	}
}

func TestServerPathTraversalBlocked(t *testing.T) {
	dir := t.TempDir()
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	srv := NewServer(dir, false)
	go srv.Serve(serverConn)
	protocol.WriteMessage(clientConn, &protocol.FileRequest{ID: 1, Op: protocol.OpStat, Path: "../../etc/passwd"})
	raw, _ := protocol.ReadMessage(clientConn)
	resp := raw.(*protocol.FileResponse)
	if resp.Error == "" {
		t.Fatal("expected path traversal error")
	}
}

func TestServerReadOnly(t *testing.T) {
	dir := t.TempDir()
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	srv := NewServer(dir, true)
	go srv.Serve(serverConn)

	// Attempt to create a file on a read-only server
	protocol.WriteMessage(clientConn, &protocol.FileRequest{ID: 1, Op: protocol.OpCreate, Path: "blocked.txt", Mode: 0o644})
	raw, _ := protocol.ReadMessage(clientConn)
	resp := raw.(*protocol.FileResponse)
	if resp.Error != "read-only mount" {
		t.Fatalf("expected read-only error, got %q", resp.Error)
	}
}

func TestServerReadDir(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o644)
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	srv := NewServer(dir, false)
	go srv.Serve(serverConn)

	protocol.WriteMessage(clientConn, &protocol.FileRequest{ID: 1, Op: protocol.OpReadDir, Path: "."})
	raw, _ := protocol.ReadMessage(clientConn)
	resp := raw.(*protocol.FileResponse)
	if resp.Error != "" {
		t.Fatal(resp.Error)
	}
	if len(resp.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(resp.Entries))
	}
}

func TestServerMkdirAndRemove(t *testing.T) {
	dir := t.TempDir()
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	srv := NewServer(dir, false)
	go srv.Serve(serverConn)

	// Mkdir
	protocol.WriteMessage(clientConn, &protocol.FileRequest{ID: 1, Op: protocol.OpMkdir, Path: "newdir", Mode: 0o755})
	raw, _ := protocol.ReadMessage(clientConn)
	resp := raw.(*protocol.FileResponse)
	if resp.Error != "" {
		t.Fatal(resp.Error)
	}

	// Verify on disk
	fi, err := os.Stat(filepath.Join(dir, "newdir"))
	if err != nil || !fi.IsDir() {
		t.Fatal("expected newdir to be a directory")
	}

	// Remove
	protocol.WriteMessage(clientConn, &protocol.FileRequest{ID: 2, Op: protocol.OpRemove, Path: "newdir"})
	raw, _ = protocol.ReadMessage(clientConn)
	resp = raw.(*protocol.FileResponse)
	if resp.Error != "" {
		t.Fatal(resp.Error)
	}

	// Verify removed
	if _, err := os.Stat(filepath.Join(dir, "newdir")); !os.IsNotExist(err) {
		t.Fatal("expected newdir to be removed")
	}
}

func TestServerRename(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "old.txt"), []byte("data"), 0o644)
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	srv := NewServer(dir, false)
	go srv.Serve(serverConn)

	protocol.WriteMessage(clientConn, &protocol.FileRequest{ID: 1, Op: protocol.OpRename, Path: "old.txt", NewPath: "new.txt"})
	raw, _ := protocol.ReadMessage(clientConn)
	resp := raw.(*protocol.FileResponse)
	if resp.Error != "" {
		t.Fatal(resp.Error)
	}

	if _, err := os.Stat(filepath.Join(dir, "new.txt")); err != nil {
		t.Fatal("expected new.txt to exist")
	}
	if _, err := os.Stat(filepath.Join(dir, "old.txt")); !os.IsNotExist(err) {
		t.Fatal("expected old.txt to be gone")
	}
}

func TestServerReadOnlyBlocksOpenWithWriteFlags(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0o644)

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	srv := NewServer(dir, true) // read-only
	go srv.Serve(serverConn)

	// Try to open with write flags
	protocol.WriteMessage(clientConn, &protocol.FileRequest{
		ID: 1, Op: protocol.OpOpen, Path: "test.txt", Flags: os.O_RDWR,
	})
	raw, _ := protocol.ReadMessage(clientConn)
	resp := raw.(*protocol.FileResponse)
	if resp.Error == "" {
		t.Fatal("expected read-only error for O_RDWR open")
	}
}

func TestServerSymlinkTargetEscapeBlocked(t *testing.T) {
	dir := t.TempDir()
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	srv := NewServer(dir, false)
	go srv.Serve(serverConn)

	// Absolute target outside root
	protocol.WriteMessage(clientConn, &protocol.FileRequest{
		ID: 1, Op: protocol.OpSymlink, Path: "evil-link", NewPath: "/etc/shadow",
	})
	raw, _ := protocol.ReadMessage(clientConn)
	resp := raw.(*protocol.FileResponse)
	if resp.Error == "" {
		t.Fatal("expected error for absolute symlink target outside root")
	}

	// Relative target that escapes
	protocol.WriteMessage(clientConn, &protocol.FileRequest{
		ID: 2, Op: protocol.OpSymlink, Path: "evil-link2", NewPath: "../../../../etc/shadow",
	})
	raw, _ = protocol.ReadMessage(clientConn)
	resp = raw.(*protocol.FileResponse)
	if resp.Error == "" {
		t.Fatal("expected error for relative symlink target escaping root")
	}
}

func TestServerSymlinkTraversalBlocked(t *testing.T) {
	dir := t.TempDir()
	// Create a symlink inside dir that points outside
	linkPath := filepath.Join(dir, "escape")
	os.Symlink("/etc", linkPath)

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	srv := NewServer(dir, false)
	go srv.Serve(serverConn)

	// Try to stat via the symlink
	protocol.WriteMessage(clientConn, &protocol.FileRequest{ID: 1, Op: protocol.OpStat, Path: "escape"})
	raw, _ := protocol.ReadMessage(clientConn)
	resp := raw.(*protocol.FileResponse)
	if resp.Error == "" {
		t.Fatal("expected path traversal via symlink to be blocked")
	}
}
