package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type selfTestFlags struct {
	jsonReport    bool
	outDir        string
	keep          bool
	verbose       bool
	timeout       time.Duration
	requireWorker bool
}

type selfTestReport struct {
	OK            bool           `json:"ok"`
	WorkDir       string         `json:"work_dir"`
	ArtifactsKept bool           `json:"artifacts_kept"`
	Artifacts     []string       `json:"artifacts,omitempty"`
	Steps         []selfTestStep `json:"steps"`
	StartedAt     string         `json:"started_at"`
	DurationMS    int64          `json:"duration_ms"`
}

type selfTestStep struct {
	Name       string         `json:"name"`
	OK         bool           `json:"ok"`
	Command    []string       `json:"command,omitempty"`
	Detail     string         `json:"detail,omitempty"`
	Error      string         `json:"error,omitempty"`
	Diagnosis  []string       `json:"diagnosis,omitempty"`
	Stdout     string         `json:"stdout,omitempty"`
	Stderr     string         `json:"stderr,omitempty"`
	Report     map[string]any `json:"report,omitempty"`
	parsed     map[string]any `json:"-"`
	stdoutRaw  string         `json:"-"`
	stderrRaw  string         `json:"-"`
	DurationMS int64          `json:"duration_ms"`
}

func (a app) selfTestCommand() *cobra.Command {
	flags := &selfTestFlags{timeout: 120 * time.Second}
	cmd := &cobra.Command{
		Use:     "self-test",
		Aliases: []string{"selftest"},
		Short:   "Run an end-to-end local CLI smoke test",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("self-test", flags.jsonReport, func() error {
				if len(args) != 0 {
					return markError(kindInvalidInput, fmt.Errorf("self-test does not accept positional arguments"))
				}
				report, err := a.runSelfTest(cmd.Context(), flags)
				if flags.jsonReport {
					if writeErr := writeJSON(a.stdout, report); writeErr != nil {
						return markError(kindInternal, writeErr)
					}
					if err != nil {
						return alreadyReported(err)
					}
					return nil
				}
				status := "passed"
				if !report.OK {
					status = "failed"
				}
				fmt.Fprintf(a.stdout, "self-test %s (%d steps, %d ms)\n", status, len(report.Steps), report.DurationMS)
				fmt.Fprintf(a.stdout, "work dir %s\n", report.WorkDir)
				for _, step := range report.Steps {
					stepStatus := "OK"
					if !step.OK {
						stepStatus = "FAIL"
					}
					fmt.Fprintf(a.stdout, "%-5s %s", stepStatus, step.Name)
					if step.Detail != "" {
						fmt.Fprintf(a.stdout, " (%s)", step.Detail)
					}
					if step.Error != "" {
						fmt.Fprintf(a.stdout, " - %s", singleLine(step.Error))
					}
					fmt.Fprintln(a.stdout)
					for _, hint := range step.Diagnosis {
						fmt.Fprintf(a.stdout, "      %s\n", hint)
					}
				}
				return err
			})
		},
	}
	cmd.Flags().BoolVar(&flags.jsonReport, "json", false, "write a machine-readable report to stdout")
	cmd.Flags().StringVar(&flags.outDir, "out-dir", "", "directory for temporary self-test artifacts")
	cmd.Flags().BoolVar(&flags.keep, "keep", false, "keep generated self-test artifacts")
	cmd.Flags().BoolVar(&flags.verbose, "verbose", false, "include child command stdout/stderr and parsed reports")
	cmd.Flags().BoolVar(&flags.requireWorker, "require-worker", false, "fail when the persistent worker renderer cannot render the demo")
	cmd.Flags().DurationVar(&flags.timeout, "timeout", 120*time.Second, "overall self-test timeout")
	return cmd
}

