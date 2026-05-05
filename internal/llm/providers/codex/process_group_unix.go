//go:build unix

package codex

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func configureInterruptibleCommand(cmd *exec.Cmd, _ string) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil || cmd.Process.Pid <= 0 {
			return os.ErrProcessDone
		}
		pid := cmd.Process.Pid
		err := syscall.Kill(-pid, syscall.SIGKILL)
		switch {
		case err == nil:
			return nil
		case errors.Is(err, syscall.ESRCH):
			return nil
		}
		if killErr := cmd.Process.Kill(); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
			return killErr
		}
		return nil
	}
}
