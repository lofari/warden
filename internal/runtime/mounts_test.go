package runtime

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/winler/warden/internal/config"
)

func TestResolveMounts(t *testing.T) {
	tmp := t.TempDir()
	mounts := []config.Mount{{Path: tmp, Mode: "rw"}}
	resolved, err := ResolveMounts(mounts, tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("got %d mounts, want 1", len(resolved))
	}
	if !filepath.IsAbs(resolved[0].Path) {
		t.Errorf("resolved path %q is not absolute", resolved[0].Path)
	}
}

func TestResolveMountsRelativePath(t *testing.T) {
	tmp := t.TempDir()
	sub := filepath.Join(tmp, "project")
	os.MkdirAll(sub, 0o755)
	mounts := []config.Mount{{Path: "project", Mode: "ro"}}
	resolved, err := ResolveMounts(mounts, tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved[0].Path != sub {
		t.Errorf("path = %q, want %q", resolved[0].Path, sub)
	}
}

func TestResolveMountsMissingPath(t *testing.T) {
	_, err := ResolveMounts([]config.Mount{{Path: "/nonexistent/path", Mode: "ro"}}, "/")
	if err == nil {
		t.Fatal("expected error for nonexistent mount path")
	}
}

func TestResolveMountsPreservesAccessControlFields(t *testing.T) {
	dir := t.TempDir()
	mounts := []config.Mount{{
		Path:         dir,
		Mode:         "rw",
		DenyExtra:    []string{"**/*.secret"},
		DenyOverride: []string{"**/.env"},
		ReadOnly:     []string{"vendor/**"},
	}}
	resolved, err := ResolveMounts(mounts, "/")
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 1 {
		t.Fatalf("expected 1 mount, got %d", len(resolved))
	}
	m := resolved[0]
	if len(m.DenyExtra) != 1 || m.DenyExtra[0] != "**/*.secret" {
		t.Errorf("DenyExtra not preserved: %v", m.DenyExtra)
	}
	if len(m.DenyOverride) != 1 || m.DenyOverride[0] != "**/.env" {
		t.Errorf("DenyOverride not preserved: %v", m.DenyOverride)
	}
	if len(m.ReadOnly) != 1 || m.ReadOnly[0] != "vendor/**" {
		t.Errorf("ReadOnly not preserved: %v", m.ReadOnly)
	}
}