func (a app) runSelfTest(parent context.Context, flags *selfTestFlags) (selfTestReport, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(parent, flags.timeout)
	defer cancel()

	workDir := strings.TrimSpace(flags.outDir)
	artifactsKept := flags.keep || workDir != ""
	var err error
	if workDir == "" {
		workDir, err = os.MkdirTemp("", "headlessmolstar-self-test-*")
		if err != nil {
			return selfTestReport{}, markError(kindInternal, err)
		}
		if !artifactsKept {
			defer os.RemoveAll(workDir)
		}
	} else {
		workDir, err = filepath.Abs(workDir)
		if err != nil {
			return selfTestReport{}, markError(kindInvalidInput, err)
		}
		if err := os.MkdirAll(workDir, 0o755); err != nil {
			return selfTestReport{}, markError(kindInvalidInput, err)
		}
	}

	executable, err := os.Executable()
	if err != nil {
		return selfTestReport{}, markError(kindInternal, err)
	}
	report := selfTestReport{
		OK:            true,
		WorkDir:       workDir,
		ArtifactsKept: artifactsKept,
		StartedAt:     start.UTC().Format(time.RFC3339Nano),
	}
	run := func(name string, args ...string) selfTestStep {
		step := runSelfTestSubcommand(ctx, executable, workDir, name, flags.verbose, args...)
		report.Steps = append(report.Steps, step)
		if !step.OK {
			report.OK = false
		}
		return step
	}

	doctor := run("doctor config", "doctor", "--skip-probe", "--json")
	if doctor.OK && !truthy(doctor.parsed["ok"]) {
		report.Steps[len(report.Steps)-1].OK = false
		report.Steps[len(report.Steps)-1].Error = "doctor report was not ok"
		report.OK = false
	}

	capabilities := run("capabilities report", "capabilities", "--json")
	if capabilities.OK {
		if !truthy(capabilities.parsed["ok"]) {
			report.Steps[len(report.Steps)-1].OK = false
			report.Steps[len(report.Steps)-1].Error = "capabilities report was not ok"
			report.OK = false
		} else {
			report.Steps[len(report.Steps)-1].Detail = capabilitiesSummary(capabilities.parsed)
		}
	}

	demoOut := filepath.Join(workDir, "demo.png")
	demo := run("demo render and JSON cleanliness", "render", "--demo", "--out", demoOut, "--size", "96x72", "--json")
	if demo.OK {
		if strings.Contains(demo.stderrRaw, "Processing ") {
			report.markLastFailed("renderer progress leaked to stderr")
		} else if err := requireNonEmptyFile(demoOut); err != nil {
			report.markLastFailed(err.Error())
		} else if outputReport, ok := firstOutputReportFromParsed(demo.parsed); !ok {
			report.markLastFailed("demo render did not report output verification")
		} else if err := checkOutputReportAgainstGolden(outputReport, demoVisualGolden); err != nil {
			report.markLastFailed(err.Error())
		} else {
			report.Steps[len(report.Steps)-1].Detail = "visual golden " + outputReport.AverageHash
			report.Artifacts = append(report.Artifacts, demoOut)
		}
	}

	workerOut := filepath.Join(workDir, "worker.png")
	worker := run("worker render", "render", "--demo", "--out", workerOut, "--size", "96x72", "--renderer-mode", "worker", "--json")
	if !worker.OK && !flags.requireWorker {
		report.Steps[len(report.Steps)-1].OK = true
		report.Steps[len(report.Steps)-1].Detail = "optional worker unavailable"
		report.recomputeOK()
	} else if worker.OK {
		if err := requireNonEmptyFile(workerOut); err != nil {
			report.markLastFailed(err.Error())
		} else if !workerWasUsed(worker.parsed) {
			report.markLastFailed("worker renderer metadata was not reported")
		} else {
			report.Artifacts = append(report.Artifacts, workerOut)
		}
	}

	fallbackOut := filepath.Join(workDir, "fallback.png")
	fallback := run("fallback render", "render", "--demo", "--out", fallbackOut, "--size", "96x72", "--renderer-command", "false", "--json")
	if fallback.OK {
		if err := requireNonEmptyFile(fallbackOut); err != nil {
			report.markLastFailed(err.Error())
		} else if !fallbackWasUsed(fallback.parsed) {
			report.markLastFailed("fallback renderer metadata was not reported")
		} else {
			report.Artifacts = append(report.Artifacts, fallbackOut)
		}
	}

	completionDir := filepath.Join(workDir, "completions")
	run("completion generation", "completion", "all", "--out-dir", completionDir)
	if len(report.Steps) > 0 && report.Steps[len(report.Steps)-1].OK {
		for _, name := range []string{"molstar.bash", "_molstar", "molstar.fish", "molstar.ps1"} {
			path := filepath.Join(completionDir, name)
			if err := requireNonEmptyFile(path); err != nil {
				report.markLastFailed(err.Error())
				break
			}
			report.Artifacts = append(report.Artifacts, path)
		}
	}

	docsDir := filepath.Join(workDir, "docs")
	run("CLI docs generation", "docs", "--out", docsDir)
	if len(report.Steps) > 0 && report.Steps[len(report.Steps)-1].OK {
		rootDoc := filepath.Join(docsDir, "molstar.md")
		if err := requireNonEmptyFile(rootDoc); err != nil {
			report.markLastFailed(err.Error())
		} else {
			report.Artifacts = append(report.Artifacts, rootDoc)
		}
	}

	a.runOfflineCacheSelfTest(ctx, executable, workDir, flags.verbose, &report)

	report.DurationMS = time.Since(start).Milliseconds()
	report.addDiagnoses()
	if !report.ArtifactsKept {
		report.Artifacts = nil
	}
	if !report.OK {
		return report, markError(kindDoctor, fmt.Errorf("self-test failed"))
	}
	return report, nil
}

