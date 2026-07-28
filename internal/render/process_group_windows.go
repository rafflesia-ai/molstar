//go:build windows

package render

import (
	"context"
	"os/exec"
	"time"
)

func runCommandWithContext(ctx context.Context, cmd *exec.Cmd) error {
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
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		// Bounded, for the same reason as the POSIX path: cmd.Wait also waits on
		// the stdout/stderr copies, and a surviving grandchild holding those pipes
		// would hang the caller.
		select {
		case <-done:
		case <-time.After(waitAfterKill):
		}
		return ctx.Err()
	}
}

// waitAfterKill bounds how long a canceled command may take to reap after its
// process is killed.
const waitAfterKill = 5 * time.Second
