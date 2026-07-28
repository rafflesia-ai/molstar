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

func TestRecipeInitValidateCompileAndNormalize(t *testing.T) {
	dir := t.TempDir()
	recipePath := filepath.Join(dir, "ligand.recipe.yaml")
	ctx := context.Background()
	stdout, stderr, err := runAppForTest(ctx,
		"recipe", "init", "ligand",
		"--id", "1cbs",
		"--out", recipePath,
		"--size", "320x240",
	)
	if err != nil {
		t.Fatalf("recipe init failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if _, err := os.Stat(recipePath); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err = runAppForTest(ctx, "recipe", "validate", recipePath, "--json")
	if err != nil {
		t.Fatalf("recipe validate failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var validate map[string]any
	if err := json.Unmarshal([]byte(stdout), &validate); err != nil {
		t.Fatalf("validate stdout is not JSON: %v\n%s", err, stdout)
	}
	if validate["ok"] != true || validate["preset"] != "ligand" {
		t.Fatalf("unexpected validate report: %#v", validate)
	}
	jobPath := filepath.Join(dir, "ligand.job.yaml")
	stdout, stderr, err = runAppForTest(ctx, "recipe", "compile", recipePath, "--out", jobPath, "--json")
	if err != nil {
		t.Fatalf("recipe compile job failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	data, err := os.ReadFile(jobPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "inputs:") || !strings.Contains(string(data), "scene:") {
		t.Fatalf("compiled job did not look like a job:\n%s", string(data))
	}
	stdout, stderr, err = runAppForTest(ctx, "job", "normalize", recipePath, "--json")
	if err != nil {
		t.Fatalf("job normalize recipe failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if !json.Valid([]byte(stdout)) {
		t.Fatalf("normalized recipe output is not JSON:\n%s", stdout)
	}
}

func TestRecipeSchemaCommandAndStrictValidation(t *testing.T) {
	dir := t.TempDir()
	recipePath := filepath.Join(dir, "ligand.recipe.yaml")
	ctx := context.Background()
	stdout, stderr, err := runAppForTest(ctx,
		"recipe", "init", "ligand",
		"--id", "1cbs",
		"--out", recipePath,
		"--size", "320x240",
	)
	if err != nil {
		t.Fatalf("recipe init failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	stdout, stderr, err = runAppForTest(ctx, "recipe", "schema", "--info", "--json")
	if err != nil {
		t.Fatalf("recipe schema info failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var info map[string]any
	if err := json.Unmarshal([]byte(stdout), &info); err != nil {
		t.Fatalf("schema info output is not JSON: %v\n%s", err, stdout)
	}
	if info["schema_id"] == "" || info["recipe_version"] != float64(1) {
		t.Fatalf("unexpected schema info: %#v", info)
	}

	stdout, stderr, err = runAppForTest(ctx, "recipe", "validate", recipePath, "--schema", "--json")
	if err != nil {
		t.Fatalf("schema recipe validate failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var validate map[string]any
	if err := json.Unmarshal([]byte(stdout), &validate); err != nil {
		t.Fatalf("validate stdout is not JSON: %v\n%s", err, stdout)
	}
	if validate["ok"] != true || validate["schema"] != true {
		t.Fatalf("unexpected strict validate report: %#v", validate)
	}

	badPath := filepath.Join(dir, "bad.recipe.yaml")
	if err := os.WriteFile(badPath, []byte(`version: 1
input:
  id: 1cbs
  provider: pdbe
unexpected: true
outputs:
  - type: image
    path: out.png
    size: [64, 64]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err = runAppForTest(ctx, "recipe", "validate", badPath, "--schema", "--json")
	if err == nil {
		t.Fatalf("expected strict recipe validate to reject unknown fields\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	var failure errorReport
	if jsonErr := json.Unmarshal([]byte(stdout), &failure); jsonErr != nil {
		t.Fatalf("failure output is not JSON: %v\n%s", jsonErr, stdout)
	}
	if failure.Error.Code != string(kindValidation) {
		t.Fatalf("expected validation error, got %#v", failure)
	}
}

func TestRecipeExplain(t *testing.T) {
	recipePath := filepath.Join(cliRepoRootForTest(t), "examples/ligand.recipe.yaml")
	stdout, stderr, err := runAppForTest(context.Background(), "recipe", "explain", recipePath, "--schema", "--json")
	if err != nil {
		t.Fatalf("recipe explain failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var report map[string]any
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("recipe explain stdout is not JSON: %v\n%s", err, stdout)
	}
	if report["ok"] != true || report["recipe"] == nil || report["job"] == nil || report["mvs"] == nil {
		t.Fatalf("unexpected recipe explain report: %#v", report)
	}
}

func TestSelectorsCommands(t *testing.T) {
	stdout, stderr, err := runAppForTest(context.Background(), "selectors", "list", "--json")
	if err != nil {
		t.Fatalf("selectors list failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var listed struct {
		OK        bool `json:"ok"`
		Selectors []struct {
			Selector string `json:"selector"`
		} `json:"selectors"`
	}
	if err := json.Unmarshal([]byte(stdout), &listed); err != nil {
		t.Fatalf("selectors list output is not JSON: %v\n%s", err, stdout)
	}
	if !listed.OK || len(listed.Selectors) == 0 {
		t.Fatalf("unexpected selectors list output: %#v", listed)
	}

	stdout, stderr, err = runAppForTest(context.Background(), "selectors", "explain", "chain:A/residue:10-20", "--json")
	if err != nil {
		t.Fatalf("selectors explain failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var explained map[string]any
	if err := json.Unmarshal([]byte(stdout), &explained); err != nil {
		t.Fatalf("selectors explain output is not JSON: %v\n%s", err, stdout)
	}
	if explained["ok"] != true {
		t.Fatalf("unexpected selectors explain output: %#v", explained)
	}
}

func TestFixturesVerifyLocal(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	stdout, stderr, err := runAppForTest(ctx, "fixtures", "verify", "--out-dir", dir, "--json")
	if err != nil {
		t.Fatalf("fixtures verify failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var report struct {
		OK       bool            `json:"ok"`
		Fixtures []fixtureResult `json:"fixtures"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("fixtures verify stdout is not JSON: %v\n%s", err, stdout)
	}
	if !report.OK || len(report.Fixtures) != 3 {
		t.Fatalf("unexpected fixtures report: %#v", report)
	}
	for _, name := range []string{"demo.png", "local-cif.png", "local-scene.mvsx"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("expected fixture output %s: %v", name, err)
		}
	}
}

func TestFixturesVerifyGolden(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	stdout, stderr, err := runAppForTest(ctx, "fixtures", "verify", "--out-dir", dir, "--golden", "--json")
	if err != nil {
		t.Fatalf("fixtures verify --golden failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var report struct {
		OK       bool            `json:"ok"`
		Golden   bool            `json:"golden"`
		Fixtures []fixtureResult `json:"fixtures"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("fixtures verify --golden stdout is not JSON: %v\n%s", err, stdout)
	}
	if !report.OK || !report.Golden || len(report.Fixtures) != 3 {
		t.Fatalf("unexpected golden fixture report: %#v", report)
	}
	for _, fixture := range report.Fixtures {
		if len(fixture.Golden) == 0 {
			t.Fatalf("expected golden results for %s: %#v", fixture.Name, fixture)
		}
		for _, golden := range fixture.Golden {
			if !golden.OK {
				t.Fatalf("golden failed for %s: %#v", fixture.Name, golden)
			}
		}
	}
}

func TestCompletionCommand(t *testing.T) {
	stdout, stderr, err := runAppForTest(context.Background(), "completion", "bash")
	if err != nil {
		t.Fatalf("completion failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "molstar") {
		t.Fatalf("completion output did not mention molstar")
	}
}

// A preset's focus names one of that preset's own components. When a recipe
// supplies its own components the preset's are discarded, so inheriting the
// preset's focus left the scene pointing at a component that no longer existed:
// compilation failed naming a focus the author never wrote.
func TestRecipePresetFocusIsDroppedWithCustomComponents(t *testing.T) {
	a := app{}

	// Preset alone: its components and its focus are both used.
	preset, err := a.recipeToJob(recipe.Recipe{
		Version: 1, Kind: "recipe", Preset: "ligand",
		Input: job.Input{ID: "1cbs", Provider: "pdbe"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if preset.Scene.Camera.Focus != "ligand" {
		t.Fatalf("preset focus = %q, want ligand", preset.Scene.Camera.Focus)
	}

	// Custom components: the preset's focus must not survive, because the
	// component it names is gone.
	custom, err := a.recipeToJob(recipe.Recipe{
		Version: 1, Kind: "recipe", Preset: "ligand",
		Input: job.Input{ID: "1cbs", Provider: "pdbe"},
		Components: []job.Component{{
			Ref: "custom", Select: "chain:A",
			Representation: job.Representation{Type: "surface"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if custom.Scene.Camera.Focus != "" {
		t.Fatalf("focus = %q, want empty when the preset's components were replaced", custom.Scene.Camera.Focus)
	}
	if err := custom.ValidateRender(); err != nil {
		t.Fatalf("recipe with custom components should compile: %v", err)
	}
	if _, err := mvs.Compile(custom); err != nil {
		t.Fatalf("scene should compile: %v", err)
	}

	// An explicit focus always wins.
	explicit, err := a.recipeToJob(recipe.Recipe{
		Version: 1, Kind: "recipe", Preset: "ligand", Focus: "custom",
		Input: job.Input{ID: "1cbs", Provider: "pdbe"},
		Components: []job.Component{{
			Ref: "custom", Select: "chain:A",
			Representation: job.Representation{Type: "surface"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if explicit.Scene.Camera.Focus != "custom" {
		t.Fatalf("explicit focus = %q, want custom", explicit.Scene.Camera.Focus)
	}
}
