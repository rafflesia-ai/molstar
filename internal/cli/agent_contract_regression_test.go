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

// Regressions for agent-facing contract bugs found while dogfooding the CLI.
// Each test pins a behaviour an automated caller branches on.

// TestErrorCodeMatrix pins the code -> agent_code / exit / retryable mapping
// that the README documents. The README table previously listed `code` values
// under the `agent_code` heading, so agents branched on codes the CLI never
// emitted. Update the README table and docs/json-contracts.md whenever this
// matrix changes.
func TestErrorCodeMatrix(t *testing.T) {
	cases := []struct {
		code      errorKind
		agentCode string
		exit      int
		retryable bool
	}{
		{kindInvalidInput, "invalid_job", 2, false},
		{kindValidation, "invalid_job", 3, false},
		{kindInvalidScene, "invalid_job", 3, false},
		{kindRuntime, "security_policy", 4, false},
		{kindSecurity, "security_policy", 8, false},
		{kindNetwork, "network_blocked", 7, true},
		{kindRenderer, "renderer_unavailable", 5, true},
		{kindRendererABI, "renderer_unavailable", 5, true},
		{kindRender, "renderer_unavailable", 5, true},
		{kindServerBusy, "server_busy", 9, true},
		{kindCanceled, "canceled", 130, false},
		{kindInternal, "internal_error", 1, false},
	}
	for _, tc := range cases {
		err := markError(tc.code, fmt.Errorf("representative failure"))
		body := newErrorBody(err)
		if body.Code != string(tc.code) {
			t.Fatalf("code = %q, want %q", body.Code, tc.code)
		}
		if body.AgentCode != tc.agentCode {
			t.Fatalf("%s: agent_code = %q, want %q", tc.code, body.AgentCode, tc.agentCode)
		}
		if body.ExitCode != tc.exit {
			t.Fatalf("%s: exit_code = %d, want %d", tc.code, body.ExitCode, tc.exit)
		}
		if body.Retryable != tc.retryable {
			t.Fatalf("%s: retryable = %v, want %v", tc.code, body.Retryable, tc.retryable)
		}
	}
}

// A bad command line is caller input, not an internal fault. Reporting it as
// internal_error left agents with no branchable signal that they had simply
// typed the wrong flag or command.
func TestUsageErrorsClassifyAsInvalidInput(t *testing.T) {
	for _, message := range []string{
		`unknown command "nosuchcmd" for "molstar"`,
		"unknown flag: --bogus",
		"unknown shorthand flag: 'z' in -z",
		"flag needs an argument: --size",
		`invalid argument "1ms" for "--timeout" flag: strconv.ParseInt: parsing "1ms": invalid syntax`,
	} {
		err := markUsageError(fmt.Errorf("%s", message))
		if got := classifyError(err); got != kindInvalidInput {
			t.Fatalf("classifyError(%q) = %q, want %q", message, got, kindInvalidInput)
		}
		if got := agentErrorCode(err); got != "invalid_job" {
			t.Fatalf("agentErrorCode(%q) = %q, want invalid_job", message, got)
		}
		if got := ExitCode(err); got != 2 {
			t.Fatalf("ExitCode(%q) = %d, want 2", message, got)
		}
	}

	// Errors that merely mention a flag must not be swallowed as usage errors.
	rendererErr := fmt.Errorf("renderer stderr: unknown flag reported by mol*")
	if _, ok := markUsageError(rendererErr).(kindError); ok {
		t.Fatal("markUsageError should only match messages that start with a usage prefix")
	}
}

func TestUnknownFlagReportsInvalidInputJSON(t *testing.T) {
	stdout, _, err := runAppForTest(context.Background(), "render", "--bogus-flag", "--json")
	if err == nil {
		t.Fatal("expected unknown flag to fail")
	}
	var failure errorReport
	if jsonErr := json.Unmarshal([]byte(stdout), &failure); jsonErr != nil {
		// The root command writes the envelope in Execute; when driven through
		// ExecuteContext the marked error is enough to assert on.
		if got := agentErrorCode(err); got != "invalid_job" {
			t.Fatalf("agentErrorCode for unknown flag = %q, want invalid_job", got)
		}
		return
	}
	if failure.Error.Code != string(kindInvalidInput) {
		t.Fatalf("unknown flag error code = %q, want %q", failure.Error.Code, kindInvalidInput)
	}
}

