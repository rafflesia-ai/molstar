package job

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateSchemaBytesAcceptsValidYAML(t *testing.T) {
	data := []byte(`version: 1
inputs:
  input:
    id: 1cbs
scene:
  structures:
    - source: input
      components:
        - select: polymer
outputs:
  - type: image
    path: out.png
`)
	if err := ValidateSchemaBytes(data, "job.yaml"); err != nil {
		t.Fatal(err)
	}
}

func TestValidateSchemaBytesRejectsUnknownField(t *testing.T) {
	data := []byte(`{
  "version": 1,
  "unexpected": true,
  "inputs": {
    "input": { "id": "1cbs" }
  },
  "scene": {
    "structures": [
      { "source": "input", "components": [{ "select": "polymer" }] }
    ]
  },
  "outputs": [
    { "type": "image", "path": "out.png" }
  ]
}`)
	if err := ValidateSchemaBytes(data, "job.json"); err == nil {
		t.Fatal("expected schema validation error")
	}
}

func TestJSONSchemaGolden(t *testing.T) {
	data, err := json.MarshalIndent(JSONSchema(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	golden, err := os.ReadFile(filepath.Join("..", "..", "schema", "headlessmolstar-job-v1.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(golden) {
		t.Fatal("generated JSON schema does not match schema/headlessmolstar-job-v1.schema.json; run `go run ./cmd/molstar job schema --out schema/headlessmolstar-job-v1.schema.json`")
	}
}
