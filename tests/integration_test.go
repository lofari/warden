//go:build integration

package tests

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func wardenBin(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "warden")
	cmd := exec.Command("go", "build", "-o", bin, "../cmd/warden")
	cmd.Dir = ".."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build failed: %s\n%s", err, out)
	}
	return bin
}

func TestRunEchoCommand(t *testing.T) {
	bin := wardenBin(t)
	cmd := exec.Command(bin, "run", "--no-network", "--", "echo", "hello from warden")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("warden run failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "hello from warden") {
		t.Errorf("output = %q, want 'hello from warden'", string(out))
	}
}

func TestRunMountReadOnly(t *testing.T) {
	bin := wardenBin(t)
	tmp := t.TempDir()
	testFile := filepath.Join(tmp, "test.txt")
	os.WriteFile(testFile, []byte("readable"), 0o644)

	// Read should work
	cmd := exec.Command(bin, "run", "--mount", tmp+":ro", "--no-network", "--", "cat", testFile)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("read failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "readable") {
		t.Errorf("should be able to read file, got: %s", out)
	}

	// Write should fail
	cmd = exec.Command(bin, "run", "--mount", tmp+":ro", "--no-network", "--", "sh", "-c", "echo nope > "+testFile)
	err = cmd.Run()
	if err == nil {
		t.Error("writing to read-only mount should fail")
	}
}

func TestRunNoNetworkBlocks(t *testing.T) {
	bin := wardenBin(t)
	cmd := exec.Command(bin, "run", "--no-network", "--timeout", "10s", "--", "sh", "-c", "curl -s --max-time 5 https://example.com || exit 1")
	err := cmd.Run()
	if err == nil {
		t.Error("network request should fail with --no-network")
	}
}

func TestRunExitCodePropagation(t *testing.T) {
	bin := wardenBin(t)
	cmd := exec.Command(bin, "run", "--no-network", "--", "sh", "-c", "exit 42")
	err := cmd.Run()
	if exitErr, ok := err.(*exec.ExitError); ok {
		if exitErr.ExitCode() != 42 {
			t.Errorf("exit code = %d, want 42", exitErr.ExitCode())
		}
	} else if err == nil {
		t.Error("expected non-zero exit")
	}
}

func TestRunDryRun(t *testing.T) {
	bin := wardenBin(t)
	cmd := exec.Command(bin, "run", "--mount", "/tmp:ro", "--no-network", "--dry-run", "--", "echo", "hello")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("dry-run failed: %v\n%s", err, out)
	}
	output := string(out)
	if !strings.Contains(output, "docker") {
		t.Errorf("dry-run should print docker command, got: %s", output)
	}
	if !strings.Contains(output, "--network none") {
		t.Errorf("dry-run should show --network none, got: %s", output)
	}
}

func TestRunTimeout(t *testing.T) {
	bin := wardenBin(t)
	cmd := exec.Command(bin, "run", "--no-network", "--timeout", "5s", "--", "sleep", "60")
	err := cmd.Run()
	if exitErr, ok := err.(*exec.ExitError); ok {
		if exitErr.ExitCode() != 124 {
			t.Errorf("exit code = %d, want 124 (timeout)", exitErr.ExitCode())
		}
	} else if err == nil {
		t.Error("expected timeout exit")
	}
}