// A blank image means the renderer worked and the scene had nothing in it.
// Classifying it as a renderer failure told agents to run doctor and marked the
// failure retryable, so they re-ran an identical job that could never succeed.
func TestBlankOutputClassifiesAsSceneProblem(t *testing.T) {
	err := markError(kindInvalidScene, fmt.Errorf("output out.png appears blank: the scene rendered no visible geometry"))
	if got := classifyError(err); got != kindInvalidScene {
		t.Fatalf("classifyError = %q, want %q", got, kindInvalidScene)
	}
	if got := agentErrorCode(err); got != "invalid_job" {
		t.Fatalf("agentErrorCode = %q, want invalid_job", got)
	}
	if errorRetryable(err) {
		t.Fatal("a blank scene must not be reported as retryable")
	}
	body := newErrorBody(err)
	if len(body.Diagnosis) == 0 || !strings.Contains(body.Diagnosis[0], "selector") {
		t.Fatalf("blank-scene diagnosis should point at selectors, got %#v", body.Diagnosis)
	}
	if strings.Contains(strings.Join(body.Diagnosis, " "), "doctor") {
		t.Fatalf("blank-scene diagnosis must not suggest doctor, got %#v", body.Diagnosis)
	}
	if got := likelyCauseFromBody(&body); got != "invalid job or scene" {
		t.Fatalf("likelyCause = %q, want %q", got, "invalid job or scene")
	}
}

// Run logs persist stage errors as plain strings. diagnose must re-derive the
// classification from the message, not from the stage name alone.
func TestErrorFromRenderReportPrefersMessageOverStageName(t *testing.T) {
	report := renderReport{
		Stages: []stageReport{{
			Name:  "render_output",
			OK:    false,
			Error: "output out.png appears blank: the scene rendered no visible geometry",
		}},
	}
	err := errorFromRenderReport(report)
	if err == nil {
		t.Fatal("expected an error from a failed stage")
	}
	if got := classifyError(err); got != kindInvalidScene {
		t.Fatalf("classifyError = %q, want %q; render_output stage name must not override the message", got, kindInvalidScene)
	}

	// A genuine renderer failure in the same stage still classifies as a render failure.
	rendererReport := renderReport{
		Stages: []stageReport{{Name: "render_output", OK: false, Error: "renderer exited with status 1"}},
	}
	if got := classifyError(errorFromRenderReport(rendererReport)); got != kindRender {
		t.Fatalf("renderer stage failure = %q, want %q", got, kindRender)
	}
}

// Diagnosis hints previously matched bare "molstar"/"render" against the whole
// message, so any error carrying a path under a molstar checkout was told to go
// run doctor.
func TestInvalidInputDiagnosisDoesNotBlameTheRenderer(t *testing.T) {
	err := markError(kindInvalidInput, fmt.Errorf("no such file: /Users/dev/Local/molstar/missing.job.yaml"))
	hints := diagnoseError(err)
	if len(hints) == 0 {
		t.Fatal("expected a hint for a missing job file")
	}
	joined := strings.Join(hints, " ")
	if strings.Contains(joined, "doctor") {
		t.Fatalf("a missing job file must not suggest doctor, got %#v", hints)
	}
}

// The documented agent loop reads RUN_ID out of --report to drive
// `logs export` and `diagnose`, so the report file must carry it.
func TestRenderReportFileCarriesRunID(t *testing.T) {
	dir := t.TempDir()
	jobPath := writeContractJob(t, dir)
	reportPath := filepath.Join(dir, "render-report.json")
	t.Setenv("MOLSTAR_RUNS_DIR", filepath.Join(dir, "runs"))

	stdout, _, err := runAppForTest(context.Background(),
		"render", jobPath, "--json", "--report", reportPath)
	if err != nil {
		t.Fatalf("render failed: %v\n%s", err, stdout)
	}

	data, readErr := os.ReadFile(reportPath)
	if readErr != nil {
		t.Fatalf("read report: %v", readErr)
	}
	var fileReport renderReport
	if err := json.Unmarshal(data, &fileReport); err != nil {
		t.Fatalf("report file is not JSON: %v", err)
	}
	if strings.TrimSpace(fileReport.RunID) == "" {
		t.Fatal("--report file is missing run_id; agents cannot reach logs export or diagnose from it")
	}
	if strings.TrimSpace(fileReport.RunLog) == "" {
		t.Fatal("--report file is missing run_log")
	}

	var stdoutReport renderReport
	if err := json.Unmarshal([]byte(stdout), &stdoutReport); err != nil {
		t.Fatalf("stdout is not JSON: %v", err)
	}
	if fileReport.RunID != stdoutReport.RunID {
		t.Fatalf("report file run_id %q != stdout run_id %q", fileReport.RunID, stdoutReport.RunID)
	}
}

// readRunBundle stopped enumerating entries once it had decoded run.json, so
// verify reported bundles this tool had just written as missing their sidecars.
func TestReadRunBundleEnumeratesEveryEntry(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "run.molrun")
	envelope := runLogEnvelope{
		ID:     "20260101T000000.000000000Z",
		OK:     true,
		Report: renderReport{OK: true, Job: &job.Job{Version: 1}},
	}
	if err := exportRunBundle(bundle, envelope); err != nil {
		t.Fatalf("export bundle: %v", err)
	}

	decoded, files, err := readRunBundle(bundle)
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	if decoded.ID != envelope.ID {
		t.Fatalf("decoded id = %q, want %q", decoded.ID, envelope.ID)
	}
	if !containsString(files, "run.json") || !containsString(files, "job.json") {
		t.Fatalf("bundle file list = %#v, want run.json and job.json", files)
	}
	for _, warning := range verifyRunBundleReport(bundle, decoded, files).Warnings {
		if strings.Contains(warning, "sidecar is missing") {
			t.Fatalf("verify reported a missing sidecar for a bundle that contains it: %q", warning)
		}
	}
}

