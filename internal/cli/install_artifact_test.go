package cli

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func writeTarGz(t *testing.T, path string, write func(*tar.Writer)) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	write(tw)
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestExtractTarGzRejectsEscapingSymlink proves a relative symlink whose target
// escapes the extraction root is rejected (matching the zip extractor), so a
// later entry cannot be written through it outside the destination.
func TestExtractTarGzRejectsEscapingSymlink(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "evil.tar.gz")
	body := []byte("pwned")
	writeTarGz(t, archive, func(tw *tar.Writer) {
		if err := tw.WriteHeader(&tar.Header{
			Name:     "x",
			Typeflag: tar.TypeSymlink,
			Linkname: "../../../../escape",
			Mode:     0o777,
		}); err != nil {
			t.Fatal(err)
		}
		if err := tw.WriteHeader(&tar.Header{
			Name:     "x/passwd",
			Typeflag: tar.TypeReg,
			Mode:     0o644,
			Size:     int64(len(body)),
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	})

	target := filepath.Join(dir, "out")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := extractTarGz(archive, target); err == nil {
		t.Fatal("expected escaping symlink to be rejected")
	}
	if _, err := os.Stat(filepath.Join(dir, "escape")); !os.IsNotExist(err) {
		t.Fatalf("escape path was created outside target: err=%v", err)
	}
}

// TestExtractTarGzAllowsInternalSymlink confirms the fix does not over-reject a
// benign symlink whose target stays within the extraction root.
func TestExtractTarGzAllowsInternalSymlink(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "ok.tar.gz")
	body := []byte("data")
	writeTarGz(t, archive, func(tw *tar.Writer) {
		if err := tw.WriteHeader(&tar.Header{
			Name:     "real.txt",
			Typeflag: tar.TypeReg,
			Mode:     0o644,
			Size:     int64(len(body)),
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
		if err := tw.WriteHeader(&tar.Header{
			Name:     "link.txt",
			Typeflag: tar.TypeSymlink,
			Linkname: "real.txt",
			Mode:     0o777,
		}); err != nil {
			t.Fatal(err)
		}
	})

	target := filepath.Join(dir, "out")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := extractTarGz(archive, target); err != nil {
		t.Fatalf("benign internal symlink should extract, got: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(target, "link.txt")); err != nil {
		t.Fatalf("expected internal symlink to be created: %v", err)
	}
}
