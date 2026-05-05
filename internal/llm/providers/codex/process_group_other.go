//go:build !unix

package codex

import "os/exec"

func configureInterruptibleCommand(cmd *exec.Cmd, processName string) {
}
