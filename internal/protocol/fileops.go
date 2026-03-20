package protocol

type FileRequest struct {
	ID      uint64 `json:"id"`
	Op      string `json:"op"`
	Path    string `json:"path,omitempty"`
	NewPath string `json:"new_path,omitempty"`
	Handle  uint64 `json:"handle,omitempty"`
	Offset  int64  `json:"offset,omitempty"`
	Size    int    `json:"size,omitempty"`
	Data    string `json:"data,omitempty"`
	Mode    uint32 `json:"mode,omitempty"`
	Flags   int    `json:"flags,omitempty"`
}

type FileStat struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	Mode    uint32 `json:"mode"`
	ModTime int64  `json:"mod_time"`
	IsDir   bool   `json:"is_dir"`
	Nlink   uint32 `json:"nlink"`
	Uid     uint32 `json:"uid"`
	Gid     uint32 `json:"gid"`
}

type DirEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir"`
	Mode  uint32 `json:"mode"`
}

type FileResponse struct {
	ID      uint64     `json:"id"`
	Error   string     `json:"error,omitempty"`
	Data    string     `json:"data,omitempty"`
	Stat    *FileStat  `json:"stat,omitempty"`
	Entries []DirEntry `json:"entries,omitempty"`
	Handle  uint64     `json:"handle,omitempty"`
	Written int        `json:"written,omitempty"`
}

const (
	OpStat     = "stat"
	OpReadDir  = "readdir"
	OpOpen     = "open"
	OpRead     = "read"
	OpWrite    = "write"
	OpClose    = "close"
	OpCreate   = "create"
	OpMkdir    = "mkdir"
	OpRemove   = "remove"
	OpRename   = "rename"
	OpTrunc    = "truncate"
	OpSymlink  = "symlink"
	OpReadlink = "readlink"
	OpChmod    = "chmod"
	OpFlush    = "flush"
)
