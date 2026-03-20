package guest

import (
	"context"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
)

// toErrno maps an error from the FileClient to an appropriate syscall errno.
func toErrno(err error) syscall.Errno {
	if err == nil {
		return fs.OK
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "no such file"), strings.Contains(msg, "not found"),
		strings.Contains(msg, "does not exist"):
		return syscall.ENOENT
	case strings.Contains(msg, "permission denied"):
		return syscall.EACCES
	case strings.Contains(msg, "read-only"):
		return syscall.EROFS
	case strings.Contains(msg, "file exists"), strings.Contains(msg, "already exists"):
		return syscall.EEXIST
	case strings.Contains(msg, "not a directory"):
		return syscall.ENOTDIR
	case strings.Contains(msg, "is a directory"):
		return syscall.EISDIR
	case strings.Contains(msg, "not empty"):
		return syscall.ENOTEMPTY
	case strings.Contains(msg, "invalid"):
		return syscall.EINVAL
	case strings.Contains(msg, "connection closed"):
		return syscall.EIO
	default:
		return syscall.EIO
	}
}

// WardenFS implements a FUSE filesystem that forwards all operations to
// the host file server over vsock via a FileClient.
type WardenFS struct {
	fs.Inode
	client *FileClient
	path   string // path relative to the server root
}

// Verify interface compliance at compile time.
var _ = (fs.NodeGetattrer)((*WardenFS)(nil))
var _ = (fs.NodeReaddirer)((*WardenFS)(nil))
var _ = (fs.NodeLookuper)((*WardenFS)(nil))
var _ = (fs.NodeOpener)((*WardenFS)(nil))
var _ = (fs.NodeCreater)((*WardenFS)(nil))
var _ = (fs.NodeMkdirer)((*WardenFS)(nil))
var _ = (fs.NodeUnlinker)((*WardenFS)(nil))
var _ = (fs.NodeRmdirer)((*WardenFS)(nil))
var _ = (fs.NodeRenamer)((*WardenFS)(nil))
var _ = (fs.NodeSymlinker)((*WardenFS)(nil))
var _ = (fs.NodeReadlinker)((*WardenFS)(nil))
var _ = (fs.NodeSetattrer)((*WardenFS)(nil))

// childPath returns the path of a named child relative to server root.
func (n *WardenFS) childPath(name string) string {
	if n.path == "." || n.path == "" {
		return name
	}
	return filepath.Join(n.path, name)
}

// Getattr implements NodeGetattrer.
func (n *WardenFS) Getattr(ctx context.Context, f fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	stat, err := n.client.Stat(n.path)
	if err != nil {
		return toErrno(err)
	}
	out.Mode = stat.Mode
	out.Size = uint64(stat.Size)
	out.Mtime = uint64(stat.ModTime)
	out.Nlink = stat.Nlink
	if out.Nlink == 0 {
		out.Nlink = 1
	}
	out.Uid = stat.Uid
	out.Gid = stat.Gid
	t := time.Duration(1) * time.Second
	out.SetTimeout(t)
	return 0
}

// Setattr implements NodeSetattrer (required for write support, e.g. truncate).
func (n *WardenFS) Setattr(ctx context.Context, f fs.FileHandle, in *fuse.SetAttrIn, out *fuse.AttrOut) syscall.Errno {
	if sz, ok := in.GetSize(); ok {
		if err := n.client.Truncate(n.path, int64(sz)); err != nil {
			return toErrno(err)
		}
	}
	if mode, ok := in.GetMode(); ok {
		if err := n.client.Chmod(n.path, mode); err != nil {
			return toErrno(err)
		}
	}
	return n.Getattr(ctx, f, out)
}

// Readdir implements NodeReaddirer.
func (n *WardenFS) Readdir(ctx context.Context) (fs.DirStream, syscall.Errno) {
	entries, err := n.client.ReadDir(n.path)
	if err != nil {
		return nil, toErrno(err)
	}
	dirEntries := make([]fuse.DirEntry, 0, len(entries))
	for _, e := range entries {
		mode := e.Mode
		if e.IsDir {
			mode = syscall.S_IFDIR | 0o755
		} else if mode&syscall.S_IFMT == 0 {
			mode = syscall.S_IFREG | mode
		}
		dirEntries = append(dirEntries, fuse.DirEntry{
			Name: e.Name,
			Mode: mode,
		})
	}
	return fs.NewListDirStream(dirEntries), 0
}

// Lookup implements NodeLookuper.
func (n *WardenFS) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	childP := n.childPath(name)
	stat, err := n.client.Stat(childP)
	if err != nil {
		return nil, toErrno(err)
	}

	mode := stat.Mode
	if mode&syscall.S_IFMT == 0 {
		if stat.IsDir {
			mode = syscall.S_IFDIR | mode
		} else {
			mode = syscall.S_IFREG | mode
		}
	}

	out.Mode = mode
	out.Size = uint64(stat.Size)
	out.Mtime = uint64(stat.ModTime)
	out.Nlink = stat.Nlink
	if out.Nlink == 0 {
		out.Nlink = 1
	}
	out.Uid = stat.Uid
	out.Gid = stat.Gid
	t := time.Duration(1) * time.Second
	out.SetEntryTimeout(t)
	out.SetAttrTimeout(t)

	child := n.NewInode(ctx, &WardenFS{
		client: n.client,
		path:   childP,
	}, fs.StableAttr{Mode: mode})

	return child, 0
}

// Open implements NodeOpener.
func (n *WardenFS) Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	handle, err := n.client.Open(n.path, int(flags))
	if err != nil {
		return nil, 0, toErrno(err)
	}
	return &wardenFileHandle{client: n.client, handle: handle}, 0, 0
}

