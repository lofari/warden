package fileserver

import (
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/winler/warden/internal/protocol"
)

// Server serves file operations against a root directory over a connection.
type Server struct {
	root       string
	readOnly   bool
	ac         *AccessControl
	handles    sync.Map
	nextID     atomic.Uint64
	maxHandles int
	openCount  atomic.Int32
}

// NewServer creates a new file server rooted at root.
func NewServer(root string, readOnly bool, ac *AccessControl) *Server {
	return &Server{root: root, readOnly: readOnly, ac: ac, maxHandles: 1024}
}

// requireWritePath checks both global read-only and path-level read-only overrides.
func (s *Server) requireWritePath(relPath string) *protocol.FileResponse {
	if s.readOnly {
		return &protocol.FileResponse{Error: "read-only mount"}
	}
	if s.ac.IsReadOnly(relPath) {
		return &protocol.FileResponse{Error: "read-only path"}
	}
	return nil
}

// relPath extracts the relative path from an absolute resolved path.
func (s *Server) relPath(absPath string) string {
	return strings.TrimPrefix(absPath, s.root+string(os.PathSeparator))
}

// Serve reads FileRequests from conn, dispatches them, and writes FileResponses.
func (s *Server) Serve(conn net.Conn) {
	defer conn.Close()
	for {
		raw, err := protocol.ReadMessage(conn)
		if err != nil {
			if err == io.EOF {
				return
			}
			return
		}
		req, ok := raw.(*protocol.FileRequest)
		if !ok {
			return
		}
		resp := s.dispatch(req)
		resp.ID = req.ID
		if err := protocol.WriteMessage(conn, resp); err != nil {
			return
		}
	}
}

func (s *Server) dispatch(req *protocol.FileRequest) *protocol.FileResponse {
	switch req.Op {
	case protocol.OpStat:
		return s.handleStat(req)
	case protocol.OpReadDir:
		return s.handleReadDir(req)
	case protocol.OpOpen:
		return s.handleOpen(req)
	case protocol.OpCreate:
		return s.handleCreate(req)
	case protocol.OpRead:
		return s.handleRead(req)
	case protocol.OpWrite:
		return s.handleWrite(req)
	case protocol.OpClose:
		return s.handleClose(req)
	case protocol.OpFlush:
		return s.handleFlush(req)
	case protocol.OpMkdir:
		return s.handleMkdir(req)
	case protocol.OpRemove:
		return s.handleRemove(req)
	case protocol.OpRename:
		return s.handleRename(req)
	case protocol.OpTrunc:
		return s.handleTruncate(req)
	case protocol.OpSymlink:
		return s.handleSymlink(req)
	case protocol.OpReadlink:
		return s.handleReadlink(req)
	case protocol.OpChmod:
		return s.handleChmod(req)
	default:
		return &protocol.FileResponse{Error: fmt.Sprintf("unknown op: %s", req.Op)}
	}
}

// resolvePath resolves a client-supplied path against the root, blocking traversal and denied paths.
func (s *Server) resolvePath(path string) (string, error) {
	clean := filepath.Clean(filepath.Join(s.root, path))
	if !strings.HasPrefix(clean, s.root+string(os.PathSeparator)) && clean != s.root {
		return "", fmt.Errorf("path traversal blocked: %s", path)
	}

	// Deny check on requested relative path
	relPath := strings.TrimPrefix(clean, s.root+string(os.PathSeparator))
	if s.ac.IsDenied(relPath) {
		return "", fmt.Errorf("access denied: %s", path)
	}

	real, err := filepath.EvalSymlinks(clean)
	if err != nil {
		if os.IsNotExist(err) {
			parentReal, err2 := filepath.EvalSymlinks(filepath.Dir(clean))
			if err2 != nil {
				return "", fmt.Errorf("path traversal blocked: %s", path)
			}
			if !strings.HasPrefix(parentReal, s.root) {
				return "", fmt.Errorf("path traversal blocked via symlink: %s", path)
			}
			return clean, nil
		}
		return "", err
	}
	if !strings.HasPrefix(real, s.root+string(os.PathSeparator)) && real != s.root {
		return "", fmt.Errorf("path traversal blocked via symlink: %s", path)
	}

	// Deny check on symlink-resolved path
	realRel := strings.TrimPrefix(real, s.root+string(os.PathSeparator))
	if realRel != relPath && s.ac.IsDenied(realRel) {
		return "", fmt.Errorf("access denied: %s", path)
	}

	return clean, nil
}

func fileInfoToStat(fi os.FileInfo) *protocol.FileStat {
	st := &protocol.FileStat{
		Name:    fi.Name(),
		Size:    fi.Size(),
		Mode:    uint32(fi.Mode()),
		ModTime: fi.ModTime().Unix(),
		IsDir:   fi.IsDir(),
	}
	if sys, ok := fi.Sys().(*syscall.Stat_t); ok {
		st.Nlink = uint32(sys.Nlink)
		st.Uid = sys.Uid
		st.Gid = sys.Gid
	}
	return st
}

