package guest

import (
	"encoding/base64"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"github.com/winler/warden/internal/protocol"
)

// FileClient is the protocol client for file operations over vsock.
// It multiplexes concurrent requests over a single connection using request IDs.
type FileClient struct {
	conn    io.ReadWriter
	mu      sync.Mutex
	nextID  atomic.Uint64
	pending sync.Map   // uint64 -> chan *protocol.FileResponse
	done    chan struct{} // closed when readLoop exits
}

// NewFileClient creates a new FileClient and starts the read loop.
func NewFileClient(conn io.ReadWriter) *FileClient {
	c := &FileClient{conn: conn, done: make(chan struct{})}
	go c.readLoop()
	return c
}

func (c *FileClient) readLoop() {
	defer close(c.done)
	for {
		raw, err := protocol.ReadMessage(c.conn)
		if err != nil {
			return
		}
		resp, ok := raw.(*protocol.FileResponse)
		if !ok {
			continue
		}
		if ch, ok := c.pending.LoadAndDelete(resp.ID); ok {
			ch.(chan *protocol.FileResponse) <- resp
		}
	}
}

func (c *FileClient) call(req *protocol.FileRequest) (*protocol.FileResponse, error) {
	req.ID = c.nextID.Add(1)
	ch := make(chan *protocol.FileResponse, 1)
	c.pending.Store(req.ID, ch)

	c.mu.Lock()
	err := protocol.WriteMessage(c.conn, req)
	c.mu.Unlock()
	if err != nil {
		c.pending.Delete(req.ID)
		return nil, err
	}

	select {
	case resp := <-ch:
		if resp.Error != "" {
			return nil, fmt.Errorf("%s", resp.Error)
		}
		return resp, nil
	case <-c.done:
		c.pending.Delete(req.ID)
		return nil, fmt.Errorf("connection closed")
	}
}

// Stat returns metadata about a file or directory.
func (c *FileClient) Stat(path string) (*protocol.FileStat, error) {
	resp, err := c.call(&protocol.FileRequest{Op: protocol.OpStat, Path: path})
	if err != nil {
		return nil, err
	}
	return resp.Stat, nil
}

// ReadDir returns the entries in a directory.
func (c *FileClient) ReadDir(path string) ([]protocol.DirEntry, error) {
	resp, err := c.call(&protocol.FileRequest{Op: protocol.OpReadDir, Path: path})
	if err != nil {
		return nil, err
	}
	return resp.Entries, nil
}

// Open opens a file and returns a handle.
func (c *FileClient) Open(path string, flags int) (uint64, error) {
	resp, err := c.call(&protocol.FileRequest{Op: protocol.OpOpen, Path: path, Flags: flags})
	if err != nil {
		return 0, err
	}
	return resp.Handle, nil
}

// Create creates a new file and returns a handle.
func (c *FileClient) Create(path string, mode uint32) (uint64, error) {
	resp, err := c.call(&protocol.FileRequest{Op: protocol.OpCreate, Path: path, Mode: mode})
	if err != nil {
		return 0, err
	}
	return resp.Handle, nil
}

// Read reads data from a file handle at the given offset.
func (c *FileClient) Read(handle uint64, offset int64, size int) ([]byte, error) {
	resp, err := c.call(&protocol.FileRequest{Op: protocol.OpRead, Handle: handle, Offset: offset, Size: size})
	if err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(resp.Data)
}

// Write writes data to a file handle at the given offset.
func (c *FileClient) Write(handle uint64, data []byte, offset int64) (int, error) {
	resp, err := c.call(&protocol.FileRequest{
		Op:     protocol.OpWrite,
		Handle: handle,
		Data:   base64.StdEncoding.EncodeToString(data),
		Offset: offset,
	})
	if err != nil {
		return 0, err
	}
	return resp.Written, nil
}

// Close closes a file handle.
func (c *FileClient) Close(handle uint64) error {
	_, err := c.call(&protocol.FileRequest{Op: protocol.OpClose, Handle: handle})
	return err
}

// Mkdir creates a new directory.
func (c *FileClient) Mkdir(path string, mode uint32) error {
	_, err := c.call(&protocol.FileRequest{Op: protocol.OpMkdir, Path: path, Mode: mode})
	return err
}

// Remove removes a file or empty directory.
func (c *FileClient) Remove(path string) error {
	_, err := c.call(&protocol.FileRequest{Op: protocol.OpRemove, Path: path})
	return err
}

// Rename renames a file or directory.
func (c *FileClient) Rename(oldPath, newPath string) error {
	_, err := c.call(&protocol.FileRequest{Op: protocol.OpRename, Path: oldPath, NewPath: newPath})
	return err
}

// Truncate truncates a file to the given size.
func (c *FileClient) Truncate(path string, size int64) error {
	_, err := c.call(&protocol.FileRequest{Op: protocol.OpTrunc, Path: path, Offset: size})
	return err
}

// Symlink creates a symbolic link at linkPath pointing to target.
func (c *FileClient) Symlink(target, linkPath string) error {
	_, err := c.call(&protocol.FileRequest{Op: protocol.OpSymlink, Path: linkPath, NewPath: target})
	return err
}

// Readlink returns the target of a symbolic link.
func (c *FileClient) Readlink(path string) (string, error) {
	resp, err := c.call(&protocol.FileRequest{Op: protocol.OpReadlink, Path: path})
	if err != nil {
		return "", err
	}
	return resp.Data, nil
}

// Chmod changes the mode of a file.
func (c *FileClient) Chmod(path string, mode uint32) error {
	_, err := c.call(&protocol.FileRequest{Op: protocol.OpChmod, Path: path, Mode: mode})
	return err
}

// Flush flushes (syncs) a file handle.
func (c *FileClient) Flush(handle uint64) error {
	_, err := c.call(&protocol.FileRequest{Op: protocol.OpFlush, Handle: handle})
	return err
}
