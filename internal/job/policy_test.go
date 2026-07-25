package job

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// These tests lock in the path-confinement security behavior: a job must not be
// able to read inputs or write outputs outside the operator's allowed roots.

func TestEnforceOutputPathPolicy(t *testing.T) {
	root := t.TempDir()
	runtime := Runtime{AllowPaths: []string{root}}

	allowed := []string{
		filepath.Join(root, "out.png"),
		filepath.Join(root, "nested", "deep", "out.png"),
		root, // the root itself
	}
	for _, path := range allowed {
		if err := EnforceOutputPathPolicy(Output{Path: path}, runtime); err != nil {
			t.Errorf("EnforceOutputPathPolicy(%q) = %v, want allowed", path, err)
		}
	}

	denied := []string{
		filepath.Join(root, "..", "escape.png"),           // parent traversal
		filepath.Join(root, "sub", "..", "..", "esc.png"), // traversal back out
		root + "X",                         // sibling-prefix (must not match root)
		filepath.Join(root+"X", "out.png"), // sibling dir with shared prefix
		filepath.Dir(root),                 // parent directory
	}
	for _, path := range denied {
		if err := EnforceOutputPathPolicy(Output{Path: path}, runtime); err == nil {
			t.Errorf("EnforceOutputPathPolicy(%q) = nil, want denied", path)
		}
	}
}

func TestEnforceLocalPathPolicyCoversFileURLs(t *testing.T) {
	root := t.TempDir()
	runtime := Runtime{AllowPaths: []string{root}}

	// A file:// URL pointing outside the allowed root must be blocked — this is
	// the LFI vector where a URL input bypasses the path sandbox.
	outside := Input{URL: "file:///etc/passwd"}
	if err := EnforceLocalPathPolicy(outside, runtime); err == nil {
		t.Fatal("file:// URL outside allowed roots should be blocked")
	}

	// A file:// URL inside the allowed root is fine.
	inside := Input{URL: "file://" + filepath.Join(root, "model.cif")}
	if err := EnforceLocalPathPolicy(inside, runtime); err != nil {
		t.Fatalf("file:// URL inside allowed root should be allowed: %v", err)
	}

	// A plain path input outside the root is blocked.
	if err := EnforceLocalPathPolicy(Input{Path: "/etc/shadow"}, runtime); err == nil {
		t.Fatal("path input outside allowed roots should be blocked")
	}
}

func TestEnforcePathPolicyBlocksSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	root := t.TempDir()
	outside := t.TempDir() // a sibling the sandbox must never reach

	// A symlink INSIDE the allowed root that points OUTSIDE it.
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	rt := Runtime{AllowPaths: []string{root}}

	// Reading through the symlink to a file outside the root must be blocked...
	throughLink := filepath.Join(link, "secret.cif")
	if err := EnforceLocalPathPolicy(Input{Path: throughLink}, rt); err == nil {
		t.Fatal("input path traversing a symlink out of the root should be blocked")
	}
	// ...and so must writing an output through it.
	if err := EnforceOutputPathPolicy(Output{Path: filepath.Join(link, "out.png")}, rt); err == nil {
		t.Fatal("output path traversing a symlink out of the root should be blocked")
	}

	// A genuine path inside the root (no escaping symlink) is still allowed,
	// including one that does not exist yet.
	if err := EnforceOutputPathPolicy(Output{Path: filepath.Join(root, "sub", "out.png")}, rt); err != nil {
		t.Fatalf("legitimate in-root output path should be allowed: %v", err)
	}
}

func TestEnforcePathPolicyNoAllowListIsUnrestricted(t *testing.T) {
	// With no allow-list configured, confinement is disabled (documented behavior).
	if err := EnforceOutputPathPolicy(Output{Path: "/anywhere/out.png"}, Runtime{}); err != nil {
		t.Fatalf("empty allow-list should not restrict: %v", err)
	}
}

// TestValidateRuntimeLimitsRejectsOverflowingSize locks in that an output size
// crafted to overflow width*height cannot slip past the max_pixels ceiling.
func TestValidateRuntimeLimitsRejectsOverflowingSize(t *testing.T) {
	j := Job{
		Version: 1,
		Runtime: Runtime{MaxPixels: 1_000_000},
		Outputs: []Output{{Type: "png", Path: "out.png", Size: []int{4611686018427387904, 8}}},
	}
	if err := j.ValidateRuntimeLimits(); err == nil {
		t.Fatal("expected overflowing output size to be rejected by max_pixels")
	}

	// A normal in-bounds size still validates.
	ok := Job{
		Version: 1,
		Runtime: Runtime{MaxPixels: 1_000_000},
		Outputs: []Output{{Type: "png", Path: "out.png", Size: []int{800, 600}}},
	}
	if err := ok.ValidateRuntimeLimits(); err != nil {
		t.Fatalf("in-bounds size should validate, got: %v", err)
	}
}

// TestValidateRenderRejectsNonPositiveSize locks in that a non-positive output
// size is rejected at validation (clean 400) rather than reaching the renderer
// and producing a 5xx — regardless of whether max_pixels is configured.
func TestValidateRenderRejectsNonPositiveSize(t *testing.T) {
	base := func(size []int) Job {
		return Job{
			Version: 1,
			Inputs:  map[string]Input{"x": {Path: "/tmp/x.cif", Format: "mmcif"}},
			Scene:   Scene{Structures: []Structure{{Source: "x", Components: []Component{{Select: "all"}}}}},
			Outputs: []Output{{Type: "image", Path: "/tmp/o.png", Size: size}},
		}
	}
	for _, sz := range [][]int{{-4, -4}, {0, 0}, {0, 100}, {100, 0}} {
		if err := base(sz).ValidateRender(); err == nil {
			t.Fatalf("size %v should be rejected", sz)
		}
	}
	if err := base([]int{800, 600}).ValidateRender(); err != nil {
		t.Fatalf("valid size rejected: %v", err)
	}
	if err := base(nil).ValidateRender(); err != nil {
		t.Fatalf("omitted size should default, got: %v", err)
	}
}
