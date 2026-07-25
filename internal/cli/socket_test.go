package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func shortUnixSocketPath(t *testing.T, name string) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hms-sock-*")
	if err != nil {
		dir = t.TempDir()
	} else {
		t.Cleanup(func() {
			_ = os.RemoveAll(dir)
		})
	}
	return filepath.Join(dir, name)
}
