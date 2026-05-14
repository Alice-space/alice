package shared

import (
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestWaitOrKill_NormalExit(t *testing.T) {
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := WaitOrKill(cmd); err != nil {
		t.Fatalf("expected nil for exit 0, got %v", err)
	}
}

func TestWaitOrKill_NonZeroExit(t *testing.T) {
	cmd := exec.Command("sh", "-c", "exit 42")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	err := WaitOrKill(cmd)
	if err == nil {
		t.Fatal("expected non-nil error for exit 42")
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected *exec.ExitError, got %T: %v", err, err)
	}
	if status := exitErr.Sys().(syscall.WaitStatus); status.ExitStatus() != 42 {
		t.Fatalf("expected exit code 42, got %d", status.ExitStatus())
	}
}

func TestWaitOrKill_AlreadyExited(t *testing.T) {
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	// WaitOrKill on already-waited process should return an error
	// because cmd.Wait() can only be called once.
	err := WaitOrKill(cmd)
	if err == nil {
		t.Fatal("expected error for already-waited process")
	}
}

func TestWaitOrKill_SIGTERM_Escalation(t *testing.T) {
	// Restore original durations after test.
	oldGrace := SubprocessGracePeriod
	oldTerm := SubprocessTermGracePeriod
	SubprocessGracePeriod = 100 * time.Millisecond
	SubprocessTermGracePeriod = 100 * time.Millisecond
	defer func() {
		SubprocessGracePeriod = oldGrace
		SubprocessTermGracePeriod = oldTerm
	}()

	// Start a process that ignores SIGTERM.
	// We use a shell script that traps SIGTERM and sleeps.
	cmd := exec.Command("sh", "-c", "trap '' TERM; sleep 60")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	start := time.Now()
	err := WaitOrKill(cmd)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected non-nil error from killed process")
	}

	// Should have been killed after both grace periods expire
	// (wait 100ms + SIGTERM 100ms ≈ 200ms, then SIGKILL).
	// Allow some slack for scheduling.
	if elapsed < 150*time.Millisecond {
		t.Fatalf("expected at least ~200ms before kill, took %v", elapsed)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("kill took too long: %v", elapsed)
	}

	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected *exec.ExitError, got %T: %v", err, err)
	}
	status := exitErr.Sys().(syscall.WaitStatus)
	if !status.Signaled() {
		t.Fatalf("expected process killed by signal, got status %v", status)
	}
}

func TestWaitOrKill_SIGTERM_Accepted(t *testing.T) {
	oldGrace := SubprocessGracePeriod
	oldTerm := SubprocessTermGracePeriod
	SubprocessGracePeriod = 100 * time.Millisecond
	SubprocessTermGracePeriod = 100 * time.Millisecond
	defer func() {
		SubprocessGracePeriod = oldGrace
		SubprocessTermGracePeriod = oldTerm
	}()

	// Start a process that accepts SIGTERM (the default).
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	err := WaitOrKill(cmd)
	if err == nil {
		t.Fatal("expected non-nil error from signaled process")
	}

	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected *exec.ExitError, got %T: %v", err, err)
	}
	status := exitErr.Sys().(syscall.WaitStatus)
	if !status.Signaled() {
		t.Fatalf("expected process killed by signal, got status %v", status)
	}
	// SIGTERM (15) is the expected signal.
	if status.Signal() != syscall.SIGTERM {
		t.Fatalf("expected SIGTERM (15), got signal %d", status.Signal())
	}
}