// Create implements NodeCreater.
func (n *WardenFS) Create(ctx context.Context, name string, flags uint32, mode uint32, out *fuse.EntryOut) (*fs.Inode, fs.FileHandle, uint32, syscall.Errno) {
	childP := n.childPath(name)
	handle, err := n.client.Create(childP, mode)
	if err != nil {
		return nil, nil, 0, toErrno(err)
	}

	out.Mode = mode | syscall.S_IFREG
	out.Nlink = 1
	t := time.Duration(1) * time.Second
	out.SetEntryTimeout(t)
	out.SetAttrTimeout(t)

	child := n.NewInode(ctx, &WardenFS{
		client: n.client,
		path:   childP,
	}, fs.StableAttr{Mode: syscall.S_IFREG | mode})

	return child, &wardenFileHandle{client: n.client, handle: handle}, 0, 0
}

// Mkdir implements NodeMkdirer.
func (n *WardenFS) Mkdir(ctx context.Context, name string, mode uint32, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	childP := n.childPath(name)
	if err := n.client.Mkdir(childP, mode); err != nil {
		return nil, toErrno(err)
	}

	out.Mode = syscall.S_IFDIR | mode
	out.Nlink = 2
	t := time.Duration(1) * time.Second
	out.SetEntryTimeout(t)
	out.SetAttrTimeout(t)

	child := n.NewInode(ctx, &WardenFS{
		client: n.client,
		path:   childP,
	}, fs.StableAttr{Mode: syscall.S_IFDIR | mode})

	return child, 0
}

// Unlink implements NodeUnlinker.
func (n *WardenFS) Unlink(ctx context.Context, name string) syscall.Errno {
	if err := n.client.Remove(n.childPath(name)); err != nil {
		return toErrno(err)
	}
	return 0
}

// Rmdir implements NodeRmdirer.
func (n *WardenFS) Rmdir(ctx context.Context, name string) syscall.Errno {
	if err := n.client.Remove(n.childPath(name)); err != nil {
		return toErrno(err)
	}
	return 0
}

// Rename implements NodeRenamer.
func (n *WardenFS) Rename(ctx context.Context, name string, newParent fs.InodeEmbedder, newName string, flags uint32) syscall.Errno {
	newParentNode, ok := newParent.(*WardenFS)
	if !ok {
		return syscall.EINVAL
	}
	oldPath := n.childPath(name)
	newPath := newParentNode.childPath(newName)
	if err := n.client.Rename(oldPath, newPath); err != nil {
		return toErrno(err)
	}
	return 0
}

// Symlink implements NodeSymlinker.
func (n *WardenFS) Symlink(ctx context.Context, target, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	linkPath := n.childPath(name)
	if err := n.client.Symlink(target, linkPath); err != nil {
		return nil, toErrno(err)
	}

	out.Mode = syscall.S_IFLNK | 0o777
	out.Nlink = 1
	t := time.Duration(1) * time.Second
	out.SetEntryTimeout(t)
	out.SetAttrTimeout(t)

	child := n.NewInode(ctx, &WardenFS{
		client: n.client,
		path:   linkPath,
	}, fs.StableAttr{Mode: syscall.S_IFLNK})

	return child, 0
}

// Readlink implements NodeReadlinker.
func (n *WardenFS) Readlink(ctx context.Context) ([]byte, syscall.Errno) {
	target, err := n.client.Readlink(n.path)
	if err != nil {
		return nil, toErrno(err)
	}
	return []byte(target), 0
}

// wardenFileHandle implements FileReader, FileWriter, FileFlusher, FileReleaser.
type wardenFileHandle struct {
	client *FileClient
	handle uint64
}

var _ = (fs.FileReader)((*wardenFileHandle)(nil))
var _ = (fs.FileWriter)((*wardenFileHandle)(nil))
var _ = (fs.FileFlusher)((*wardenFileHandle)(nil))
var _ = (fs.FileReleaser)((*wardenFileHandle)(nil))

// Read implements FileReader.
func (f *wardenFileHandle) Read(ctx context.Context, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	data, err := f.client.Read(f.handle, off, len(dest))
	if err != nil {
		return nil, toErrno(err)
	}
	return fuse.ReadResultData(data), 0
}

// Write implements FileWriter.
func (f *wardenFileHandle) Write(ctx context.Context, data []byte, off int64) (uint32, syscall.Errno) {
	n, err := f.client.Write(f.handle, data, off)
	if err != nil {
		return 0, toErrno(err)
	}
	return uint32(n), 0
}

// Flush implements FileFlusher.
func (f *wardenFileHandle) Flush(ctx context.Context) syscall.Errno {
	if err := f.client.Flush(f.handle); err != nil {
		return toErrno(err)
	}
	return 0
}

// Release implements FileReleaser.
func (f *wardenFileHandle) Release(ctx context.Context) syscall.Errno {
	if err := f.client.Close(f.handle); err != nil {
		return toErrno(err)
	}
	return 0
}

// MountFUSE mounts the warden filesystem at mountPoint using the given FileClient.
// It returns an unmount function and any error.
func MountFUSE(mountPoint string, client *FileClient) (func(), error) {
	root := &WardenFS{client: client, path: "."}
	sec := time.Second
	server, err := fs.Mount(mountPoint, root, &fs.Options{
		EntryTimeout: &sec,
		AttrTimeout:  &sec,
		MountOptions: fuse.MountOptions{
			AllowOther: true,
			Name:       "wardenfs",
		},
	})
	if err != nil {
		return nil, err
	}
	return func() { server.Unmount() }, nil
}
