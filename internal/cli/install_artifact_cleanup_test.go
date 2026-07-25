package cli

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestInstallArtifactCleansUpFailedExtraction verifies that a corrupt/wrong
// artifact does not leave a partially-extracted runtime directory behind, which
// would otherwise block a retry with "already exists; pass --force".
func TestInstallArtifactCleansUpFailedExtraction(t *testing.T) {
	dir := t.TempDir()
	prefix := filepath.Join(dir, "runtime")

	// A zip with a file but no valid runtime root inside.
	archive := filepath.Join(dir, "partial.zip")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("junk/readme.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("not a runtime")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	_, _, err = runAppForTest(context.Background(),
		"install-artifact", "--artifact", archive,
		"--prefix", prefix,
		"--bin-dir", filepath.Join(dir, "bin"),
		"--config", filepath.Join(dir, "config.json"),
		"--install-deps=false", "--json")
	if err == nil {
		t.Fatal("expected install-artifact to fail on an artifact with no runtime root")
	}
	if _, statErr := os.Stat(prefix); !os.IsNotExist(statErr) {
		t.Fatalf("failed install left the runtime dir behind (stat err=%v); a retry would need --force", statErr)
	}
}