func (r *selfTestReport) recomputeOK() {
	r.OK = true
	for _, step := range r.Steps {
		if !step.OK {
			r.OK = false
			return
		}
	}
}

func (a app) runOfflineCacheSelfTest(ctx context.Context, executable string, workDir string, verbose bool, report *selfTestReport) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "chemical/x-mmcif")
		_, _ = w.Write([]byte(oneAtomCIF))
	}))
	cacheDir := filepath.Join(workDir, "cache")
	jobPath := filepath.Join(workDir, "offline-cache.job.json")
	networkOut := filepath.Join(workDir, "network-cache.png")
	offlineOut := filepath.Join(workDir, "offline-cache.png")
	if err := writeSelfTestURLJob(jobPath, server.URL+"/model.cif", networkOut); err != nil {
		report.Steps = append(report.Steps, selfTestStep{Name: "offline cache", OK: false, Error: err.Error()})
		report.OK = false
		server.Close()
		return
	}
	network := runSelfTestSubcommand(ctx, executable, workDir, "offline cache warmup", verbose, "render", jobPath, "--cache", cacheDir, "--json")
	report.Steps = append(report.Steps, network)
	if !network.OK {
		report.OK = false
		server.Close()
		return
	}
	server.Close()
	if err := writeSelfTestURLJob(jobPath, server.URL+"/model.cif", offlineOut); err != nil {
		report.Steps = append(report.Steps, selfTestStep{Name: "offline cache render", OK: false, Error: err.Error()})
		report.OK = false
		return
	}
	offline := runSelfTestSubcommand(ctx, executable, workDir, "offline cache render", verbose, "render", jobPath, "--cache", cacheDir, "--offline", "--json")
	report.Steps = append(report.Steps, offline)
	if !offline.OK {
		report.OK = false
		return
	}
	if err := requireNonEmptyFile(offlineOut); err != nil {
		report.markLastFailed(err.Error())
		return
	}
	if !offlineCacheHit(offline.parsed) {
		report.markLastFailed("offline render did not report a cache hit")
		return
	}
	report.Artifacts = append(report.Artifacts, networkOut, offlineOut)
}

