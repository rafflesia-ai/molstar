//go:build !windows

package render

import (
	"context"
	"os/exec"
	"syscall"
)

// runCommandWithContext runs cmd in its own process group and, on cancellation
// or timeout, kills the whole group rather than just the direct child.
// exec.CommandContext only signals the process it started, so a renderer command
// that is a wrapper script left its real renderer running after a --timeout or a
// server-side cancel.
func runCommandWithContext(ctx context.Context, cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		killProcessGroup(cmd)
		<-done
		return ctx.Err()
	}
}

func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		_ = cmd.Process.Kill()
		return
	}
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
}
