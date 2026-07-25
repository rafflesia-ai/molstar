package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeInspectSemanticMode(t *testing.T) {
	cases := map[string]string{
		"":      "auto",
		"auto":  "auto",
		"true":  "true",
		"1":     "true",
		"false": "false",
		"0":     "false",
	}
	for input, expected := range cases {
		actual, err := normalizeInspectSemanticMode(input)
		if err != nil {
			t.Fatalf("normalizeInspectSemanticMode(%q) failed: %v", input, err)
		}
		if actual != expected {
			t.Fatalf("normalizeInspectSemanticMode(%q) = %q, want %q", input, actual, expected)
		}
	}
	if _, err := normalizeInspectSemanticMode("maybe"); err == nil {
		t.Fatal("expected invalid semantic mode to fail")
	}
}

func TestInspectMVSInputReportsSceneCommand(t *testing.T) {
	dir := t.TempDir()
	scenePath := filepath.Join(dir, "scene.mvsj")
	if err := os.WriteFile(scenePath, []byte(`{"metadata":{"version":"1"},"root":{"kind":"root"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := runAppForTest(context.Background(), "inspect", scenePath, "--json")
	if err == nil {
		t.Fatal("expected inspect of MVS scene to fail")
	}
	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("expected JSON inspect error to keep stderr empty, got %q", stderr)
	}
	var failure errorReport
	if err := json.Unmarshal([]byte(stdout), &failure); err != nil {
		t.Fatalf("inspect failure stdout is not JSON: %v\n%s", err, stdout)
	}
	if failure.Error.AgentCode != "invalid_job" {
		t.Fatalf("unexpected inspect error code: %#v", failure)
	}
	if !strings.Contains(failure.Error.Message, "scene validate") || !strings.Contains(failure.Error.Message, "molstar render") {
		t.Fatalf("inspect MVS error should include actionable commands, got %q", failure.Error.Message)
	}
}
