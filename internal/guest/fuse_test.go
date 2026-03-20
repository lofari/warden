package guest

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/winler/warden/internal/fileserver"
)

func TestFileClientStatViaServer(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("content"), 0o644)

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	srv := fileserver.NewServer(dir, false, nil)
	go srv.Serve(serverConn)

	client := NewFileClient(clientConn)
	stat, err := client.Stat("test.txt")
	if err != nil {
		t.Fatal(err)
	}
	if stat.Size != 7 {
		t.Fatalf("expected size 7, got %d", stat.Size)
	}
}

func TestFileClientReadDir(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("world"), 0o644)
	os.Mkdir(filepath.Join(dir, "subdir"), 0o755)

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	srv := fileserver.NewServer(dir, false, nil)
	go srv.Serve(serverConn)

	client := NewFileClient(clientConn)
	entries, err := client.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
}

func TestFileClientReadWrite(t *testing.T) {
	dir := t.TempDir()

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	srv := fileserver.NewServer(dir, false, nil)
	go srv.Serve(serverConn)

	client := NewFileClient(clientConn)

	// Create and write
	handle, err := client.Create("hello.txt", 0o644)
	if err != nil {
		t.Fatal(err)
	}
	n, err := client.Write(handle, []byte("hello world"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if n != 11 {
		t.Fatalf("expected 11 bytes written, got %d", n)
	}
	if err := client.Close(handle); err != nil {
		t.Fatal(err)
	}

	// Open and read back
	handle2, err := client.Open("hello.txt", os.O_RDONLY)
	if err != nil {
		t.Fatal(err)
	}
	data, err := client.Read(handle2, 0, 11)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello world" {
		t.Fatalf("expected 'hello world', got %q", string(data))
	}
	client.Close(handle2)
}

func TestFileClientReturnsErrorOnConnectionClose(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	client := NewFileClient(clientConn)

	// Close the server side to simulate connection drop
	serverConn.Close()

	// Give readLoop time to detect the close
	time.Sleep(50 * time.Millisecond)

	// This should return an error, not hang
	_, err := client.Stat("anything")
	if err == nil {
		t.Fatal("expected error on closed connection")
	}
}

func TestMountFUSESkipsWithoutDev(t *testing.T) {
	if _, err := os.Stat("/dev/fuse"); os.IsNotExist(err) {
		t.Skip("/dev/fuse not available")
	}

	dir := t.TempDir()
	mountDir := t.TempDir()

	hostDir := t.TempDir()
	os.WriteFile(filepath.Join(hostDir, "hello.txt"), []byte("fuse test"), 0o644)

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	srv := fileserver.NewServer(hostDir, false, nil)
	go srv.Serve(serverConn)

	client := NewFileClient(clientConn)

	unmount, err := MountFUSE(mountDir, client)
	if err != nil {
		t.Skipf("MountFUSE failed (likely no FUSE support): %v", err)
	}
	defer unmount()

	_ = dir // avoid unused var error
	data, err := os.ReadFile(filepath.Join(mountDir, "hello.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "fuse test" {
		t.Fatalf("expected 'fuse test', got %q", string(data))
	}
}
