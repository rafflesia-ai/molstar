package render

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// InspectMVS used to parse CommandResult.Stdout, which is truncated to 16 KiB so
// a runaway renderer cannot bloat a report. Any structure whose inspect payload
// exceeded that limit — 4HHB does, at ~24 KB — failed with a JSON syntax error
// rather than returning stats. The full stdout is now kept for parsing while the
// reported copy stays truncated.
func TestInspectMVSParsesPayloadsLargerThanTheReportLimit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub renderer script is POSIX shell")
	}
	dir := t.TempDir()

	// A payload comfortably past the 16 KiB report truncation limit.
	refs := make([]map[string]any, 0, 400)
	for i := 0; i < 400; i++ {
		refs = append(refs, map[string]any{
			"ref":         "component_with_a_reasonably_long_name",
			"state_ref":   "!mvs:0123456789abcdef:0",
			"description": "padding so the payload exceeds the report truncation limit",
		})
	}
	payload, err := json.Marshal(map[string]any{"ok": true, "refs": refs})
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) <= 16<<10 {
		t.Fatalf("test payload is %d bytes, needs to exceed the 16 KiB report limit", len(payload))
	}

	payloadPath := filepath.Join(dir, "payload.json")
	if err := os.WriteFile(payloadPath, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	stub := filepath.Join(dir, "stub-renderer.sh")
	script := "#!/bin/sh\ncat " + payloadPath + "\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	runner := Molstar{RendererCommand: []string{stub}}
	result, decoded, err := runner.InspectMVS(context.Background(), filepath.Join(dir, "scene.mvsj"))
	if err != nil {
		t.Fatalf("InspectMVS on a %d byte payload failed: %v", len(payload), err)
	}
	if decoded["ok"] != true {
		t.Fatalf("decoded payload = %#v, want ok true", decoded)
	}
	if got := len(decoded["refs"].([]any)); got != 400 {
		t.Fatalf("decoded %d refs, want 400", got)
	}

	// The reported copy stays truncated so run logs and reports do not grow
	// without bound.
	if len(result.Stdout) > (16<<10)+len("\n[truncated]") {
		t.Fatalf("reported stdout is %d bytes, expected it to stay truncated", len(result.Stdout))
	}
	if !strings.HasSuffix(result.Stdout, "[truncated]") {
		t.Fatal("reported stdout should carry the truncation marker")
	}
}
