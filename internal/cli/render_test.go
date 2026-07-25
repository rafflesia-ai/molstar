package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sacha-ichbiah/molstar/internal/job"
)

func TestFirstOutputSize(t *testing.T) {
	if got := firstOutputSize(nil); got != nil {
		t.Errorf("no outputs should yield nil, got %v", got)
	}
	outs := []job.Output{
		{Type: "mvsj", Path: "a.mvsj"}, // no size
		{Type: "image", Path: "b.png", Size: []int{1200, 900}},
	}
	if got := firstOutputSize(outs); len(got) != 2 || got[0] != 1200 || got[1] != 900 {
		t.Errorf("firstOutputSize = %v, want [1200 900]", got)
	}
}

func TestLoadOrBuildJobPreservesDeclaredSize(t *testing.T) {
	dir := t.TempDir()
	jobPath := filepath.Join(dir, "scene.job.yaml")
	spec := `version: 1
runtime: { strict: true }
inputs:
  input: { path: model.cif, format: mmcif }
scene:
  structures:
    - ref: s
      source: input
      components:
        - { ref: p, select: polymer, representation: { type: cartoon } }
outputs:
  - { type: image, path: outputs/big.png, size: [1200, 900] }
`
	if err := os.WriteFile(jobPath, []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	a := app{}

	// --out without an explicit --size must keep the declared 1200x900.
	j, err := a.loadOrBuildJob(jobPath, &renderFlags{out: "custom.png", size: "800x800", sizeExplicit: false})
	if err != nil {
		t.Fatal(err)
	}
	if len(j.Outputs) != 1 || j.Outputs[0].Path != "custom.png" {
		t.Fatalf("expected single redirected output, got %#v", j.Outputs)
	}
	if got := j.Outputs[0].Size; len(got) != 2 || got[0] != 1200 || got[1] != 900 {
		t.Fatalf("declared size not preserved: got %v, want [1200 900]", got)
	}

	// An explicit --size overrides the declared size.
	j2, err := a.loadOrBuildJob(jobPath, &renderFlags{out: "custom.png", size: "640x480", sizeExplicit: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := j2.Outputs[0].Size; len(got) != 2 || got[0] != 640 || got[1] != 480 {
		t.Fatalf("explicit --size ignored: got %v, want [640 480]", got)
	}
}

func TestLooksLikeLocalPath(t *testing.T) {
	localPaths := []string{
		"scene.job.yaml",
		"job.yml",
		"recipe.json",
		"scene.mvsj",
		"model.mvsx",
		"structure.cif",
		"protein.pdb",
		"some/dir/thing",
		"./relative.yaml",
		"/absolute/path",
	}
	for _, input := range localPaths {
		if !looksLikeLocalPath(input) {
			t.Errorf("looksLikeLocalPath(%q) = false, want true", input)
		}
	}

	identifiers := []string{
		"1cbs",
		"4hhb",
		"AF-P12345-F1",
		"6VXX",
	}
	for _, input := range identifiers {
		if looksLikeLocalPath(input) {
			t.Errorf("looksLikeLocalPath(%q) = true, want false (should stay an identifier)", input)
		}
	}
}

func TestLoadOrBuildJobRejectsMissingFile(t *testing.T) {
	a := app{}
	// A path-like argument that does not exist must fail cleanly rather than
	// being fetched as a network identifier.
	if _, err := a.loadOrBuildJob("does-not-exist.job.yaml", &renderFlags{}); err == nil {
		t.Fatal("expected an error for a missing job file, got nil")
	} else if got := err.Error(); got != "no such file: does-not-exist.job.yaml" {
		t.Fatalf("unexpected error: %q", got)
	}
}
