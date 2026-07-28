package render

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rafflesia-ai/molstar/internal/job"
	"github.com/rafflesia-ai/molstar/internal/mvs"
)

// Local inputs are handed to the renderer as RFC 8089 file:// URLs, so a path
// containing a space, a non-ASCII character, or a parenthesis arrives
// percent-encoded. Mol*'s Node file reader strips the scheme with a bare
// substring and never decodes, so those renders failed with ENOENT — which hits
// ordinary paths like "~/Desktop/my project/model.pdb". scripts/render-mvs.js
// now decodes on the fallback.
func TestRenderReadsLocalInputsUnderAwkwardPaths(t *testing.T) {
	cases := map[string]string{
		"spaces":               "my project",
		"non ascii":            "données été",
		"parens and ampersand": "raw (v2) & more",
		"literal percent":      "100%pure",
	}
	for name, dirName := range cases {
		t.Run(name, func(t *testing.T) {
			base := t.TempDir()
			dir := filepath.Join(base, dirName)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			cifPath := filepath.Join(dir, "one atom.cif")
			if err := os.WriteFile(cifPath, []byte(oneAtomCIF), 0o644); err != nil {
				t.Fatal(err)
			}
			output := job.Output{Type: "image", Path: filepath.Join(dir, "probe out.png"), Size: []int{96, 72}}
			j := job.Job{
				Version: 1,
				Inputs:  map[string]job.Input{"probe": {Path: cifPath, Format: "mmcif"}},
				Scene: job.Scene{
					Canvas: job.Canvas{Background: "white"},
					Structures: []job.Structure{{
						Source: "probe",
						Components: []job.Component{{
							Ref:            "atom",
							Select:         "all",
							Representation: job.Representation{Type: "spacefill", Color: "#cc3399"},
						}},
					}},
					Camera: job.Camera{Focus: "all"},
				},
				Outputs: []job.Output{output},
			}
			compiled, err := mvs.Compile(j)
			if err != nil {
				t.Fatal(err)
			}
			scenePath := filepath.Join(dir, "probe scene.mvsj")
			if err := mvs.WriteFile(scenePath, compiled.Document); err != nil {
				t.Fatal(err)
			}

			runner := NewMolstar()
			runner.Stdout = nil
			runner.Stderr = nil
			runner.Quiet = true
			if !Available(runner.RendererCommand) {
				t.Skipf("renderer command is not available: %v", runner.RendererCommand)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
			defer cancel()
			if _, err := runner.RenderImage(ctx, ImageRequest{InputMVS: scenePath, Output: output}); err != nil {
				t.Fatalf("render under %q failed: %v", dirName, err)
			}
			assertPNGOutput(t, output.Path, 96, 72)
		})
	}
}
