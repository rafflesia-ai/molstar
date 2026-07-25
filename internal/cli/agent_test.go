package cli

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAgentDoctorCommand(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	stdout, stderr, err := runAppForTest(ctx, "agent", "doctor", "--json", "--out-dir", dir)
	if err != nil {
		t.Fatalf("agent doctor failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("agent doctor should keep stderr quiet on success, got %q", stderr)
	}
	var report agentDoctorReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("agent doctor stdout is not JSON: %v\n%s", err, stdout)
	}
	if !report.OK || report.Contract != "headlessmolstar.agent-doctor/v1" || len(report.Steps) < 7 {
		t.Fatalf("unexpected agent doctor report: %#v", report)
	}
	required := map[string]bool{
		"doctor JSON envelope":    false,
		"capabilities contract":   false,
		"job schema validation":   false,
		"job explain contract":    false,
		"compile-only inspect":    false,
		"render dry-run contract": false,
		"server OpenAPI contract": false,
	}
	for _, step := range report.Steps {
		if _, ok := required[step.Name]; ok {
			required[step.Name] = step.OK
		}
	}
	for name, ok := range required {
		if !ok {
			t.Fatalf("agent doctor missing/passing step %q in %#v", name, report.Steps)
		}
	}
	if report.WorkDir != dir {
		t.Fatalf("agent doctor work dir = %q, want %q", report.WorkDir, dir)
	}
	if err := requireNonEmptyFile(filepath.Join(dir, "agent-doctor.job.json")); err != nil {
		t.Fatal(err)
	}
}
