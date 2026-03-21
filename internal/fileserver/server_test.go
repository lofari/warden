package fileserver

import (
	"encoding/base64"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/winler/warden/internal/protocol"
)

func TestServerStatFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0o644)
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	srv := NewServer(dir, false, nil)
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
	srv := NewServer(dir, false, nil)
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
	srv := NewServer(dir, false, nil)
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
	srv := NewServer(dir, true, nil)
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
	srv := NewServer(dir, false, nil)
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
	srv := NewServer(dir, false, nil)
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
	srv := NewServer(dir, false, nil)
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

	srv := NewServer(dir, true, nil) // read-only
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

	srv := NewServer(dir, false, nil)
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
	srv := NewServer(dir, false, nil)
	go srv.Serve(serverConn)

	// Try to stat via the symlink
	protocol.WriteMessage(clientConn, &protocol.FileRequest{ID: 1, Op: protocol.OpStat, Path: "escape"})
	raw, _ := protocol.ReadMessage(clientConn)
	resp := raw.(*protocol.FileResponse)
	if resp.Error == "" {
		t.Fatal("expected path traversal via symlink to be blocked")
	}
}

func TestServerWriteAtOffsetZero(t *testing.T) {
	dir := t.TempDir()
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	srv := NewServer(dir, false, nil)
	go srv.Serve(serverConn)

	// Create file
	protocol.WriteMessage(clientConn, &protocol.FileRequest{ID: 1, Op: protocol.OpCreate, Path: "test.txt", Mode: 0o644})
	raw, _ := protocol.ReadMessage(clientConn)
	handle := raw.(*protocol.FileResponse).Handle

	// Write "hello" at offset 0
	data1 := base64.StdEncoding.EncodeToString([]byte("hello"))
	protocol.WriteMessage(clientConn, &protocol.FileRequest{ID: 2, Op: protocol.OpWrite, Handle: handle, Data: data1, Offset: 0})
	protocol.ReadMessage(clientConn)

	// Write "world" at offset 0 again — should OVERWRITE, not append
	data2 := base64.StdEncoding.EncodeToString([]byte("world"))
	protocol.WriteMessage(clientConn, &protocol.FileRequest{ID: 3, Op: protocol.OpWrite, Handle: handle, Data: data2, Offset: 0})
	protocol.ReadMessage(clientConn)

	// Close
	protocol.WriteMessage(clientConn, &protocol.FileRequest{ID: 4, Op: protocol.OpClose, Handle: handle})
	protocol.ReadMessage(clientConn)

	// Verify: file should be "world" (5 bytes), not "worldhello" or "helloworld"
	got, _ := os.ReadFile(filepath.Join(dir, "test.txt"))
	if string(got) != "world" {
		t.Fatalf("expected 'world', got %q (len=%d)", got, len(got))
	}
}

func TestServerDenyListBlocksAccess(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte("SECRET=x"), 0o644)
	os.WriteFile(filepath.Join(dir, "app.go"), []byte("package main"), 0o644)

	ac := NewAccessControl(nil, nil, nil) // built-in defaults
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	srv := NewServer(dir, false, ac)
	go srv.Serve(serverConn)

	// .env should be blocked
	protocol.WriteMessage(clientConn, &protocol.FileRequest{ID: 1, Op: protocol.OpStat, Path: ".env"})
	raw, _ := protocol.ReadMessage(clientConn)
	resp := raw.(*protocol.FileResponse)
	if resp.Error == "" {
		t.Fatal("expected .env to be denied")
	}
	if !strings.Contains(resp.Error, "denied") {
		t.Fatalf("expected 'denied' error, got: %s", resp.Error)
	}

	// app.go should be accessible
	protocol.WriteMessage(clientConn, &protocol.FileRequest{ID: 2, Op: protocol.OpStat, Path: "app.go"})
	raw, _ = protocol.ReadMessage(clientConn)
	resp = raw.(*protocol.FileResponse)
	if resp.Error != "" {
		t.Fatalf("app.go should be accessible: %s", resp.Error)
	}
}

func TestServerDenyListFiltersReaddir(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte("SECRET=x"), 0o644)
	os.WriteFile(filepath.Join(dir, "app.go"), []byte("package main"), 0o644)

	ac := NewAccessControl(nil, nil, nil)
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	srv := NewServer(dir, false, ac)
	go srv.Serve(serverConn)

	protocol.WriteMessage(clientConn, &protocol.FileRequest{ID: 1, Op: protocol.OpReadDir, Path: "."})
	raw, _ := protocol.ReadMessage(clientConn)
	resp := raw.(*protocol.FileResponse)
	if resp.Error != "" {
		t.Fatal(resp.Error)
	}
	for _, e := range resp.Entries {
		if e.Name == ".env" {
			t.Fatal(".env should be filtered from readdir results")
		}
	}
	found := false
	for _, e := range resp.Entries {
		if e.Name == "app.go" {
			found = true
		}
	}
	if !found {
		t.Fatal("app.go should be in readdir results")
	}
}

