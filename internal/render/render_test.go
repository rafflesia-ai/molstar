package render

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"math/bits"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sacha-ichbiah/molstar/internal/job"
	"github.com/sacha-ichbiah/molstar/internal/mvs"
)

func TestGoldenRenderProbe(t *testing.T) {
	runner := NewMolstar()
	if !Available(runner.RendererCommand) {
		t.Skipf("renderer command is not available: %v", runner.RendererCommand)
	}
	dir := t.TempDir()
	cifPath := filepath.Join(dir, "one.cif")
	if err := os.WriteFile(cifPath, []byte(oneAtomCIF), 0o644); err != nil {
		t.Fatal(err)
	}
	j := job.Job{
		Version: 1,
		Inputs: map[string]job.Input{
			"probe": {Path: cifPath, Format: "mmcif"},
		},
		Scene: job.Scene{
			Canvas: job.Canvas{Background: "white"},
			Structures: []job.Structure{{
				Source: "probe",
				Components: []job.Component{{
					Ref:    "atom",
					Select: "all",
					Representation: job.Representation{
						Type:  "spacefill",
						Color: "#cc3399",
					},
				}},
			}},
			Camera: job.Camera{Focus: "all"},
		},
		Outputs: []job.Output{{
			Type: "image",
			Path: filepath.Join(dir, "probe.png"),
			Size: []int{96, 72},
		}},
	}
	compiled, err := mvs.Compile(j)
	if err != nil {
		t.Fatal(err)
	}
	scenePath := filepath.Join(dir, "probe.mvsj")
	if err := mvs.WriteFile(scenePath, compiled.Document); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	runner.Stdout = nil
	runner.Stderr = nil
	if _, err := runner.RenderImage(ctx, ImageRequest{InputMVS: scenePath, Output: j.Outputs[0]}); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(j.Outputs[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	img, err := png.Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	bounds := img.Bounds()
	if bounds.Dx() != 96 || bounds.Dy() != 72 {
		t.Fatalf("unexpected image size %dx%d", bounds.Dx(), bounds.Dy())
	}
	if nonWhitePixelCount(img) < 25 {
		t.Fatalf("render appears blank")
	}
	hash := averageHash(img)
	const expectedAverageHash = "ffffefe7e7e7ffff"
	if distance := hashDistance(hash, expectedAverageHash); distance > 6 {
		t.Fatalf("render average hash drifted too far: got %s, want near %s, distance %d", hash, expectedAverageHash, distance)
	}
}

func TestWorkerPoolRenderProbe(t *testing.T) {
	command := defaultWorkerCommand()
	if !Available(command) {
		t.Skipf("renderer worker command is not available: %v", command)
	}
	scenePath, output := writeProbeScene(t, "#cc3399")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	pool, err := NewWorkerPool(1, command, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pool.Close() }()

	capabilities, err := pool.Capabilities(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if capabilities["protocol"] != "headlessmolstar-worker-v1" {
		t.Fatalf("unexpected worker capabilities: %#v", capabilities)
	}

	result, err := pool.RenderImage(ctx, ImageRequest{InputMVS: scenePath, Output: output})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Worker || result.WorkerID == 0 {
		t.Fatalf("expected worker command result, got %#v", result)
	}
	assertPNGOutput(t, output.Path, 96, 72)
}

func TestRendererUsesInstalledConfigOutsideRepo(t *testing.T) {
	root := repoRootForTest(t)
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := WriteRuntimeConfig(configPath, RuntimeConfigForHome(root)); err != nil {
		t.Fatal(err)
	}
	t.Setenv(ConfigEnv, configPath)
	t.Setenv("MOLSTAR_RENDER", "")
	t.Setenv("MOLSTAR_RENDER_FALLBACK", "")
	t.Setenv("MOLSTAR_VALIDATE", "")
	chdirForTest(t, t.TempDir())

	runner := NewMolstar()
	if !Available(runner.RendererCommand) {
		t.Fatalf("configured renderer is not available: %v", runner.RendererCommand)
	}
	if got, want := runner.RendererCommand, RuntimeConfigForHome(root).RendererCommand; !sameCommand(got, want) {
		t.Fatalf("renderer command = %#v, want %#v", got, want)
	}
	if !Available(runner.ValidateCommand) {
		t.Fatalf("configured validator is not available: %v", runner.ValidateCommand)
	}

	scenePath, output := writeProbeScene(t, "chain")
	runner.Stdout = nil
	runner.Stderr = nil
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := runner.RenderImage(ctx, ImageRequest{InputMVS: scenePath, Output: output}); err != nil {
		t.Fatal(err)
	}
	assertPNGOutput(t, output.Path, 96, 72)
}

func TestRendererFallsBackToMolstarMVSRender(t *testing.T) {
	base := NewMolstar()
	if !Available(base.RendererFallbackCommand) {
		t.Skipf("fallback renderer command is not available: %v", base.RendererFallbackCommand)
	}
	falsePath, err := exec.LookPath("false")
	if err != nil {
		t.Skip("false command is not available")
	}
	scenePath, output := writeProbeScene(t, "#cc3399")
	runner := Molstar{
		RendererCommand:         []string{falsePath},
		RendererFallbackCommand: base.RendererFallbackCommand,
		Stdout:                  nil,
		Stderr:                  nil,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := runner.RenderImage(ctx, ImageRequest{InputMVS: scenePath, Output: output})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.FallbackOf) == 0 || result.FallbackReason == "" {
		t.Fatalf("expected fallback metadata, got %#v", result)
	}
	assertPNGOutput(t, output.Path, 96, 72)
}

func nonWhitePixelCount(img image.Image) int {
	bounds := img.Bounds()
	count := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			if a > 0 && (r < 0xf000 || g < 0xf000 || b < 0xf000) {
				count++
			}
		}
	}
	return count
}

func averageHash(img image.Image) string {
	bounds := img.Bounds()
	var values [64]uint32
	var total uint64
	for y := 0; y < 8; y++ {
		sourceY := bounds.Min.Y + (y*bounds.Dy()+bounds.Dy()/2)/8
		if sourceY >= bounds.Max.Y {
			sourceY = bounds.Max.Y - 1
		}
		for x := 0; x < 8; x++ {
			sourceX := bounds.Min.X + (x*bounds.Dx()+bounds.Dx()/2)/8
			if sourceX >= bounds.Max.X {
				sourceX = bounds.Max.X - 1
			}
			r, g, b, _ := img.At(sourceX, sourceY).RGBA()
			gray := (299*r + 587*g + 114*b) / 1000
			index := y*8 + x
			values[index] = gray
			total += uint64(gray)
		}
	}
	average := total / 64
	var value uint64
	for i, gray := range values {
		if uint64(gray) >= average {
			value |= 1 << uint(63-i)
		}
	}
	return fmt.Sprintf("%016x", value)
}

func hashDistance(a, b string) int {
	av, errA := strconv.ParseUint(a, 16, 64)
	bv, errB := strconv.ParseUint(b, 16, 64)
	if errA != nil || errB != nil {
		return 64
	}
	return bits.OnesCount64(av ^ bv)
}

func writeProbeScene(t *testing.T, color string) (string, job.Output) {
	t.Helper()
	dir := t.TempDir()
	cifPath := filepath.Join(dir, "one.cif")
	if err := os.WriteFile(cifPath, []byte(oneAtomCIF), 0o644); err != nil {
		t.Fatal(err)
	}
	output := job.Output{
		Type: "image",
		Path: filepath.Join(dir, "probe.png"),
		Size: []int{96, 72},
	}
	j := job.Job{
		Version: 1,
		Inputs: map[string]job.Input{
			"probe": {Path: cifPath, Format: "mmcif"},
		},
		Scene: job.Scene{
			Canvas: job.Canvas{Background: "white"},
			Structures: []job.Structure{{
				Source: "probe",
				Components: []job.Component{{
					Ref:    "atom",
					Select: "all",
					Representation: job.Representation{
						Type:  "spacefill",
						Color: color,
					},
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
	return scenePath, output
}

func assertPNGOutput(t *testing.T, path string, width, height int) {
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
	bounds := img.Bounds()
	if bounds.Dx() != width || bounds.Dy() != height {
		t.Fatalf("unexpected image size %dx%d", bounds.Dx(), bounds.Dy())
	}
	if nonWhitePixelCount(img) < 25 {
		t.Fatalf("render appears blank")
	}
}

func repoRootForTest(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repo root not found from %s: %v", file, err)
	}
	return root
}

func TestLimitedWriterCapsLiveOutput(t *testing.T) {
	var sink bytes.Buffer
	lw := &limitedWriter{w: &sink, remaining: 64}

	// A leading meaningful line, then a large runaway dump.
	lead := "Error: Download failed with status code 404\n"
	if _, err := lw.Write([]byte(lead)); err != nil {
		t.Fatal(err)
	}
	dump := make([]byte, 100_000)
	for i := range dump {
		dump[i] = 'x'
	}
	if _, err := lw.Write(dump); err != nil {
		t.Fatal(err)
	}
	// Further writes after the cap must not grow the sink beyond the notice.
	if _, err := lw.Write([]byte("more junk")); err != nil {
		t.Fatal(err)
	}

	out := sink.String()
	if !strings.HasPrefix(out, lead) {
		t.Fatalf("leading error was not preserved, got prefix %q", out[:min(len(out), 60)])
	}
	if !strings.Contains(out, "[renderer output truncated]") {
		t.Fatal("expected a truncation notice")
	}
	if len(out) > 64+len("\n[renderer output truncated]\n")+8 {
		t.Fatalf("live output not capped: %d bytes", len(out))
	}
	if strings.Contains(out, "more junk") {
		t.Fatal("output past the cap should have been dropped")
	}
}

// evalSymlinksForTest resolves symlinks so path comparisons match the
// working directory reported by os.Getwd (macOS maps /var to /private/var).
func evalSymlinksForTest(t *testing.T, dir string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

// writeFakeNode writes a stand-in "node" binary at node_modules/node/bin/node
// under root. When runnable is true it is an executable shell script that
// answers --version; otherwise it is a non-executable file that cannot run
// (simulating a wrong-architecture or corrupt bundled node).
func writeFakeNode(t *testing.T, root string, runnable bool) string {
	t.Helper()
	nodePath := filepath.Join(root, "node_modules", "node", "bin", "node")
	if err := os.MkdirAll(filepath.Dir(nodePath), 0o755); err != nil {
		t.Fatal(err)
	}
	mode := os.FileMode(0o644)
	body := []byte("not a real binary\x00\x00")
	if runnable {
		mode = 0o755
		body = []byte("#!/bin/sh\necho v0.0.0-fake\n")
	}
	if err := os.WriteFile(nodePath, body, mode); err != nil {
		t.Fatal(err)
	}
	return nodePath
}

func TestFindLocalScriptFallsBackToSystemNode(t *testing.T) {
	systemNode, err := exec.LookPath("node")
	if err != nil {
		t.Skip("system node is not available on PATH")
	}
	root := evalSymlinksForTest(t, t.TempDir())
	bundled := writeFakeNode(t, root, false) // corrupt / non-runnable bundled node
	scriptPath := filepath.Join(root, "scripts", "render-mvs.js")
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scriptPath, []byte("// fake renderer\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	chdirForTest(t, root)

	node, script := findLocalScript("scripts", "render-mvs.js")
	if node == bundled {
		t.Fatalf("resolver used the non-runnable bundled node %q instead of falling back", bundled)
	}
	if node != systemNode {
		t.Fatalf("resolver node = %q, want system node %q", node, systemNode)
	}
	if script != scriptPath {
		t.Fatalf("resolver script = %q, want %q", script, scriptPath)
	}
}

func TestFindLocalScriptPrefersRunnableBundledNode(t *testing.T) {
	root := evalSymlinksForTest(t, t.TempDir())
	bundled := writeFakeNode(t, root, true) // runnable bundled node
	scriptPath := filepath.Join(root, "scripts", "render-mvs.js")
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scriptPath, []byte("// fake renderer\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	chdirForTest(t, root)

	node, _ := findLocalScript("scripts", "render-mvs.js")
	if node != bundled {
		t.Fatalf("resolver node = %q, want bundled node %q", node, bundled)
	}
}

func chdirForTest(t *testing.T, dir string) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(previous)
	})
}

const oneAtomCIF = `data_one
_entry.id one
_cell.entry_id one
_cell.length_a 1
_cell.length_b 1
_cell.length_c 1
_cell.angle_alpha 90
_cell.angle_beta 90
_cell.angle_gamma 90
_symmetry.entry_id one
_symmetry.space_group_name_H-M 'P 1'
loop_
_atom_site.group_PDB
_atom_site.id
_atom_site.type_symbol
_atom_site.label_atom_id
_atom_site.label_alt_id
_atom_site.label_comp_id
_atom_site.label_asym_id
_atom_site.label_entity_id
_atom_site.label_seq_id
_atom_site.pdbx_PDB_ins_code
_atom_site.Cartn_x
_atom_site.Cartn_y
_atom_site.Cartn_z
_atom_site.occupancy
_atom_site.B_iso_or_equiv
_atom_site.pdbx_formal_charge
_atom_site.auth_seq_id
_atom_site.auth_comp_id
_atom_site.auth_asym_id
_atom_site.auth_atom_id
_atom_site.pdbx_PDB_model_num
ATOM 1 C C . LIG A 1 1 ? 0.0 0.0 0.0 1.00 10.0 ? 1 LIG A C 1
`
