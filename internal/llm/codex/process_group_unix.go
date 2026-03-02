//go:build unix

package codex

import (
	"errors"
	"os"
	"os/exec"
	"syscall"

	"gitee.com/alicespace/alice/internal/logging"
)

func configureInterruptibleCommand(cmd *exec.Cmd, processName string) {
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
			logging.Debugf("%s process group killed pid=%d", processName, pid)
			return nil
		case errors.Is(err, syscall.ESRCH):
			return nil
		}

		if killErr := cmd.Process.Kill(); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
			logging.Debugf("%s process kill fallback failed pid=%d err=%v", processName, pid, killErr)
			return killErr
		}
		logging.Debugf("%s process group kill fallback to direct process pid=%d err=%v", processName, pid, err)
		return nil
	}
}