func TestServerReadOnlyOverrideBlocksWrite(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".git", "hooks"), 0o755)
	os.WriteFile(filepath.Join(dir, ".git", "hooks", "pre-commit"), []byte("#!/bin/sh"), 0o755)

	ac := NewAccessControl(nil, nil, []string{".git/hooks"})
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	srv := NewServer(dir, false, ac)
	go srv.Serve(serverConn)

	// Reading should work
	protocol.WriteMessage(clientConn, &protocol.FileRequest{ID: 1, Op: protocol.OpStat, Path: ".git/hooks/pre-commit"})
	raw, _ := protocol.ReadMessage(clientConn)
	resp := raw.(*protocol.FileResponse)
	if resp.Error != "" {
		t.Fatalf("should be able to stat read-only path: %s", resp.Error)
	}

	// Writing should be blocked
	protocol.WriteMessage(clientConn, &protocol.FileRequest{ID: 2, Op: protocol.OpCreate, Path: ".git/hooks/post-commit", Mode: 0o755})
	raw, _ = protocol.ReadMessage(clientConn)
	resp = raw.(*protocol.FileResponse)
	if resp.Error == "" {
		t.Fatal("expected write to read-only path to be blocked")
	}
}

func TestServerDenyListBlocksSymlinkBypass(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte("SECRET=x"), 0o644)
	// Create a symlink that points to .env
	os.Symlink(".env", filepath.Join(dir, "sneaky-link"))

	ac := NewAccessControl(nil, nil, nil)
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	srv := NewServer(dir, false, ac)
	go srv.Serve(serverConn)

	// Direct access blocked
	protocol.WriteMessage(clientConn, &protocol.FileRequest{ID: 1, Op: protocol.OpStat, Path: ".env"})
	raw, _ := protocol.ReadMessage(clientConn)
	resp := raw.(*protocol.FileResponse)
	if resp.Error == "" {
		t.Fatal("direct .env access should be denied")
	}

	// Symlink bypass also blocked
	protocol.WriteMessage(clientConn, &protocol.FileRequest{ID: 2, Op: protocol.OpStat, Path: "sneaky-link"})
	raw, _ = protocol.ReadMessage(clientConn)
	resp = raw.(*protocol.FileResponse)
	if resp.Error == "" {
		t.Fatal("symlink to .env should also be denied")
	}
}

func TestResolvePathReturnsRealPath(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "real-file.txt")
	os.WriteFile(target, []byte("hello"), 0o644)
	link := filepath.Join(root, "link")
	os.Symlink(target, link)

	ac := NewAccessControl(nil, nil, nil)
	srv := NewServer(root, false, ac)

	resolved, err := srv.resolvePath("link")
	if err != nil {
		t.Fatal(err)
	}
	if resolved != target {
		t.Errorf("expected real path %s, got %s", target, resolved)
	}
}

func TestHandleWriteRespectsPerPathReadOnly(t *testing.T) {
	root := t.TempDir()
	testFile := filepath.Join(root, "protected.txt")
	os.WriteFile(testFile, []byte("original"), 0o644)

	ac := NewAccessControl(nil, nil, []string{"protected.txt"})
	srv := NewServer(root, false, ac) // mount is rw, but file is pattern-read-only

	// Open the file read-only (should succeed)
	openReq := &protocol.FileRequest{
		Op:   protocol.OpOpen,
		Path: "protected.txt",
	}
	openResp := srv.dispatch(openReq)
	if openResp.Error != "" {
		t.Fatalf("open failed: %s", openResp.Error)
	}

	// Attempt write via the handle — should be rejected
	writeReq := &protocol.FileRequest{
		Op:     protocol.OpWrite,
		Handle: openResp.Handle,
		Data:   base64.StdEncoding.EncodeToString([]byte("hacked")),
		Offset: 0,
	}
	writeResp := srv.dispatch(writeReq)
	if writeResp.Error == "" {
		t.Error("expected write to be rejected for read-only-patterned file")
	}

	// Close handle
	srv.dispatch(&protocol.FileRequest{Op: protocol.OpClose, Handle: openResp.Handle})
}

func TestServerReadOnlyRenameBlocked(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Makefile"), []byte("all: build"), 0o644)
	os.WriteFile(filepath.Join(dir, "temp.txt"), []byte("temp"), 0o644)

	ac := NewAccessControl(nil, nil, []string{"Makefile"})
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	srv := NewServer(dir, false, ac)
	go srv.Serve(serverConn)

	// Renaming a read-only source should be blocked
	protocol.WriteMessage(clientConn, &protocol.FileRequest{ID: 1, Op: protocol.OpRename, Path: "Makefile", NewPath: "Makefile.bak"})
	raw, _ := protocol.ReadMessage(clientConn)
	resp := raw.(*protocol.FileResponse)
	if resp.Error == "" {
		t.Fatal("renaming a read-only source should be blocked")
	}

	// Renaming INTO a read-only destination should also be blocked
	protocol.WriteMessage(clientConn, &protocol.FileRequest{ID: 2, Op: protocol.OpRename, Path: "temp.txt", NewPath: "Makefile"})
	raw, _ = protocol.ReadMessage(clientConn)
	resp = raw.(*protocol.FileResponse)
	if resp.Error == "" {
		t.Fatal("renaming into a read-only destination should be blocked")
	}
}