func runSelfTestSubcommand(ctx context.Context, executable string, dir string, name string, verbose bool, args ...string) selfTestStep {
	start := time.Now()
	cmd := exec.Command(executable, args...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	step := selfTestStep{
		Name:    name,
		Command: append([]string{executable}, args...),
	}
	err := runCommandWithContext(ctx, cmd)
	step.DurationMS = time.Since(start).Milliseconds()
	rawStdout := truncateSelfTestOutput(stdout.String())
	rawStderr := truncateSelfTestOutput(stderr.String())
	step.stdoutRaw = rawStdout
	step.stderrRaw = rawStderr
	if err != nil {
		step.OK = false
		step.Error = err.Error()
		step.Stdout = rawStdout
		step.Stderr = rawStderr
		return step
	}
	var decoded map[string]any
	if len(bytes.TrimSpace(stdout.Bytes())) > 0 {
		if jsonErr := json.Unmarshal(stdout.Bytes(), &decoded); jsonErr == nil {
			step.parsed = decoded
			if verbose {
				step.Report = decoded
			}
		} else if expectsSelfTestJSON(args) {
			step.OK = false
			step.Error = "stdout was not valid JSON: " + jsonErr.Error()
			step.Stdout = rawStdout
			step.Stderr = rawStderr
			return step
		}
	}
	if verbose {
		step.Stdout = rawStdout
		step.Stderr = rawStderr
	}
	step.OK = true
	return step
}

func expectsSelfTestJSON(args []string) bool {
	for _, arg := range args {
		if arg == "--json" {
			return true
		}
	}
	return false
}

func (r *selfTestReport) markLastFailed(message string) {
	if len(r.Steps) == 0 {
		return
	}
	r.Steps[len(r.Steps)-1].OK = false
	r.Steps[len(r.Steps)-1].Error = message
	r.Steps[len(r.Steps)-1].Diagnosis = diagnoseSelfTestStep(r.Steps[len(r.Steps)-1])
	r.OK = false
}

func (r *selfTestReport) addDiagnoses() {
	for i := range r.Steps {
		if r.Steps[i].OK || len(r.Steps[i].Diagnosis) > 0 {
			continue
		}
		r.Steps[i].Diagnosis = diagnoseSelfTestStep(r.Steps[i])
	}
}

func diagnoseSelfTestStep(step selfTestStep) []string {
	text := strings.ToLower(step.Name + " " + step.Error + " " + step.stderrRaw)
	var hints []string
	if strings.Contains(text, "deadline") || strings.Contains(text, "timeout") {
		hints = append(hints, "rerun with a larger --timeout value")
	}
	switch {
	case strings.Contains(text, "doctor") || strings.Contains(text, "capabilities") || strings.Contains(text, "runtime"):
		hints = append(hints, "run `molstar update-runtime --json` to refresh the configured Node/Mol* runtime")
		hints = append(hints, "run `molstar capabilities --json --probe-worker` for the detailed renderer probe")
	case strings.Contains(text, "worker"):
		hints = append(hints, "run `molstar capabilities --json --probe-worker` to isolate worker startup failures")
		hints = append(hints, "rerun render with `--renderer-mode auto` to allow subprocess fallback")
	case strings.Contains(text, "golden") || strings.Contains(text, "demo render"):
		hints = append(hints, "run `molstar render --demo --out demo.png --size 96x72 --json --verbose` and inspect the output hash")
	case strings.Contains(text, "fallback"):
		hints = append(hints, "run `molstar doctor --json` and check renderer_fallback_command in the runtime config")
	case strings.Contains(text, "completion"):
		hints = append(hints, "run `molstar completion all --out-dir completions` to reproduce completion generation")
	case strings.Contains(text, "docs"):
		hints = append(hints, "run `molstar docs --out docs/cli` to reproduce documentation generation")
	case strings.Contains(text, "offline cache") || strings.Contains(text, "cache"):
		hints = append(hints, "rerun with `--keep --out-dir <dir>` and inspect the cache/downloads directory")
	}
	if strings.Contains(text, "executable file not found") || strings.Contains(text, "command not found") {
		hints = append(hints, "check PATH and rerun `molstar install-local --force` or install the packaged artifact")
	}
	if len(hints) == 0 {
		hints = append(hints, "rerun `molstar self-test --verbose --keep` to keep artifacts and include child command output")
	}
	return hints
}

func requireNonEmptyFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", path)
	}
	if info.Size() == 0 {
		return fmt.Errorf("%s is empty", path)
	}
	return nil
}

func truthy(value any) bool {
	v, ok := value.(bool)
	return ok && v
}

func fallbackWasUsed(report map[string]any) bool {
	commands, ok := report["commands"].([]any)
	if !ok || len(commands) == 0 {
		return false
	}
	first, ok := commands[0].(map[string]any)
	if !ok {
		return false
	}
	fallbackOf, ok := first["fallback_of"].([]any)
	return ok && len(fallbackOf) > 0
}

func workerWasUsed(report map[string]any) bool {
	commands, ok := report["commands"].([]any)
	if !ok || len(commands) == 0 {
		return false
	}
	first, ok := commands[0].(map[string]any)
	if !ok {
		return false
	}
	return truthy(first["worker"])
}

func offlineCacheHit(report map[string]any) bool {
	inputs, ok := report["cached_inputs"].([]any)
	if !ok || len(inputs) == 0 {
		return false
	}
	first, ok := inputs[0].(map[string]any)
	if !ok {
		return false
	}
	return truthy(first["cached"])
}

func capabilitiesSummary(report map[string]any) string {
	runtimeValue, _ := report["runtime"].(map[string]any)
	node, _ := runtimeValue["node"].(string)
	molstar, _ := runtimeValue["molstar"].(string)
	parts := []string{}
	if node != "" {
		parts = append(parts, "node "+node)
	}
	if molstar != "" {
		parts = append(parts, "molstar "+molstar)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ", ")
}

func truncateSelfTestOutput(value string) string {
	const limit = 4096
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "\n[truncated]"
}

func writeSelfTestURLJob(path string, url string, output string) error {
	spec := map[string]any{
		"version": 1,
		"inputs": map[string]any{
			"input": map[string]any{"url": url, "format": "mmcif"},
		},
		"scene": map[string]any{
			"canvas": map[string]any{"background": "white"},
			"structures": []any{map[string]any{
				"source": "input",
				"components": []any{map[string]any{
					"ref":    "all",
					"select": "all",
					"representation": map[string]any{
						"type":  "spacefill",
						"color": "#cc3399",
					},
				}},
			}},
			"camera": map[string]any{"focus": "all"},
		},
		"outputs": []any{map[string]any{"type": "image", "path": output, "size": []int{96, 72}}},
	}
	data, err := json.Marshal(spec)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
