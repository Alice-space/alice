// Package shared provides utilities and constants used across LLM providers.
package shared

import (
	"os/exec"
	"syscall"
	"time"
)

// Default scanner buffer and token size constants used across providers.
const (
	DefaultScannerBuf       = 64 * 1024
	MaxScannerTokenSize2MB  = 2 * 1024 * 1024
	MaxScannerTokenSize10MB = 10 * 1024 * 1024
)

// AuthCheckTimeout is the default timeout for CLI login/auth status checks.
const AuthCheckTimeout = 15 * time.Second

const (
	// SubprocessGracePeriod is how long to wait for a subprocess to exit
	// naturally after its stdin is closed before escalating to SIGTERM.
	SubprocessGracePeriod = 10 * time.Second
	// SubprocessTermGracePeriod is how long to wait after SIGTERM before
	// escalating to SIGKILL.
	SubprocessTermGracePeriod = 10 * time.Second
)

// WaitOrKill closes the subprocess gracefully: wait for natural exit after
// stdin close, then SIGTERM, then SIGKILL as last resort.
func WaitOrKill(cmd *exec.Cmd) error {
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(SubprocessGracePeriod):
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		_ = cmd.Process.Kill()
		return <-done
	}
	select {
	case err := <-done:
		return err
	case <-time.After(SubprocessTermGracePeriod):
	}
	_ = cmd.Process.Kill()
	return <-done
}
