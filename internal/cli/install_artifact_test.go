package cli

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"strings"
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

// install-artifact only checked that the expected files existed, so it reported
// ok for a runtime that doctor and update-runtime both rejected. An artifact
// built for another Node ABI, or a truncated download, passes a file check and
// then fails at the first render.
func TestInstallArtifactVerifiesTheInstalledRuntime(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "src")
	for _, sub := range []string{"scripts", filepath.Join("node_modules", ".bin"), "bin"} {
		if err := os.MkdirAll(filepath.Join(source, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// A runtime with the right shape whose renderer cannot actually run.
	write := func(path, body string, mode os.FileMode) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(source, path), []byte(body), mode); err != nil {
			t.Fatal(err)
		}
	}
	write("package.json", `{"name":"headlessmolstar"}`, 0o644)
	write(filepath.Join("scripts", "render-mvs.js"), "process.exit(1)\n", 0o644)
	write(filepath.Join("scripts", "molstar-node-cli.js"), "process.exit(1)\n", 0o644)
	write(filepath.Join("node_modules", ".bin", "mvs-render"), "#!/bin/sh\nexit 1\n", 0o755)
	write(filepath.Join("node_modules", ".bin", "mvs-validate"), "#!/bin/sh\nexit 1\n", 0o755)
	write(filepath.Join("bin", "molstar"), "#!/bin/sh\nexit 0\n", 0o755)

	args := func(extra ...string) []string {
		base := []string{
			"install-artifact",
			"--artifact", source,
			"--bin-dir", filepath.Join(dir, "bin"),
			"--config", filepath.Join(dir, "config.json"),
			"--install-deps=false",
			"--verify-timeout", "30s",
			"--force",
			"--json",
		}
		return append(base, extra...)
	}

	stdout, _, err := runAppForTest(context.Background(), args()...)
	if err == nil {
		t.Fatalf("install should fail when the runtime cannot run\n%s", stdout)
	}
	if got := classifyError(err); got != kindDoctor {
		t.Fatalf("classifyError = %q, want %q", got, kindDoctor)
	}
	// A failed probe is a renderer problem the caller can act on, not an
	// unclassifiable internal error.
	if got := agentErrorCode(err); got != kindRenderer {
		t.Fatalf("agentErrorCode = %q, want renderer_unavailable", got)
	}
	if !strings.Contains(err.Error(), "capability probe") {
		t.Fatalf("error should name the probe: %v", err)
	}

	// --verify=false installs without checking, for callers who want the old
	// behaviour.
	if _, _, err := runAppForTest(context.Background(), args("--verify=false")...); err != nil {
		t.Fatalf("--verify=false should install without probing: %v", err)
	}
}

// A Node stack trace buries the useful line; the envelope carries only a
// message, so the probe failure has to name the cause itself.
func TestFirstMeaningfulLinePicksTheErrorLine(t *testing.T) {
	stack := "node:internal/modules/cjs/loader:1572\n  throw err;\n  ^\n\n" +
		"Error: Cannot find module 'molstar/lib/commonjs/mol-canvas3d/canvas3d.js'\n" +
		"    at Module._resolveFilename (node:internal/modules/cjs/loader:1568:15)\n"
	if got := firstMeaningfulLine(stack); got != "Error: Cannot find module 'molstar/lib/commonjs/mol-canvas3d/canvas3d.js'" {
		t.Fatalf("firstMeaningfulLine = %q", got)
	}
	if got := firstMeaningfulLine("\n\n  just a warning\n"); got != "just a warning" {
		t.Fatalf("fallback line = %q", got)
	}
	if got := firstMeaningfulLine("   \n"); got != "" {
		t.Fatalf("empty input should yield empty, got %q", got)
	}
}
