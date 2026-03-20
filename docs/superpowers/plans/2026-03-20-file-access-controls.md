# File Access Controls Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add deny-list and read-only override support to the file server so sensitive files are blocked and critical paths are immutable, with secure-by-default built-in patterns.

**Architecture:** Extend the `Mount` config type with `DenyExtra`, `DenyOverride`, and `ReadOnly` fields. Add a `fileserver.AccessControl` struct that encapsulates pattern matching logic using the `doublestar` library. Wire it into `resolvePath` (deny) and a new `requireWritePath` helper (read-only). Filter denied entries from `readdir` results. Check resolved symlink paths against deny patterns to prevent bypass.

**Tech Stack:** Go 1.25, `github.com/bmatcuk/doublestar/v4`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/fileserver/access.go` | `AccessControl` struct: built-in defaults, pattern merging, `IsDenied(relPath)`, `IsReadOnly(relPath)` |
| `internal/fileserver/access_test.go` | Unit tests for pattern matching in isolation |
| `internal/fileserver/server.go` | Modified: accept `AccessControl`, wire into `resolvePath`, `readdir`, all write handlers |
| `internal/fileserver/server_test.go` | Modified: add deny-list and read-only override integration tests |
| `internal/config/types.go` | Modified: add `DenyExtra`, `DenyOverride`, `ReadOnly` fields to `Mount` |
| `internal/config/parse.go` | Modified: add fields to `ProfileEntry.Mount` equivalent |
| `internal/runtime/firecracker/firecracker.go` | Modified: pass access control config when constructing file server |

---

### Task 1: Add doublestar Dependency and AccessControl Struct

**Files:**
- Create: `internal/fileserver/access.go`
- Create: `internal/fileserver/access_test.go`
- Modify: `go.mod`

- [ ] **Step 1: Add doublestar dependency**

Run:
```bash
go get github.com/bmatcuk/doublestar/v4
```

- [ ] **Step 2: Write tests for AccessControl pattern matching**

```go
// internal/fileserver/access_test.go
package fileserver

import "testing"

