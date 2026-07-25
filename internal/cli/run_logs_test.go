package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sacha-ichbiah/molstar/internal/job"
	"github.com/sacha-ichbiah/molstar/internal/mvs"
)

func TestRunLogAssetPolicy(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	runDir := t.TempDir()
	t.Setenv("MOLSTAR_RUNS_DIR", runDir)

	output := filepath.Join(t.TempDir(), "no-assets.png")
	stdout, stderr, err := runAppForTest(ctx,
		"render", "--demo",
		"--out", output,
		"--size", "96x72",
		"--json",
		"--log-assets=false",
	)
	if err != nil {
		t.Fatalf("render with --log-assets=false failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var renderReport renderReport
	if err := json.Unmarshal([]byte(stdout), &renderReport); err != nil {
		t.Fatalf("render stdout is not JSON: %v\n%s", err, stdout)
	}
	if renderReport.RunLog == "" {
		t.Fatalf("render did not write run log: %#v", renderReport)
	}

	stdout, stderr, err = runAppForTest(ctx, "logs", "--last", "--dir", runDir, "--json")
	if err != nil {
		t.Fatalf("logs --last failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var logReport struct {
		OK  bool           `json:"ok"`
		Run runLogEnvelope `json:"run"`
	}
	if err := json.Unmarshal([]byte(stdout), &logReport); err != nil {
		t.Fatalf("logs stdout is not JSON: %v\n%s", err, stdout)
	}
	if !logReport.OK || len(logReport.Run.Assets) != 0 || logReport.Run.Replay.FullyReplayable {
		t.Fatalf("expected no embedded assets and non-fully-replayable report: %#v", logReport.Run)
	}
	if !strings.Contains(strings.Join(logReport.Run.Replay.Warnings, "\n"), "include_inputs=false") {
		t.Fatalf("missing run-log asset policy warning: %#v", logReport.Run.Replay.Warnings)
	}

	limitedOutput := filepath.Join(t.TempDir(), "too-small.png")
	stdout, stderr, err = runAppForTest(ctx,
		"render", "--demo",
		"--out", limitedOutput,
		"--size", "96x72",
		"--json",
		"--log-asset-max-bytes", "1",
	)
	if err != nil {
		t.Fatalf("render with small asset limit failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	stdout, stderr, err = runAppForTest(ctx, "logs", "--last", "--dir", runDir, "--json")
	if err != nil {
		t.Fatalf("logs --last after small limit failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if err := json.Unmarshal([]byte(stdout), &logReport); err != nil {
		t.Fatalf("logs stdout is not JSON: %v\n%s", err, stdout)
	}
	if len(logReport.Run.Assets) != 0 || !strings.Contains(strings.Join(logReport.Run.Replay.Warnings, "\n"), "max_single_input_bytes=1") {
		t.Fatalf("expected per-input limit warning and no assets: %#v", logReport.Run)
	}
}

func TestMVSRunLogRerun(t *testing.T) {
	restore := configureRuntimeForCLITest()
	defer restore()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dir := t.TempDir()
	cifPath := filepath.Join(dir, "one.cif")
	if err := os.WriteFile(cifPath, []byte(oneAtomCIF), 0o644); err != nil {
		t.Fatal(err)
	}
	j := job.Job{
		Version: 1,
		Inputs: map[string]job.Input{
			"input": {Path: cifPath, Format: "mmcif"},
		},
		Scene: job.Scene{
			Canvas: job.Canvas{Background: "white"},
			Structures: []job.Structure{{
				Ref:    "structure",
				Source: "input",
				Components: []job.Component{{
					Ref:    "all",
					Select: "all",
					Representation: job.Representation{
						Type:  "spacefill",
						Color: "#cc3399",
					},
				}},
			}},
			Camera: job.Camera{Focus: "all"},
		},
	}
	compiled, err := mvs.Compile(j)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "from-mvs.png")
	envelope := runLogEnvelope{
		ID:        "mvs-run",
		Command:   "render",
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Report: renderReport{
			OK:          true,
			Input:       "scene.mvsj",
			MVSDocument: &compiled.Document,
			OutputFiles: []outputReport{{Path: output, Type: "image", Width: 64, Height: 64}},
		},
	}
	replay := replayInfoForEnvelope(envelope)
	if !replay.Replayable || replay.FullyReplayable {
		t.Fatalf("expected locally replayable but not portable MVS replay info: %#v", replay)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	a := app{stdin: strings.NewReader(""), stdout: &stdout, stderr: &stderr}
	rerunDir := filepath.Join(dir, "rerun")
	report, err := a.rerunRunLog(ctx, envelope, rerunDir)
	if err != nil {
		t.Fatalf("MVS run-log rerun failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	if !report.OK || len(report.OutputFiles) == 0 {
		t.Fatalf("unexpected MVS rerun report: %#v", report)
	}
	if err := requireNonEmptyFile(filepath.Join(rerunDir, filepath.Base(output))); err != nil {
		t.Fatal(err)
	}
}
