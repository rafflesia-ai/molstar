//go:build windows

package render

import (
	"context"
	"os/exec"
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
		<-done
		return ctx.Err()
	}
}