func (s *Server) handleStat(req *protocol.FileRequest) *protocol.FileResponse {
	path, err := s.resolvePath(req.Path)
	if err != nil {
		return &protocol.FileResponse{Error: err.Error()}
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return &protocol.FileResponse{Error: err.Error()}
	}
	return &protocol.FileResponse{Stat: fileInfoToStat(fi)}
}

func (s *Server) handleReadDir(req *protocol.FileRequest) *protocol.FileResponse {
	path, err := s.resolvePath(req.Path)
	if err != nil {
		return &protocol.FileResponse{Error: err.Error()}
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return &protocol.FileResponse{Error: err.Error()}
	}

	// Compute relative path of the directory for deny-list filtering
	dirRel := strings.TrimPrefix(path, s.root+string(os.PathSeparator))

	var dirEntries []protocol.DirEntry
	for _, e := range entries {
		// Filter denied entries
		entryRel := e.Name()
		if dirRel != "" && dirRel != "." {
			entryRel = dirRel + "/" + e.Name()
		}
		if s.ac.IsDenied(entryRel) {
			continue
		}

		info, err := e.Info()
		if err != nil {
			continue
		}
		dirEntries = append(dirEntries, protocol.DirEntry{
			Name:  e.Name(),
			IsDir: e.IsDir(),
			Mode:  uint32(info.Mode()),
		})
	}
	return &protocol.FileResponse{Entries: dirEntries}
}

func (s *Server) handleOpen(req *protocol.FileRequest) *protocol.FileResponse {
	path, err := s.resolvePath(req.Path)
	if err != nil {
		return &protocol.FileResponse{Error: err.Error()}
	}
	flags := req.Flags
	if flags == 0 {
		flags = os.O_RDONLY
	}
	// Check write access for write flags
	if flags&(os.O_WRONLY|os.O_RDWR|os.O_APPEND|os.O_CREATE|os.O_TRUNC) != 0 {
		if r := s.requireWritePath(s.relPath(path)); r != nil {
			return r
		}
	}
	if int(s.openCount.Load()) >= s.maxHandles {
		return &protocol.FileResponse{Error: "too many open handles"}
	}
	f, err := os.OpenFile(path, flags, os.FileMode(req.Mode))
	if err != nil {
		return &protocol.FileResponse{Error: err.Error()}
	}
	id := s.nextID.Add(1)
	s.handles.Store(id, f)
	s.openCount.Add(1)
	return &protocol.FileResponse{Handle: id}
}

func (s *Server) handleCreate(req *protocol.FileRequest) *protocol.FileResponse {
	path, err := s.resolvePath(req.Path)
	if err != nil {
		return &protocol.FileResponse{Error: err.Error()}
	}
	if r := s.requireWritePath(s.relPath(path)); r != nil {
		return r
	}
	if int(s.openCount.Load()) >= s.maxHandles {
		return &protocol.FileResponse{Error: "too many open handles"}
	}
	mode := os.FileMode(req.Mode)
	if mode == 0 {
		mode = 0o644
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return &protocol.FileResponse{Error: err.Error()}
	}
	id := s.nextID.Add(1)
	s.handles.Store(id, f)
	s.openCount.Add(1)
	return &protocol.FileResponse{Handle: id}
}

const maxReadSize = 4 * 1024 * 1024 // 4 MiB

func (s *Server) handleRead(req *protocol.FileRequest) *protocol.FileResponse {
	v, ok := s.handles.Load(req.Handle)
	if !ok {
		return &protocol.FileResponse{Error: "invalid handle"}
	}
	f := v.(*os.File)
	size := req.Size
	if size <= 0 {
		size = 64 * 1024
	}
	if size > maxReadSize {
		size = maxReadSize
	}
	buf := make([]byte, size)
	// Always use ReadAt — FUSE always supplies explicit offsets
	n, err := f.ReadAt(buf, req.Offset)
	if n == 0 && err != nil {
		return &protocol.FileResponse{Error: err.Error()}
	}
	encoded := base64.StdEncoding.EncodeToString(buf[:n])
	return &protocol.FileResponse{Data: encoded}
}

func (s *Server) handleWrite(req *protocol.FileRequest) *protocol.FileResponse {
	if s.readOnly {
		return &protocol.FileResponse{Error: "read-only mount"}
	}
	v, ok := s.handles.Load(req.Handle)
	if !ok {
		return &protocol.FileResponse{Error: "invalid handle"}
	}
	f := v.(*os.File)
	data, err := base64.StdEncoding.DecodeString(req.Data)
	if err != nil {
		return &protocol.FileResponse{Error: err.Error()}
	}
	// Always use WriteAt — FUSE always supplies explicit offsets
	n, err := f.WriteAt(data, req.Offset)
	if err != nil {
		return &protocol.FileResponse{Error: err.Error()}
	}
	return &protocol.FileResponse{Written: n}
}