func TestBuiltInDenyDefaults(t *testing.T) {
	ac := NewAccessControl(nil, nil, nil)
	tests := []struct {
		path string
		want bool
	}{
		{".env", true},
		{".env.local", true},
		{".env.production", true},
		{"src/.env", true},  // **/.env matches at any depth
		{"secrets.pem", true},
		{"cert.key", true},
		{"cert.p12", true},
		{".git/credentials", true},
		{".git/config", true},
		{".ssh/id_rsa", true},
		{"home/.ssh/id_rsa", true},
		{".aws/credentials", true},
		{".npmrc", true},
		{".pypirc", true},
		{".docker/config.json", true},
		{"src/main.go", false},
		{"README.md", false},
		{".git/HEAD", false},
	}
	for _, tt := range tests {
		if got := ac.IsDenied(tt.path); got != tt.want {
			t.Errorf("IsDenied(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestDenyExtra(t *testing.T) {
	ac := NewAccessControl([]string{"secrets/", "*.secret"}, nil, nil)
	if !ac.IsDenied(".env") {
		t.Error("built-in .env should still be denied")
	}
	if !ac.IsDenied("secrets/db.yml") {
		t.Error("secrets/ should be denied via deny_extra")
	}
	if !ac.IsDenied("app.secret") {
		t.Error("*.secret should be denied via deny_extra")
	}
	if ac.IsDenied("src/main.go") {
		t.Error("src/main.go should not be denied")
	}
}

func TestDenyOverride(t *testing.T) {
	ac := NewAccessControl(nil, []string{".env"}, nil)
	if !ac.IsDenied(".env") {
		t.Error(".env should be denied")
	}
	// Built-in defaults are replaced — .pem should NOT be denied
	if ac.IsDenied("cert.pem") {
		t.Error("cert.pem should not be denied when override replaces defaults")
	}
}

func TestReadOnlyPatterns(t *testing.T) {
	ac := NewAccessControl(nil, nil, []string{".git/hooks", ".github/workflows", "Makefile"})
	tests := []struct {
		path string
		want bool
	}{
		{".git/hooks/pre-commit", true},
		{".git/hooks", true},
		{".github/workflows/ci.yml", true},
		{"Makefile", true},
		{"src/main.go", false},
		{".git/HEAD", false},
	}
	for _, tt := range tests {
		if got := ac.IsReadOnly(tt.path); got != tt.want {
			t.Errorf("IsReadOnly(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/fileserver/ -run TestBuiltInDeny -v`
Expected: FAIL — `NewAccessControl` undefined

- [ ] **Step 4: Implement AccessControl**

```go
// internal/fileserver/access.go
package fileserver

import (
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// DefaultDenyPatterns is the built-in deny-list, active unless overridden.
var DefaultDenyPatterns = []string{
	"**/.env",
	"**/.env.*",
	"**/*.pem",
	"**/*.key",
	"**/*.p12",
	"**/*.pfx",
	"**/.npmrc",
	"**/.pypirc",
	".git/credentials",
	".git/config",
	"**/.ssh/*",
	"**/.aws/*",
	"**/.gnupg/*",
	"**/.docker/config.json",
}

// AccessControl manages deny-list and read-only override patterns.
type AccessControl struct {
	denyPatterns     []string
	readOnlyPatterns []string
}

// NewAccessControl creates an AccessControl.
//   - denyExtra: additional patterns added to built-in defaults
//   - denyOverride: if non-nil, replaces built-in defaults entirely
//   - readOnly: paths that are read-only within an rw mount
func NewAccessControl(denyExtra, denyOverride, readOnly []string) *AccessControl {
	var deny []string
	if denyOverride != nil {
		deny = denyOverride
	} else {
		deny = append(deny, DefaultDenyPatterns...)
		deny = append(deny, denyExtra...)
	}
	return &AccessControl{
		denyPatterns:     deny,
		readOnlyPatterns: readOnly,
	}
}

// IsDenied returns true if the relative path matches any deny pattern.
func (ac *AccessControl) IsDenied(relPath string) bool {
	if ac == nil {
		return false
	}
	relPath = filepath.ToSlash(relPath)
	for _, pattern := range ac.denyPatterns {
		pattern = filepath.ToSlash(pattern)
		if matched, _ := doublestar.Match(pattern, relPath); matched {
			return true
		}
	}
	return false
}

// IsReadOnly returns true if the relative path falls under a read-only override.
func (ac *AccessControl) IsReadOnly(relPath string) bool {
	if ac == nil {
		return false
	}
	relPath = filepath.ToSlash(relPath)
	for _, pattern := range ac.readOnlyPatterns {
		pattern = filepath.ToSlash(pattern)
		// Exact match
		if matched, _ := doublestar.Match(pattern, relPath); matched {
			return true
		}
		// Prefix match: if pattern is "X", then "X/anything" is also read-only
		if strings.HasPrefix(relPath, pattern+"/") {
			return true
		}
	}
	return false
}

// NoAccessControl returns a nil AccessControl (no restrictions).
func NoAccessControl() *AccessControl {
	return nil
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/fileserver/ -run "TestBuiltIn|TestDeny|TestReadOnly" -v`
Expected: all PASS

- [ ] **Step 6: Commit**

```bash
git add internal/fileserver/access.go internal/fileserver/access_test.go go.mod go.sum
git commit -m "feat: add AccessControl with deny-list defaults and read-only patterns"
```

---

### Task 2: Wire AccessControl into File Server

**Files:**
- Modify: `internal/fileserver/server.go`
- Modify: `internal/fileserver/server_test.go`

- [ ] **Step 1: Write integration tests for deny-list in file server**

```go
// internal/fileserver/server_test.go — add

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
```

Add `"strings"` to the test file imports.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/fileserver/ -run "TestServerDeny|TestServerReadOnly" -v`
Expected: FAIL — `NewServer` signature mismatch (3 args vs 2)

- [ ] **Step 3: Update Server struct and NewServer**

In `server.go`, add `ac` field to Server and update constructor:

```go
type Server struct {
	root       string
	readOnly   bool
	ac         *AccessControl
	handles    sync.Map
	nextID     atomic.Uint64
	maxHandles int
	openCount  atomic.Int32
}

func NewServer(root string, readOnly bool, ac *AccessControl) *Server {
	return &Server{root: root, readOnly: readOnly, ac: ac, maxHandles: 1024}
}
```

- [ ] **Step 4: Add deny check to resolvePath**

After the existing symlink resolution in `resolvePath`, add deny-list checks on both the requested relative path and the resolved real path:

```go
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
```

- [ ] **Step 5: Add readdir filtering**

In `handleReadDir`, filter denied entries:

```go
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
```

- [ ] **Step 6: Add requireWritePath helper and wire into write handlers**

Add a helper method:

```go
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
```

Replace the `if s.readOnly { ... }` check in each write handler with:

- `handleCreate`: after `resolvePath`, add `if r := s.requireWritePath(s.relPath(path)); r != nil { return r }`
- `handleWrite`: keep the existing `s.readOnly` check (no path context for handle-based ops), OR look up the file path from the handle. **Simpler approach**: write handlers that take a path (create, mkdir, remove, rename, truncate, symlink, chmod) use `requireWritePath`. Handle-based handlers (write, flush) keep the `s.readOnly` check only.
- `handleOpen`: after resolving path, check write flags against `requireWritePath`
- `handleMkdir`: after `resolvePath`, add `requireWritePath` check
- `handleRemove`: after `resolvePath`, add `requireWritePath` check
- `handleRename`: check BOTH source and destination: `requireWritePath(s.relPath(oldPath))` and `requireWritePath(s.relPath(newPath))`
- `handleTruncate`: after `resolvePath`, add `requireWritePath` check
- `handleSymlink`: after `resolvePath`, add `requireWritePath` check
- `handleChmod`: after `resolvePath`, add `requireWritePath` check
- `handleOpen` with write flags: after `resolvePath`, check `requireWritePath`

For `handleOpen`, replace the existing read-only check:

```go
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
	// ... rest unchanged
```

- [ ] **Step 7: Update existing NewServer calls in tests**

All existing test calls use `NewServer(dir, false)` or `NewServer(dir, true)`. These need a third argument. Pass `nil` for no access control:

Find and replace in `server_test.go`:
- `NewServer(dir, false)` → `NewServer(dir, false, nil)`
- `NewServer(dir, true)` → `NewServer(dir, true, nil)`

- [ ] **Step 8: Update NewServer call in firecracker runtime**

In `internal/runtime/firecracker/firecracker.go`, the call at line ~133:

```go
srv := fileserver.NewServer(mountPath, ro)
```

Becomes:

```go
srv := fileserver.NewServer(mountPath, ro, nil)
```

For now, pass `nil` (no access controls). Task 3 will wire the real config through.

- [ ] **Step 9: Update NewServer call in guest fuse test**

In `internal/guest/fuse_test.go`, there are **4 calls** to `fileserver.NewServer(dir, false)`. Change all 4 to `fileserver.NewServer(dir, false, nil)`.

- [ ] **Step 10: Run all tests**

Run: `go test ./... -v`
Expected: all PASS

- [ ] **Step 11: Commit**

```bash
git add internal/fileserver/server.go internal/fileserver/server_test.go internal/runtime/firecracker/firecracker.go internal/guest/fuse_test.go
git commit -m "feat: wire deny-list and read-only overrides into file server"
```

---

### Task 3: Update Config Types and Wire Through Runtime

**Files:**
- Modify: `internal/config/types.go`
- Modify: `internal/config/parse_test.go`
- Modify: `internal/runtime/firecracker/firecracker.go`
- Modify: `internal/cli/init.go`

- [ ] **Step 1: Write test for new Mount fields parsing**

```go
// internal/config/parse_test.go — add

func TestParseWardenYAMLWithAccessControls(t *testing.T) {
	yaml := `
default:
  mounts:
    - path: .
      mode: rw
      deny_extra:
        - secrets/
        - "*.secret"
      read_only:
        - .git/hooks
        - .github/workflows
`
	file, err := ParseWardenYAML([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Default.Mounts) != 1 {
		t.Fatalf("expected 1 mount, got %d", len(file.Default.Mounts))
	}
	m := file.Default.Mounts[0]
	if len(m.DenyExtra) != 2 {
		t.Fatalf("expected 2 deny_extra, got %d", len(m.DenyExtra))
	}
	if m.DenyExtra[0] != "secrets/" {
		t.Fatalf("expected deny_extra[0] = 'secrets/', got %q", m.DenyExtra[0])
	}
	if len(m.ReadOnly) != 2 {
		t.Fatalf("expected 2 read_only, got %d", len(m.ReadOnly))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestParseWardenYAMLWithAccessControls -v`
Expected: FAIL — `DenyExtra` field doesn't exist

- [ ] **Step 3: Add fields to Mount struct**

In `internal/config/types.go`:

```go
type Mount struct {
	Path          string   `yaml:"path"`
	Mode          string   `yaml:"mode"`           // "ro" or "rw"
	DenyExtra     []string `yaml:"deny_extra"`     // additional deny patterns (added to defaults)
	DenyOverride  []string `yaml:"deny_override"`  // replaces default deny patterns entirely
	ReadOnly      []string `yaml:"read_only"`      // paths that are read-only within this mount
}
```

- [ ] **Step 4: Run config tests**

Run: `go test ./internal/config/ -v`
Expected: all PASS

- [ ] **Step 5: Wire config into firecracker runtime**

In `internal/runtime/firecracker/firecracker.go`, where file servers are constructed (around line 133), build an `AccessControl` from the mount config:

```go
			go func(mountPath string, p uint32, m config.Mount) {
				fsConn, err := dialGuest(vm.vsockPath, p, 10*time.Second)
				if err != nil {
					fmt.Fprintf(os.Stderr, "warden: file server for %s failed: %v\n", mountPath, err)
					return
				}
				defer fsConn.Close()
				readOnly := m.Mode == "ro"
				ac := fileserver.NewAccessControl(m.DenyExtra, m.DenyOverride, m.ReadOnly)
				srv := fileserver.NewServer(mountPath, readOnly, ac)
				srv.Serve(fsConn)
			}(m.Path, port, m)
```

Note: the goroutine closure needs to capture the `config.Mount` value, not just `m.Path` and `readOnly`.

- [ ] **Step 6: Update init template**

In `internal/cli/init.go`, update the template to document new fields:

```go
const initTemplate = `# Warden sandbox configuration
# Docs: https://github.com/winler/warden

default:
  runtime: docker  # or: firecracker
  image: ubuntu:24.04
  tools: []
  mounts:
    - path: .
      mode: rw
      # deny_extra:        # additional files to block (added to built-in defaults)
      #   - secrets/
      #   - "*.secret"
      # deny_override:     # replace built-in deny defaults entirely
      #   - .env
      # read_only:         # paths that are read-only within this rw mount
      #   - .git/hooks
      #   - .github/workflows
  network: false
  memory: 8g
`
```

- [ ] **Step 7: Run all tests**

Run: `go test ./... -v`
Expected: all PASS

- [ ] **Step 8: Commit**

```bash
git add internal/config/types.go internal/config/parse_test.go internal/runtime/firecracker/firecracker.go internal/cli/init.go
git commit -m "feat: wire file access control config through Mount type to runtime"
```

---

## Summary

| Task | What it Builds | Tests |
|------|---------------|-------|
| Task 1 | `AccessControl` struct with deny-list defaults, pattern matching via doublestar | 4 unit tests for pattern matching |
| Task 2 | Wire into file server: resolvePath deny, readdir filter, requireWritePath, symlink bypass prevention | 5 integration tests + update 11 existing test calls |
| Task 3 | Config types (`DenyExtra`, `DenyOverride`, `ReadOnly` on Mount), runtime wiring, init template | 1 parsing test |

**Dependencies:** Task 2 depends on Task 1. Task 3 depends on Task 2.
