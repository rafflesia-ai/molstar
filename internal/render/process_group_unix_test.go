//go:build !windows

package render

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A renderer command that spawns a child kept the inherited stdout/stderr pipes
// open after exec killed the direct child, so cmd.Wait blocked forever: a
// --timeout or a server-side cancel hung indefinitely instead of returning, and
// the grandchild leaked. The renderer now runs in its own process group and the
// whole group is killed on cancellation.
func TestRunKillsTheWholeProcessGroupOnCancel(t *testing.T) {
	dir := t.TempDir()
	markerPath := filepath.Join(dir, "grandchild.pid")
	stub := filepath.Join(dir, "wrapper.sh")
	// Mirrors a launcher script: the real work happens in a child process that
	// inherits this process's stdout and stderr.
	script := "#!/bin/sh\nsleep 120 &\necho $! > " + markerPath + "\nwait\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	runner := Molstar{RendererCommand: []string{stub}}
	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := runner.run(ctx, []string{stub})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected the canceled run to return an error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected a deadline error, got %v", err)
	}
	if elapsed > 20*time.Second {
		t.Fatalf("run took %v after a 750ms deadline; cancellation did not interrupt it", elapsed)
	}

	// The grandchild must be gone, not merely orphaned.
	data, readErr := os.ReadFile(markerPath)
	if readErr != nil {
		t.Skipf("stub did not record a grandchild pid: %v", readErr)
	}
	pid, convErr := strconv.Atoi(strings.TrimSpace(string(data)))
	if convErr != nil {
		t.Skipf("unreadable grandchild pid %q", data)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if syscall.Kill(pid, 0) != nil {
			return // signalling failed: the process is gone
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	t.Fatalf("grandchild pid %d survived cancellation", pid)
}
