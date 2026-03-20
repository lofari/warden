package firecracker

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractFirecrackerBinaryRejectsTraversal(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{
		Name: "../../../etc/evil",
		Size: 4,
		Mode: 0o755,
	})
	tw.Write([]byte("evil"))
	tw.Close()
	gz.Close()

	tmpTar, _ := os.CreateTemp("", "test-*.tgz")
	tmpTar.Write(buf.Bytes())
	tmpTar.Close()
	defer os.Remove(tmpTar.Name())

	dest := filepath.Join(t.TempDir(), "firecracker")
	err := extractFirecrackerBinary(tmpTar.Name(), dest)
	if err == nil {
		t.Fatal("expected error for tarball without valid binary")
	}
}
