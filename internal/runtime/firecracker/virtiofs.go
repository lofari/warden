package firecracker

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type virtiofsInstance struct {
	cmd    *exec.Cmd
	socket string
	tag    string
}

// startVirtiofs starts a virtiofsd instance for the given host path.
func startVirtiofs(homeDir, hostPath, tag string) (*virtiofsInstance, error) {
	vfsPath := filepath.Join(homeDir, ".warden", "firecracker", "bin", "virtiofsd")

	socketDir, err := os.MkdirTemp("", "warden-vfs-*")
	if err != nil {
		return nil, err
	}
	socketPath := filepath.Join(socketDir, "vfs.sock")

	cmd := exec.Command(vfsPath,
		"--socket-path", socketPath,
		"--shared-dir", hostPath,
		"--tag", tag,
	)
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		os.RemoveAll(socketDir)
		return nil, fmt.Errorf("starting virtiofsd for %s: %w", hostPath, err)
	}

	return &virtiofsInstance{
		cmd:    cmd,
		socket: socketPath,
		tag:    tag,
	}, nil
}

func (v *virtiofsInstance) stop() {
	if v.cmd.Process != nil {
		v.cmd.Process.Kill()
		v.cmd.Wait()
	}
	// Clean up socket directory
	os.RemoveAll(filepath.Dir(v.socket))
}
