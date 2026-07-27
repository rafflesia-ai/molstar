package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rafflesia-ai/molstar/internal/job"
	"github.com/rafflesia-ai/molstar/internal/mvs"
	"github.com/rafflesia-ai/molstar/internal/recipe"
)

func TestMolstarExamplesContract(t *testing.T) {
	root := cliRepoRootForTest(t)
	examplePaths, err := filepath.Glob(filepath.Join(root, "examples", "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(examplePaths) == 0 {
		t.Fatal("expected example YAML files")
	}
	for _, path := range examplePaths {
		name := filepath.Base(path)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "mdsrv.job/v1") {
			continue
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			if strings.Contains(string(data), "kind: recipe") {
				stdout, stderr, err := runAppForTest(ctx, "recipe", "validate", path, "--schema", "--json")
				if err != nil {
					t.Fatalf("recipe validate failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
				}
				requireOKJSON(t, stdout)
				stdout, stderr, err = runAppForTest(ctx, "recipe", "explain", path, "--schema", "--json")
				if err != nil {
					t.Fatalf("recipe explain failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
				}
				requireOKJSON(t, stdout)
				r, err := recipe.LoadBytes(data, path)
				if err != nil {
					t.Fatalf("decode recipe: %v", err)
				}
				j, err := app{}.recipeToJob(r)
				if err != nil {
					t.Fatalf("compile recipe to job: %v", err)
				}
				if _, err := mvs.Compile(j); err != nil {
					t.Fatalf("compile recipe MVS: %v", err)
				}
				return
			}
			stdout, stderr, err := runAppForTest(ctx, "job", "validate", path, "--schema", "--json")
			if err != nil {
				t.Fatalf("job validate failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
			}
			requireOKJSON(t, stdout)
			j, err := job.LoadSchemaRenderBytes(data, path)
			if err != nil {
				t.Fatalf("schema load job: %v", err)
			}
			if _, err := mvs.Compile(j); err != nil {
				t.Fatalf("compile job MVS: %v", err)
			}
		})
	}
}

func TestLocalExampleAgentWorkflowContract(t *testing.T) {
	dir := t.TempDir()
	cifPath := filepath.Join(dir, "one.cif")
	if err := os.WriteFile(cifPath, []byte(oneAtomCIF), 0o644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "local.png")
	j := minimalServeJob(t, dir)
	j.Inputs["input"] = job.Input{Path: cifPath, Format: "mmcif"}
	j.Scene.Canvas = job.Canvas{Background: "white"}
	j.Scene.Structures[0].Components[0].Ref = "all"
	j.Scene.Structures[0].Components[0].Representation = job.Representation{Type: "spacefill", Color: "#cc3399"}
	j.Scene.Camera = job.Camera{Focus: "all"}
	j.Outputs = []job.Output{{Type: "image", Path: output, Size: []int{96, 72}}}
	jobPath := filepath.Join(dir, "local.job.json")
	data, err := json.Marshal(j)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jobPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, args := range [][]string{
		{"job", "validate", jobPath, "--schema", "--json"},
		{"job", "explain", jobPath, "--json"},
		{"inspect", jobPath, "--semantic=auto", "--json"},
		{"render", jobPath, "--dry-run", "--json"},
		{"render", jobPath, "--json"},
	} {
		stdout, stderr, err := runAppForTest(ctx, args...)
		if err != nil {
			t.Fatalf("%v failed: %v\nstdout:\n%s\nstderr:\n%s", args, err, stdout, stderr)
		}
		requireOKJSON(t, stdout)
	}
	report, err := verifyOutput(output, job.Output{Type: "image", Path: output, Size: []int{96, 72}})
	if err != nil {
		t.Fatal(err)
	}
	if err := checkOutputReportAgainstGolden(report, demoVisualGolden); err != nil {
		t.Fatal(err)
	}
}

func requireOKJSON(t *testing.T, stdout string) {
	t.Helper()
	var report map[string]any
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if report["ok"] != true {
		t.Fatalf("expected ok JSON: %#v", report)
	}
}
