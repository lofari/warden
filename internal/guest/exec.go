package guest

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/winler/warden/internal/protocol"
)

// Execute runs a command from an ExecMessage and returns exit code.
func Execute(msg *protocol.ExecMessage) (int, error) {
	cmd := exec.Command(msg.Command, msg.Args...)
	cmd.Dir = msg.Workdir
	cmd.Env = msg.Env

	// Set UID/GID
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid: uint32(msg.UID),
			Gid: uint32(msg.GID),
		},
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), nil
		}
		return 1, fmt.Errorf("executing command: %w", err)
	}
	return 0, nil
}
