package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rafflesia-ai/molstar/internal/job"
)

func TestAgentErrorCodes(t *testing.T) {
	cases := []struct {
		err  error
		want errorKind
	}{
		{markError(kindValidation, fmt.Errorf("schema validation failed")), "invalid_job"},
		{markError(kindNetwork, fmt.Errorf("offline: no cached input")), "network_blocked"},
		{markError(kindSecurity, fmt.Errorf("outside allowed path")), kindSecurity},
		{markError(kindServerBusy, fmt.Errorf("render worker queue is full")), kindServerBusy},
		{fmt.Errorf("headless WebGL context is unavailable: gl returned null"), "webgl_unavailable"},
	}
	for _, tc := range cases {
		if got := agentErrorCode(tc.err); got != tc.want {
			t.Fatalf("agentErrorCode(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
	body := newErrorBody(fmt.Errorf("headless WebGL context is unavailable: gl returned null"))
	if body.AgentCode != "webgl_unavailable" || !body.Retryable || body.ExitCode == 0 {
		t.Fatalf("unexpected error body: %#v", body)
	}
}

func TestCLIJSONErrorAndDryRunUX(t *testing.T) {
	root := app{}.rootCommand()
	if got := commandNameForArgs(root, []string{"quickstart", "--bogus", "--json"}); got != "quickstart" {
		t.Fatalf("commandNameForArgs for quickstart unknown flag = %q", got)
	}
	if got := commandNameForArgs(root, []string{"job", "examples", "list", "--bogus", "--json"}); got != "job examples list" {
		t.Fatalf("commandNameForArgs for nested unknown flag = %q", got)
	}

	stdout, stderr, err := runAppForTest(context.Background(), "job", "validate", filepath.Join(t.TempDir(), "missing.json"), "--json")
	if err == nil {
		t.Fatal("expected missing job validation to fail")
	}
	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("expected JSON error command to keep stderr empty, got %q", stderr)
	}
	var failure errorReport
	if err := json.Unmarshal([]byte(stdout), &failure); err != nil {
		t.Fatalf("failure stdout is not JSON: %v\n%s", err, stdout)
	}
	if failure.Error.AgentCode != "invalid_job" || failure.Error.ExitCode == 0 {
		t.Fatalf("unexpected JSON error envelope: %#v", failure)
	}

	dir := t.TempDir()
	cifPath := filepath.Join(dir, "one.cif")
	if err := os.WriteFile(cifPath, []byte(oneAtomCIF), 0o644); err != nil {
		t.Fatal(err)
	}
	j := minimalServeJob(t, dir)
	j.Inputs["input"] = job.Input{Path: cifPath, Format: "mmcif"}
	jobPath := filepath.Join(dir, "job.json")
	data, err := json.Marshal(j)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jobPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err = runAppForTest(context.Background(), "render", jobPath, "--dry-run", "--json")
	if err != nil {
		t.Fatalf("dry-run render failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	requireOKJSON(t, stdout)
	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("expected JSON dry-run to keep stderr empty, got %q", stderr)
	}

	stdout, stderr, err = runAppForTest(context.Background(), "render", jobPath, "--dry-run")
	if err != nil {
		t.Fatalf("human dry-run render failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stderr, "render-mvs.js") {
		t.Fatalf("expected human dry-run command on stderr, got %q", stderr)
	}
}
