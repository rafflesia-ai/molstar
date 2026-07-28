package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rafflesia-ai/molstar/internal/job"
)

func imageJob(inputID string) job.Job {
	return job.Job{
		Version: 1,
		Inputs:  map[string]job.Input{"input": {ID: inputID}},
		Outputs: []job.Output{{Type: "image", Path: "scene.png"}},
	}
}

func TestDetectOutputCollisions(t *testing.T) {
	flags := &batchFlags{outTemplate: "renders/{id}.{ext}"}
	jobs := []job.Job{imageJob("1cbs"), imageJob("1cbs")}

	err := detectOutputCollisions(jobs, flags)
	if err == nil {
		t.Fatal("expected collision error when two jobs resolve to the same {id} output")
	}
	if !strings.Contains(err.Error(), "{index}") {
		t.Fatalf("collision error should suggest {index}, got: %v", err)
	}

	// Adding {index} disambiguates the two jobs.
	flags.outTemplate = "renders/{id}-{index}.{ext}"
	if err := detectOutputCollisions(jobs, flags); err != nil {
		t.Fatalf("unexpected collision with {index} template: %v", err)
	}

	// Distinct inputs never collide under a plain {id} template.
	flags.outTemplate = "renders/{id}.{ext}"
	if err := detectOutputCollisions([]job.Job{imageJob("1cbs"), imageJob("4hhb")}, flags); err != nil {
		t.Fatalf("distinct inputs should not collide: %v", err)
	}
}

func TestJobIDDeterministicAcrossInputs(t *testing.T) {
	j := job.Job{Inputs: map[string]job.Input{
		"zeta":  {ID: "zzz"},
		"alpha": {ID: "aaa"},
		"mid":   {ID: "mmm"},
	}}
	// The alphabetically-first ref ("alpha") must win on every call.
	first := jobID(j, 0)
	for i := 0; i < 50; i++ {
		if got := jobID(j, 0); got != first {
			t.Fatalf("jobID not deterministic: got %q then %q", first, got)
		}
	}
	if first != "aaa" {
		t.Fatalf("jobID = %q, want deterministic first-by-ref %q", first, "aaa")
	}
}

func TestSummarizeBatch(t *testing.T) {
	reports := []batchReport{
		{OK: true, Index: 0, Outputs: []string{"a.png"}},
		{OK: true, Index: 1, Skipped: true, Outputs: []string{"b.png"}},
		{OK: false, Index: 2, Error: "failed"},
	}
	summary := summarizeBatch(4, reports)
	if summary.OK {
		t.Fatal("summary should fail when one job fails and one is missing")
	}
	if summary.Total != 4 || summary.Completed != 3 || summary.Succeeded != 2 || summary.Skipped != 1 || summary.Failed != 2 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
	if len(summary.Outputs) != 2 {
		t.Fatalf("unexpected outputs: %#v", summary.Outputs)
	}
}

// batch --json is documented as JSON Lines: one report per job plus a summary.
// When a job failed, the generic error handler also appended a pretty-printed,
// multi-line error envelope to the same stream, so every line-by-line consumer
// hit a bare "{" and failed to parse. The aggregate was additionally hardcoded
// to render_failed/renderer_unavailable/retryable, which told callers to retry
// a batch of bad selectors.
func TestBatchJSONStreamStaysLineDelimited(t *testing.T) {
	dir := t.TempDir()
	modelPath := filepath.Join(dir, "one.cif")
	if err := os.WriteFile(modelPath, []byte(oneAtomCIF), 0o644); err != nil {
		t.Fatal(err)
	}
	// A selector that matches nothing renders blank, which is a scene failure.
	failing := job.Job{
		Version: 1,
		Runtime: job.Runtime{AllowPaths: []string{dir}},
		Inputs:  map[string]job.Input{"p": {Path: modelPath, Format: "mmcif"}},
		Scene: job.Scene{Structures: []job.Structure{{
			Ref:        "s",
			Source:     "p",
			Components: []job.Component{{Ref: "c", Select: "chain:ZZZ", Representation: job.Representation{Type: "cartoon"}}},
		}}},
		Outputs: []job.Output{{Type: "image", Path: filepath.Join(dir, "bad.png"), Size: []int{64, 48}}},
	}
	line, err := json.Marshal(failing)
	if err != nil {
		t.Fatal(err)
	}
	batchPath := filepath.Join(dir, "jobs.jsonl")
	if err := os.WriteFile(batchPath, append(line, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, _, runErr := runAppForTest(context.Background(), "batch", batchPath, "--continue-on-error", "--json")
	if runErr == nil {
		t.Fatal("expected a failing batch to return an error")
	}
	if got := agentErrorCode(runErr); got != "invalid_job" {
		t.Fatalf("aggregate agent_code = %q, want invalid_job for a scene failure", got)
	}
	if errorRetryable(runErr) {
		t.Fatal("a batch of non-retryable failures must not be reported as retryable")
	}

	for i, raw := range strings.Split(strings.TrimSpace(stdout), "\n") {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		var decoded map[string]any
		if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
			t.Fatalf("batch stdout line %d is not a complete JSON object: %v\n%q", i, err, raw)
		}
	}
}

// Retrying a job whose failure is not retryable cannot succeed and just burns a
// render slot.
func TestBatchDoesNotRetryNonRetryableFailures(t *testing.T) {
	if !errorRetryable(batchReportError(batchReport{Error: `cache input "p": dial tcp: lookup x.invalid: no such host`})) {
		t.Fatal("a transport failure should be retryable")
	}
	if errorRetryable(batchReportError(batchReport{Error: "output out.png appears blank: the scene rendered no visible geometry"})) {
		t.Fatal("a blank scene should not be retryable")
	}
	if batchReportError(batchReport{}) != nil {
		t.Fatal("a report without an error should produce no error")
	}
}
