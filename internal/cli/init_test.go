package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitCreatesWardenYAML(t *testing.T) {
	tmp := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmp)
	defer os.Chdir(origDir)

	err := runInit()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(tmp, ".warden.yaml"))
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if !strings.Contains(string(data), "default:") {
		t.Error("generated file should contain 'default:' section")
	}
}

func TestInitRefusesToOverwrite(t *testing.T) {
	tmp := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmp)
	defer os.Chdir(origDir)

	os.WriteFile(filepath.Join(tmp, ".warden.yaml"), []byte("existing"), 0o644)
	err := runInit()
	if err == nil {
		t.Fatal("should refuse to overwrite existing file")
	}
}
