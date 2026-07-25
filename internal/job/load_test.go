package job

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadYAMLJob(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "job.yaml")
	data := []byte(`
version: 1
inputs:
  protein:
    id: 1cbs
scene:
  structures:
    - source: protein
      components:
        - select: polymer
          representation:
            type: cartoon
outputs:
  - type: image
    path: out.png
`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	j, err := LoadRenderFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if j.Inputs["protein"].ID != "1cbs" {
		t.Fatalf("unexpected input: %#v", j.Inputs["protein"])
	}
}

func TestLoadManyBytesJSONL(t *testing.T) {
	data := []byte(`{"version":1,"inputs":{"a":{"id":"1cbs"}},"scene":{"structures":[{"source":"a"}]},"outputs":[{"type":"image","path":"a.png"}]}
{"version":1,"inputs":{"b":{"id":"2hhb"}},"scene":{"structures":[{"source":"b"}]},"outputs":[{"type":"image","path":"b.png"}]}
`)
	jobs, err := LoadManyBytes(data, "stdin")
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(jobs))
	}
}
