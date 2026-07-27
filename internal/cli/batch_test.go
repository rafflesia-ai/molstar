package cli

import (
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
