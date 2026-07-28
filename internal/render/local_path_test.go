package render

import (
	"context"
	"image/png"
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

// outputs[].transparent is in the job schema, the Python client, and the OpenAPI
// spec, but was never passed to the renderer: it rendered a fully opaque image.
// Transparency also has to be set on the image pass, not only the Canvas3D — the
// saved PNG comes from the image pass, so setting it on the canvas alone left
// the background opaque.
func TestTransparentOutputProducesAnAlphaChannel(t *testing.T) {
	render := func(t *testing.T, transparent bool) [4]byte {
		t.Helper()
		dir := t.TempDir()
		cifPath := filepath.Join(dir, "one.cif")
		if err := os.WriteFile(cifPath, []byte(oneAtomCIF), 0o644); err != nil {
			t.Fatal(err)
		}
		output := job.Output{
			Type:        "image",
			Path:        filepath.Join(dir, "probe.png"),
			Size:        []int{96, 72},
			Transparent: transparent,
		}
		j := job.Job{
			Version: 1,
			Inputs:  map[string]job.Input{"probe": {Path: cifPath, Format: "mmcif"}},
			Scene: job.Scene{
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
		scenePath := filepath.Join(dir, "probe.mvsj")
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
			t.Fatalf("render failed: %v", err)
		}
		return topLeftPixel(t, output.Path)
	}

	opaque := render(t, false)
	if opaque[3] != 0xff {
		t.Fatalf("a normal render should have an opaque background, got %v", opaque)
	}
	clear := render(t, true)
	if clear[3] != 0x00 {
		t.Fatalf("transparent: true should leave the background unpainted, got %v", clear)
	}
}

// topLeftPixel decodes a PNG far enough to read its first pixel, which for these
// probes is background.
func topLeftPixel(t *testing.T, path string) [4]byte {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	img, err := png.Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, a := img.At(img.Bounds().Min.X, img.Bounds().Min.Y).RGBA()
	return [4]byte{byte(r >> 8), byte(g >> 8), byte(b >> 8), byte(a >> 8)}
}
