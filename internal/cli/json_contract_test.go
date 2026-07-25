package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/sacha-ichbiah/molstar/internal/job"
)

type jsonContractSnapshot struct {
	Contract string         `json:"contract"`
	Stable   map[string]any `json:"stable"`
	Shape    any            `json:"shape"`
}

func TestAgentJSONContractSnapshots(t *testing.T) {
	dir := t.TempDir()
	jobPath := writeContractJob(t, dir)

	errorStdout, errorStderr, err := runAppForTest(context.Background(), "job", "validate", filepath.Join(dir, "missing.json"), "--json")
	if err == nil {
		t.Fatal("expected invalid job command to fail")
	}
	if strings.TrimSpace(errorStderr) != "" {
		t.Fatalf("JSON error contract should keep stderr empty, got %q", errorStderr)
	}
	assertJSONContractSnapshot(t, "molstar-error-invalid-job", errorStdout, map[string][]any{
		"ok":            {"ok"},
		"command":       {"command"},
		"code":          {"error", "code"},
		"agent_code":    {"error", "agent_code"},
		"retryable":     {"error", "retryable"},
		"exit_code":     {"error", "exit_code"},
		"diagnosis_len": {"error", "diagnosis", "len"},
		"has_timestamp": {"timestamp", "present"},
	})

	cases := []struct {
		name   string
		args   []string
		stable map[string][]any
	}{
		{
			name: "molstar-job-validate",
			args: []string{"job", "validate", jobPath, "--schema", "--json"},
			stable: map[string][]any{
				"ok":     {"ok"},
				"schema": {"schema"},
			},
		},
		{
			name: "molstar-job-explain",
			args: []string{"job", "explain", jobPath, "--json"},
			stable: map[string][]any{
				"ok":            {"ok"},
				"schema":        {"schema"},
				"would_compile": {"would_compile"},
				"would_render":  {"would_render"},
				"outputs_len":   {"outputs", "len"},
			},
		},
		{
			name: "molstar-render-dry-run",
			args: []string{"render", jobPath, "--dry-run", "--json"},
			stable: map[string][]any{
				"ok":               {"ok"},
				"commands_len":     {"commands", "len"},
				"first_skipped":    {"commands", 0, "skipped"},
				"outputs_len":      {"outputs", "len"},
				"output_files_len": {"output_files", "len"},
			},
		},
		{
			name: "molstar-render-dry-run-compact",
			args: []string{"render", jobPath, "--dry-run", "--json", "--compact"},
			stable: map[string][]any{
				"ok":               {"ok"},
				"commands_len":     {"commands", "len"},
				"first_skipped":    {"commands", 0, "skipped"},
				"outputs_len":      {"outputs", "len"},
				"output_files_len": {"output_files", "len"},
			},
		},
		{
			name: "molstar-examples",
			args: []string{"examples", "--json"},
			stable: map[string][]any{
				"ok":           {"ok"},
				"examples_len": {"examples", "len"},
				"first_name":   {"examples", 0, "name"},
				"first_output": {"examples", 0, "outputs", 0},
			},
		},
		{
			name: "molstar-job-examples-list",
			args: []string{"job", "examples", "list", "--json"},
			stable: map[string][]any{
				"ok":           {"ok"},
				"examples_len": {"examples", "len"},
				"first_name":   {"examples", 0},
			},
		},
		{
			name: "molstar-job-examples-show",
			args: []string{"job", "examples", "show", "ligand", "--json"},
			stable: map[string][]any{
				"ok":      {"ok"},
				"name":    {"name"},
				"version": {"job", "version"},
			},
		},
		{
			name: "molstar-capabilities",
			args: []string{"capabilities", "--json", "--timeout", "15s"},
			stable: map[string][]any{
				"ok":                            {"ok"},
				"primary_kind":                  {"renderer", "primary", "kind"},
				"primary_supports_capabilities": {"renderer", "primary", "supports_capabilities"},
				"worker_supported":              {"worker", "supported"},
				"matrix_len":                    {"matrix", "len"},
				"matrix_primary":                {"matrix", 0, "target"},
			},
		},
		{
			name: "molstar-openapi",
			args: []string{"serve", "--openapi"},
			stable: map[string][]any{
				"openapi":                   {"openapi"},
				"has_render_path":           {"paths", "/render", "present"},
				"has_rpc_path":              {"paths", "/rpc", "present"},
				"render_code_samples_len":   {"paths", "/render", "post", "x-codeSamples", "len"},
				"has_python_render_example": {"components", "examples", "PythonRender", "present"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			stdout, stderr, err := runAppForTest(ctx, tc.args...)
			if err != nil {
				t.Fatalf("%v failed: %v\nstdout:\n%s\nstderr:\n%s", tc.args, err, stdout, stderr)
			}
			assertJSONContractSnapshot(t, tc.name, stdout, tc.stable)
		})
	}

	assertServeMetricsContractSnapshot(t)
	assertRunLogContractSnapshots(t)
	assertServeSmokeContractSnapshot(t)
}

func TestAgentJSONContractCompact(t *testing.T) {
	dir := t.TempDir()
	jobPath := writeContractJob(t, dir)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	errorStdout, errorStderr, err := runAppForTest(ctx, "job", "validate", filepath.Join(dir, "missing.json"), "--json")
	if err == nil {
		t.Fatal("expected invalid job command to fail")
	}
	if strings.TrimSpace(errorStderr) != "" {
		t.Fatalf("JSON error contract should keep stderr empty, got %q", errorStderr)
	}
	errorReport := decodeContractJSON(t, errorStdout)
	for _, key := range []string{"code", "agent_code", "message", "retryable", "exit_code", "diagnosis"} {
		if contractPathValue(errorReport, "error", key) == nil {
			t.Fatalf("error envelope missing %s: %#v", key, errorReport)
		}
	}
	if got := contractPathValue(errorReport, "error", "agent_code"); got != "invalid_job" {
		t.Fatalf("agent_code = %#v, want invalid_job", got)
	}

	validateStdout, validateStderr, err := runAppForTest(ctx, "job", "validate", jobPath, "--schema", "--json")
	if err != nil {
		t.Fatalf("job validate failed: %v\nstdout:\n%s\nstderr:\n%s", err, validateStdout, validateStderr)
	}
	validateReport := decodeContractJSON(t, validateStdout)
	if validateReport["ok"] != true || validateReport["schema"] != true {
		t.Fatalf("unexpected validate contract: %#v", validateReport)
	}

	dryRunStdout, dryRunStderr, err := runAppForTest(ctx, "render", jobPath, "--dry-run", "--json")
	if err != nil {
		t.Fatalf("render dry-run failed: %v\nstdout:\n%s\nstderr:\n%s", err, dryRunStdout, dryRunStderr)
	}
	if strings.TrimSpace(dryRunStderr) != "" {
		t.Fatalf("render dry-run JSON contract should keep stderr empty, got %q", dryRunStderr)
	}
	dryRunReport := decodeContractJSON(t, dryRunStdout)
	if dryRunReport["ok"] != true ||
		contractPathValue(dryRunReport, "commands", "len") != 1 ||
		contractPathValue(dryRunReport, "commands", 0, "skipped") != true ||
		contractPathValue(dryRunReport, "diagnostics", "renderer_mode") == nil {
		t.Fatalf("unexpected dry-run contract: %#v", dryRunReport)
	}

	compactStdout, compactStderr, err := runAppForTest(ctx, "render", jobPath, "--dry-run", "--json", "--compact")
	if err != nil {
		t.Fatalf("render dry-run compact failed: %v\nstdout:\n%s\nstderr:\n%s", err, compactStdout, compactStderr)
	}
	if strings.TrimSpace(compactStderr) != "" {
		t.Fatalf("render dry-run compact JSON contract should keep stderr empty, got %q", compactStderr)
	}
	compactReport := decodeContractJSON(t, compactStdout)
	if _, ok := compactReport["job"]; ok {
		t.Fatalf("compact render report should omit job: %#v", compactReport)
	}
	if _, ok := compactReport["mvs_document"]; ok {
		t.Fatalf("compact render report should omit mvs_document: %#v", compactReport)
	}
	command, ok := contractPathValue(compactReport, "commands", 0).(map[string]any)
	if !ok {
		t.Fatalf("compact render report missing first command: %#v", compactReport)
	}
	if _, ok := command["stdout"]; ok {
		t.Fatalf("compact render report should omit command stdout: %#v", command)
	}
	if _, ok := command["stderr"]; ok {
		t.Fatalf("compact render report should omit command stderr: %#v", command)
	}

	openAPIStdout, openAPIStderr, err := runAppForTest(ctx, "serve", "--openapi")
	if err != nil {
		t.Fatalf("serve --openapi failed: %v\nstdout:\n%s\nstderr:\n%s", err, openAPIStdout, openAPIStderr)
	}
	openAPI := decodeContractJSON(t, openAPIStdout)
	if openAPI["openapi"] != "3.1.0" ||
		contractPathValue(openAPI, "paths", "/render", "post", "present") != true ||
		contractPathValue(openAPI, "paths", "/metrics/prometheus", "get", "present") != true ||
		contractPathValue(openAPI, "paths", "/render", "post", "x-codeSamples", "len") != 3 {
		t.Fatalf("unexpected openapi contract: %#v", openAPI)
	}
}

func writeContractJob(t *testing.T, dir string) string {
	t.Helper()
	modelPath := filepath.Join(dir, "one.cif")
	if err := os.WriteFile(modelPath, []byte(oneAtomCIF), 0o644); err != nil {
		t.Fatal(err)
	}
	j := minimalServeJob(t, dir)
	j.Inputs["input"] = job.Input{Path: modelPath, Format: "mmcif"}
	j.Scene.Canvas = job.Canvas{Background: "white"}
	j.Scene.Structures[0].Components[0].Ref = "all"
	j.Scene.Structures[0].Components[0].Representation = job.Representation{Type: "spacefill", Color: "#cc3399"}
	j.Scene.Camera = job.Camera{Focus: "all"}
	j.Outputs = []job.Output{{Type: "image", Path: filepath.Join(dir, "contract.png"), Size: []int{96, 72}}}
	jobPath := filepath.Join(dir, "contract.job.json")
	data, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jobPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return jobPath
}

func assertServeMetricsContractSnapshot(t *testing.T) {
	t.Helper()
	a := app{stdout: io.Discard, stderr: io.Discard}
	cmd := a.serveCommand()
	handler := a.serveHandler(&serveFlags{dryRun: true}, cmd)

	dir := t.TempDir()
	data, err := json.Marshal(minimalServeJob(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	renderResponse := httptest.NewRecorder()
	handler.ServeHTTP(renderResponse, httptest.NewRequest(http.MethodPost, "/render", bytes.NewReader(data)))
	if renderResponse.Code != http.StatusOK {
		t.Fatalf("expected render 200, got %d: %s", renderResponse.Code, renderResponse.Body.String())
	}
	metricsResponse := httptest.NewRecorder()
	handler.ServeHTTP(metricsResponse, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if metricsResponse.Code != http.StatusOK {
		t.Fatalf("expected metrics 200, got %d: %s", metricsResponse.Code, metricsResponse.Body.String())
	}
	assertJSONContractSnapshot(t, "molstar-serve-metrics", metricsResponse.Body.String(), map[string][]any{
		"ok":                 {"ok"},
		"submitted":          {"submitted"},
		"succeeded":          {"succeeded"},
		"failed":             {"failed"},
		"queue_wait_count":   {"queue_wait_ms", "count"},
		"render_count":       {"render_duration_ms", "count"},
		"queue_wait_buckets": {"queue_wait_ms", "buckets", "len"},
	})
}

func assertRunLogContractSnapshots(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	runDir := filepath.Join(dir, "runs")
	t.Setenv("MOLSTAR_RUNS_DIR", runDir)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	renderOut := filepath.Join(dir, "contract-demo.png")
	stdout, stderr, err := runAppForTest(ctx, "render", "--demo", "--out", renderOut, "--size", "64x48", "--run-label", "contract", "--json")
	if err != nil {
		t.Fatalf("contract render failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	renderReport := decodeContractJSON(t, stdout)
	runID, _ := renderReport["run_id"].(string)
	if runID == "" {
		t.Fatalf("contract render did not report run_id: %#v", renderReport)
	}

	bundle := filepath.Join(dir, "contract.molrun")
	stdout, stderr, err = runAppForTest(ctx, "logs", "export", runID, "--dir", runDir, "--out", bundle, "--json")
	if err != nil {
		t.Fatalf("contract logs export failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	stdout, stderr, err = runAppForTest(ctx, "logs", "verify", bundle, "--json")
	if err != nil {
		t.Fatalf("contract logs verify failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	assertJSONContractSnapshot(t, "molstar-logs-verify", stdout, map[string][]any{
		"ok":               {"ok"},
		"replayable":       {"replayable"},
		"fully_replayable": {"fully_replayable"},
		"files_len":        {"files", "len"},
		"outputs_len":      {"expected_outputs", "len"},
	})

	rerunDir := filepath.Join(dir, "rerun")
	stdout, stderr, err = runAppForTest(ctx, "logs", "rerun", bundle, "--out-dir", rerunDir, "--json")
	if err != nil {
		t.Fatalf("contract logs rerun failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	assertJSONContractSnapshot(t, "molstar-logs-rerun", stdout, map[string][]any{
		"ok":               {"ok"},
		"rerun_ok":         {"rerun", "ok"},
		"files_len":        {"files", "len"},
		"output_files_len": {"rerun", "output_files", "len"},
	})

	issue := filepath.Join(dir, "contract-issue.zip")
	stdout, stderr, err = runAppForTest(ctx, "diagnose", runID, "--dir", runDir, "--bundle", "--out", issue, "--redact-paths", "--redact-inputs", "--json")
	if err != nil {
		t.Fatalf("contract diagnose --bundle failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	assertJSONContractSnapshot(t, "molstar-diagnose-bundle", stdout, map[string][]any{
		"ok":               {"ok"},
		"replayable":       {"replayable"},
		"fully_replayable": {"fully_replayable"},
		"files_len":        {"files", "len"},
		"redact_paths":     {"redactions", "paths"},
		"redact_inputs":    {"redactions", "inputs"},
	})
}

func assertServeSmokeContractSnapshot(t *testing.T) {
	t.Helper()
	socketPath := shortUnixSocketPath(t, "molstar.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	a := app{stdout: &stdout, stderr: &stderr}
	cmd := a.serveCommand()
	done := make(chan error, 1)
	go func() {
		done <- a.runServe(ctx, &serveFlags{socket: socketPath, noWorker: true, rendererCommand: "false"}, cmd)
	}()
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(socketPath); err == nil {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("serve exited early: %v\nstdout:%s\nstderr:%s", err, stdout.String(), stderr.String())
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}

	smokeStdout, smokeStderr, err := runAppForTest(context.Background(), "serve", "smoke", "--socket", socketPath, "--json")
	if err != nil {
		t.Fatalf("contract serve smoke failed: %v\nstdout:\n%s\nstderr:\n%s", err, smokeStdout, smokeStderr)
	}
	assertJSONContractSnapshot(t, "molstar-serve-smoke", smokeStdout, map[string][]any{
		"ok":           {"ok"},
		"checks_len":   {"checks", "len"},
		"health_ok":    {"checks", 0, "ok"},
		"ready_ok":     {"checks", 1, "ok"},
		"metrics_ok":   {"checks", 4, "ok"},
		"rpc_check_ok": {"checks", 5, "ok"},
	})
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not shut down")
	}
}

func assertJSONContractSnapshot(t *testing.T, name string, raw string, stablePaths map[string][]any) {
	t.Helper()
	var value any
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("%s stdout is not JSON: %v\n%s", name, err, raw)
	}
	stable := make(map[string]any, len(stablePaths))
	keys := make([]string, 0, len(stablePaths))
	for key := range stablePaths {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		stable[key] = contractPathValue(value, stablePaths[key]...)
	}
	snapshot := jsonContractSnapshot{
		Contract: name,
		Stable:   stable,
		Shape:    contractShape(value),
	}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(snapshot); err != nil {
		t.Fatal(err)
	}
	data := buffer.Bytes()

	path := filepath.Join("testdata", "contracts", name+".json")
	if os.Getenv("UPDATE_CONTRACT_SNAPSHOTS") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	golden, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read contract snapshot %s: %v; set UPDATE_CONTRACT_SNAPSHOTS=1 to write it", path, err)
	}
	if !bytes.Equal(golden, data) {
		t.Fatalf("%s JSON contract changed; set UPDATE_CONTRACT_SNAPSHOTS=1 if this is intentional\n%s", name, contractSnapshotDiff(golden, data))
	}
}

func decodeContractJSON(t *testing.T, raw string) map[string]any {
	t.Helper()
	var value map[string]any
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, raw)
	}
	return value
}

func contractShape(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			if contractVolatileShapeKey(key) {
				continue
			}
			if key == "command" {
				if _, ok := child.([]any); ok {
					out[key] = []any{"<command>"}
					continue
				}
			}
			out[key] = contractShape(child)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, child := range typed {
			out[i] = contractShape(child)
		}
		return out
	case json.Number:
		return "<number>"
	case string:
		return "<string>"
	case bool:
		return "<bool>"
	case nil:
		return nil
	default:
		return fmt.Sprintf("<%T>", typed)
	}
}

func contractVolatileShapeKey(key string) bool {
	switch key {
	case "duration_ms", "software_gl":
		return true
	default:
		return false
	}
}

func contractPathValue(value any, path ...any) any {
	current := value
	for i, segment := range path {
		if text, ok := segment.(string); ok {
			if text == "len" {
				switch typed := current.(type) {
				case []any:
					return len(typed)
				case map[string]any:
					return len(typed)
				default:
					return nil
				}
			}
			if text == "present" {
				return current != nil
			}
		}
		switch typed := current.(type) {
		case map[string]any:
			key, ok := segment.(string)
			if !ok {
				return nil
			}
			child, ok := typed[key]
			if !ok {
				return nil
			}
			current = child
		case []any:
			index, ok := segment.(int)
			if !ok || index < 0 || index >= len(typed) {
				return nil
			}
			current = typed[index]
		default:
			return nil
		}
		if i == len(path)-1 {
			return current
		}
	}
	return current
}

func contractSnapshotDiff(golden []byte, data []byte) string {
	goldenLines := strings.Split(string(golden), "\n")
	dataLines := strings.Split(string(data), "\n")
	limit := len(goldenLines)
	if len(dataLines) > limit {
		limit = len(dataLines)
	}
	for i := 0; i < limit; i++ {
		var oldLine, newLine string
		if i < len(goldenLines) {
			oldLine = goldenLines[i]
		}
		if i < len(dataLines) {
			newLine = dataLines[i]
		}
		if oldLine != newLine {
			return fmt.Sprintf("first difference at line %d\n- %s\n+ %s", i+1, oldLine, newLine)
		}
	}
	return "snapshot bytes differ"
}