func (s *Server) handleClose(req *protocol.FileRequest) *protocol.FileResponse {
	v, ok := s.handles.LoadAndDelete(req.Handle)
	if !ok {
		return &protocol.FileResponse{Error: "invalid handle"}
	}
	f := v.(*os.File)
	s.openCount.Add(-1)
	if err := f.Close(); err != nil {
		return &protocol.FileResponse{Error: err.Error()}
	}
	return &protocol.FileResponse{}
}

func (s *Server) handleFlush(req *protocol.FileRequest) *protocol.FileResponse {
	v, ok := s.handles.Load(req.Handle)
	if !ok {
		return &protocol.FileResponse{Error: "invalid handle"}
	}
	f := v.(*os.File)
	if err := f.Sync(); err != nil {
		return &protocol.FileResponse{Error: err.Error()}
	}
	return &protocol.FileResponse{}
}

func (s *Server) handleMkdir(req *protocol.FileRequest) *protocol.FileResponse {
	path, err := s.resolvePath(req.Path)
	if err != nil {
		return &protocol.FileResponse{Error: err.Error()}
	}
	if r := s.requireWritePath(s.relPath(path)); r != nil {
		return r
	}
	mode := os.FileMode(req.Mode)
	if mode == 0 {
		mode = 0o755
	}
	if err := os.Mkdir(path, mode); err != nil {
		return &protocol.FileResponse{Error: err.Error()}
	}
	return &protocol.FileResponse{}
}

func (s *Server) handleRemove(req *protocol.FileRequest) *protocol.FileResponse {
	path, err := s.resolvePath(req.Path)
	if err != nil {
		return &protocol.FileResponse{Error: err.Error()}
	}
	if r := s.requireWritePath(s.relPath(path)); r != nil {
		return r
	}
	if err := os.Remove(path); err != nil {
		return &protocol.FileResponse{Error: err.Error()}
	}
	return &protocol.FileResponse{}
}

func (s *Server) handleRename(req *protocol.FileRequest) *protocol.FileResponse {
	oldPath, err := s.resolvePath(req.Path)
	if err != nil {
		return &protocol.FileResponse{Error: err.Error()}
	}
	newPath, err := s.resolvePath(req.NewPath)
	if err != nil {
		return &protocol.FileResponse{Error: err.Error()}
	}
	// Check both source and destination for write permission
	if r := s.requireWritePath(s.relPath(oldPath)); r != nil {
		return r
	}
	if r := s.requireWritePath(s.relPath(newPath)); r != nil {
		return r
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		return &protocol.FileResponse{Error: err.Error()}
	}
	return &protocol.FileResponse{}
}

func (s *Server) handleTruncate(req *protocol.FileRequest) *protocol.FileResponse {
	path, err := s.resolvePath(req.Path)
	if err != nil {
		return &protocol.FileResponse{Error: err.Error()}
	}
	if r := s.requireWritePath(s.relPath(path)); r != nil {
		return r
	}
	if err := os.Truncate(path, req.Offset); err != nil {
		return &protocol.FileResponse{Error: err.Error()}
	}
	return &protocol.FileResponse{}
}

func (s *Server) handleSymlink(req *protocol.FileRequest) *protocol.FileResponse {
	linkPath, err := s.resolvePath(req.Path)
	if err != nil {
		return &protocol.FileResponse{Error: err.Error()}
	}
	if r := s.requireWritePath(s.relPath(linkPath)); r != nil {
		return r
	}

	// Validate symlink target — must not escape root
	target := req.NewPath
	if filepath.IsAbs(target) {
		// Absolute target: must be within root when evaluated as a real path
		clean := filepath.Clean(target)
		if !strings.HasPrefix(clean, s.root+string(os.PathSeparator)) && clean != s.root {
			return &protocol.FileResponse{Error: "symlink target outside root"}
		}
	} else {
		// Relative target: resolve relative to link's parent directory
		linkDir := filepath.Dir(linkPath)
		resolved := filepath.Clean(filepath.Join(linkDir, target))
		if !strings.HasPrefix(resolved, s.root+string(os.PathSeparator)) && resolved != s.root {
			return &protocol.FileResponse{Error: "symlink target escapes root"}
		}
	}

	if err := os.Symlink(target, linkPath); err != nil {
		return &protocol.FileResponse{Error: err.Error()}
	}
	return &protocol.FileResponse{}
}

func (s *Server) handleReadlink(req *protocol.FileRequest) *protocol.FileResponse {
	path, err := s.resolvePath(req.Path)
	if err != nil {
		return &protocol.FileResponse{Error: err.Error()}
	}
	target, err := os.Readlink(path)
	if err != nil {
		return &protocol.FileResponse{Error: err.Error()}
	}
	return &protocol.FileResponse{Data: target}
}

func (s *Server) handleChmod(req *protocol.FileRequest) *protocol.FileResponse {
	path, err := s.resolvePath(req.Path)
	if err != nil {
		return &protocol.FileResponse{Error: err.Error()}
	}
	if r := s.requireWritePath(s.relPath(path)); r != nil {
		return r
	}
	if err := os.Chmod(path, os.FileMode(req.Mode)); err != nil {
		return &protocol.FileResponse{Error: err.Error()}
	}
	return &protocol.FileResponse{}
}
