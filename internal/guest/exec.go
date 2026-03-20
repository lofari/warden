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

	// Set UID/GID if specified
	if msg.UID != nil || msg.GID != nil {
		cred := &syscall.Credential{}
		if msg.UID != nil {
			cred.Uid = uint32(*msg.UID)
		}
		if msg.GID != nil {
			cred.Gid = uint32(*msg.GID)
		}
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: cred}
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
