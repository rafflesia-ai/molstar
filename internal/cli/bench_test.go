package cli

import (
	"context"
	"encoding/json"
	"testing"
)

func TestBenchDemoDryRunJSON(t *testing.T) {
	stdout, stderr, err := runAppForTest(context.Background(),
		"bench",
		"--demo",
		"--dry-run",
		"--iterations", "2",
		"--warmup", "1",
		"--size", "64x64",
		"--json",
	)
	if err != nil {
		t.Fatalf("bench failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var report benchReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("bench stdout is not JSON: %v\n%s", err, stdout)
	}
	if !report.OK || report.Input != "demo" || report.Iterations != 2 || report.Warmup != 1 || report.Summary.Succeeded != 2 {
		t.Fatalf("unexpected bench report: %#v", report)
	}
	if len(report.Runs) != 3 || !report.Runs[0].Warmup || report.Runs[1].Warmup {
		t.Fatalf("unexpected bench runs: %#v", report.Runs)
	}
}

func TestParseRegressionPercent(t *testing.T) {
	cases := map[string]float64{
		"20":    20,
		"20%":   20,
		" 2.5%": 2.5,
	}
	for input, expected := range cases {
		actual, err := parseRegressionPercent(input)
		if err != nil {
			t.Fatalf("parseRegressionPercent(%q) failed: %v", input, err)
		}
		if actual != expected {
			t.Fatalf("parseRegressionPercent(%q) = %v, want %v", input, actual, expected)
		}
	}
	if _, err := parseRegressionPercent("-1%"); err == nil {
		t.Fatal("expected negative regression limit to fail")
	}
}