// inspect used to lowercase author-supplied refs and rewrite "-" to "_", so the
// refs it reported did not exist in the compiled scene.
func TestInspectPreservesAuthorSuppliedRefs(t *testing.T) {
	j := job.Job{
		Version: 1,
		Scene: job.Scene{Structures: []job.Structure{{
			Ref:    "Model-1",
			Source: "input",
			Components: []job.Component{
				{Ref: "Chain-A", Select: "chain:A"},
				{Ref: "myLigand", Select: "ligand"},
			},
		}}},
	}
	prepared := jobWithInspectRefs(j, "")
	structure := prepared.Scene.Structures[0]
	if structure.Ref != "Model-1" {
		t.Fatalf("structure ref = %q, want Model-1", structure.Ref)
	}
	got := []string{structure.Components[0].Ref, structure.Components[1].Ref}
	want := []string{"Chain-A", "myLigand"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("component refs = %#v, want %#v", got, want)
		}
	}

	// Missing refs are still generated, and generated refs stay unique.
	generated := jobWithInspectRefs(job.Job{
		Version: 1,
		Scene: job.Scene{Structures: []job.Structure{{
			Source:     "input",
			Components: []job.Component{{Select: "ligand"}, {Select: "ligand"}},
		}}},
	}, "")
	first := generated.Scene.Structures[0].Components[0].Ref
	second := generated.Scene.Structures[0].Components[1].Ref
	if first == "" || second == "" || first == second {
		t.Fatalf("generated refs = %q, %q; want two distinct non-empty refs", first, second)
	}
}

// Static selection stats used to report supported=true with zero atoms whenever
// the input was never parsed (any remote bcif, which is the default), which
// reads as "your selector matched nothing".
func TestInspectStatsDistinguishUnparsedInputFromEmptySelection(t *testing.T) {
	t.Run("binary cif is not silently reported as empty", func(t *testing.T) {
		analysis := analyzeInputAtoms(job.Input{ID: "1cbs", Provider: "pdbe"})
		if analysis.reason == "" {
			t.Fatal("a remote input must report why it was not analyzed")
		}
		stats := inspectComponent(
			job.Structure{Ref: "s", Source: "input"},
			job.Component{Ref: "lig", Select: "ligand"},
			analysis,
		)["stats"].(selectionStats)
		if stats.Supported {
			t.Fatal("stats must not claim support for an input that was never parsed")
		}
		if stats.Reason == "" {
			t.Fatal("unsupported stats must explain why")
		}
		if stats.Atoms != 0 {
			t.Fatalf("unparsed input should not report atoms, got %d", stats.Atoms)
		}
	})

	t.Run("unsupported selector is distinguished from an empty match", func(t *testing.T) {
		dir := t.TempDir()
		pdbPath := filepath.Join(dir, "one.pdb")
		content := "ATOM      1  CA  ALA A   1      11.000  12.000  13.000  1.00  0.00           C\n" +
			"HETATM    2  C1  LIG B   1      21.000  22.000  23.000  1.00  0.00           C\n"
		if err := os.WriteFile(pdbPath, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		analysis := analyzeInputAtoms(job.Input{Path: pdbPath, Format: "pdb"})
		if analysis.reason != "" {
			t.Fatalf("local PDB should be analyzed, got reason %q", analysis.reason)
		}

		structure := job.Structure{Ref: "s", Source: "input"}
		// "polymer" is not understood by the static analyzer.
		unsupported := inspectComponent(structure, job.Component{Ref: "p", Select: "polymer"}, analysis)["stats"].(selectionStats)
		if unsupported.Supported || unsupported.Reason == "" {
			t.Fatalf("unsupported selector stats = %#v, want supported=false with a reason", unsupported)
		}

		// "water" is understood and genuinely matches nothing here.
		empty := inspectComponent(structure, job.Component{Ref: "w", Select: "water"}, analysis)["stats"].(selectionStats)
		if !empty.Supported {
			t.Fatalf("water stats = %#v, want supported=true", empty)
		}
		if empty.Atoms != 0 {
			t.Fatalf("water should match no atoms, got %d", empty.Atoms)
		}

		matched := inspectComponent(structure, job.Component{Ref: "l", Select: "ligand"}, analysis)["stats"].(selectionStats)
		if !matched.Supported || matched.Atoms != 1 {
			t.Fatalf("ligand stats = %#v, want supported=true with 1 atom", matched)
		}
	})
}
