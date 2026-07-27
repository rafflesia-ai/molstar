package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/rafflesia-ai/molstar/internal/job"
	"github.com/rafflesia-ai/molstar/internal/render"
)

type benchFlags struct {
	iterations      int
	warmup          int
	size            string
	outDir          string
	provider        string
	format          string
	assembly        string
	preset          string
	background      string
	rendererCommand string
	rendererMode    string
	workerCommand   string
	reportOut       string
	label           string
	baseline        string
	failRegression  string
	maxRegression   float64
	dryRun          bool
	demo            bool
	quiet           bool
	verbose         bool
	jsonReport      bool
	continueOnError bool
	runtime         runtimeFlags
}

type benchReport struct {
	OK           bool             `json:"ok"`
	Label        string           `json:"label,omitempty"`
	Input        string           `json:"input"`
	OutputDir    string           `json:"output_dir"`
	Iterations   int              `json:"iterations"`
	Warmup       int              `json:"warmup"`
	RendererMode string           `json:"renderer_mode"`
	CLI          cliVersionReport `json:"cli"`
	Environment  benchEnvironment `json:"environment"`
	StartedAt    string           `json:"started_at"`
	DurationMS   int64            `json:"duration_ms"`
	Summary      benchSummary     `json:"summary"`
	Comparison   *benchComparison `json:"comparison,omitempty"`
	ReportFile   *outputReport    `json:"report_file,omitempty"`
	Runs         []benchRunReport `json:"runs"`
}

type benchEnvironment struct {
	GOOS       string `json:"goos"`
	GOARCH     string `json:"goarch"`
	CPUs       int    `json:"cpus"`
	GoVersion  string `json:"go_version"`
	Executable string `json:"executable,omitempty"`
}

type benchSummary struct {
	Succeeded int     `json:"succeeded"`
	Failed    int     `json:"failed"`
	MinMS     int64   `json:"min_ms"`
	MaxMS     int64   `json:"max_ms"`
	AvgMS     float64 `json:"avg_ms"`
	P50MS     int64   `json:"p50_ms"`
	P95MS     int64   `json:"p95_ms"`
}

type benchRunReport struct {
	Index      int           `json:"index"`
	Warmup     bool          `json:"warmup,omitempty"`
	OK         bool          `json:"ok"`
	StartedAt  string        `json:"started_at"`
	DurationMS int64         `json:"duration_ms"`
	Report     *renderReport `json:"report,omitempty"`
	Error      *errorBody    `json:"error,omitempty"`
}

type benchComparison struct {
	BaselinePath         string  `json:"baseline_path"`
	BaselineAvgMS        float64 `json:"baseline_avg_ms"`
	CurrentAvgMS         float64 `json:"current_avg_ms"`
	AvgDeltaPercent      float64 `json:"avg_delta_percent"`
	BaselineP95MS        int64   `json:"baseline_p95_ms"`
	CurrentP95MS         int64   `json:"current_p95_ms"`
	P95DeltaPercent      float64 `json:"p95_delta_percent"`
	MaxRegressionPercent float64 `json:"max_regression_percent"`
	OK                   bool    `json:"ok"`
}

func (a app) benchCommand() *cobra.Command {
	flags := &benchFlags{
		iterations:    3,
		warmup:        1,
		size:          "256x256",
		provider:      "pdbe",
		preset:        "default",
		background:    "white",
		outDir:        "",
		maxRegression: 25,
	}
	cmd := &cobra.Command{
		Use:   "bench [INPUT]",
		Short: "Benchmark the headless renderer with a local fixture, identifier, structure, job, or recipe",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("bench", flags.jsonReport, func() error {
				if len(args) > 1 {
					return markError(kindInvalidInput, fmt.Errorf("bench accepts at most one input"))
				}
				if flags.demo && len(args) != 0 {
					return markError(kindInvalidInput, fmt.Errorf("--demo cannot be combined with an explicit input"))
				}
				input := "demo"
				if len(args) == 1 {
					input = args[0]
				}
				if flags.demo {
					input = "demo"
				}
				return a.runBench(cmd.Context(), input, flags, cmd)
			})
		},
	}
	cmd.Flags().IntVar(&flags.iterations, "iterations", flags.iterations, "measured render iterations")
	cmd.Flags().IntVar(&flags.warmup, "warmup", flags.warmup, "warmup iterations before measurement")
	cmd.Flags().StringVar(&flags.size, "size", flags.size, "output size as WIDTHxHEIGHT")
	cmd.Flags().StringVar(&flags.outDir, "out-dir", flags.outDir, "directory for benchmark artifacts; defaults to a temporary directory")
	cmd.Flags().StringVar(&flags.provider, "provider", flags.provider, "identifier provider: pdbe, rcsb, alphafold")
	cmd.Flags().StringVar(&flags.format, "format", "", "input format override")
	cmd.Flags().StringVar(&flags.assembly, "assembly", "", "assembly identifier")
	cmd.Flags().StringVar(&flags.preset, "preset", flags.preset, "render preset: default, ligand, polymer, surface, confidence, overview")
	cmd.Flags().StringVar(&flags.background, "background", flags.background, "canvas background color")
	cmd.Flags().StringVar(&flags.rendererCommand, "renderer-command", "", "renderer command override")
	cmd.Flags().StringVar(&flags.rendererMode, "renderer-mode", "subprocess", "renderer mode: subprocess, worker, or auto")
	cmd.Flags().StringVar(&flags.workerCommand, "worker-command", "", "persistent renderer worker command override")
	cmd.Flags().StringVar(&flags.reportOut, "report", "", "write the benchmark report to this JSON path")
	cmd.Flags().StringVar(&flags.label, "label", "", "label for the benchmark snapshot")
	cmd.Flags().StringVar(&flags.baseline, "baseline", "", "previous benchmark JSON snapshot to compare against")
	cmd.Flags().StringVar(&flags.failRegression, "fail-regression", "", "maximum allowed avg/p95 regression when --baseline is set; accepts values like 20 or 20%")
	cmd.Flags().Float64Var(&flags.maxRegression, "max-regression-percent", flags.maxRegression, "maximum allowed avg/p95 regression when --baseline is set")
	cmd.Flags().BoolVar(&flags.dryRun, "dry-run", false, "plan benchmark renders without running the renderer")
	cmd.Flags().BoolVar(&flags.demo, "demo", false, "benchmark the built-in local fixture without network")
	cmd.Flags().BoolVar(&flags.quiet, "quiet", false, "suppress renderer progress logs")
	cmd.Flags().BoolVar(&flags.verbose, "verbose", false, "show renderer progress logs")
	cmd.Flags().BoolVar(&flags.jsonReport, "json", false, "write a machine-readable benchmark report")
	cmd.Flags().BoolVar(&flags.continueOnError, "continue-on-error", false, "continue running iterations after a failed render")
	bindRuntimeFlags(cmd, &flags.runtime)
	return cmd
}

func (a app) runBench(ctx context.Context, input string, flags *benchFlags, cmd *cobra.Command) error {
	if flags.iterations < 1 {
		return markError(kindInvalidInput, fmt.Errorf("--iterations must be at least 1"))
	}
	if flags.warmup < 0 {
		return markError(kindInvalidInput, fmt.Errorf("--warmup must be non-negative"))
	}
	size, err := parseSize(flags.size)
	if err != nil {
		return markError(kindInvalidInput, err)
	}
	if flags.quiet && flags.verbose {
		return markError(kindInvalidInput, fmt.Errorf("--quiet and --verbose cannot be used together"))
	}
	if strings.TrimSpace(flags.failRegression) != "" {
		limit, err := parseRegressionPercent(flags.failRegression)
		if err != nil {
			return markError(kindInvalidInput, err)
		}
		flags.maxRegression = limit
	}
	outDir, cleanup, err := benchOutputDir(flags.outDir)
	if err != nil {
		return markError(kindRuntime, err)
	}
	defer cleanup()

	baseJob, err := a.benchBaseJob(input, flags, outDir, size)
	if err != nil {
		return markError(kindInvalidInput, err)
	}
	applyRuntimeFlags(cmd, &baseJob, flags.runtime)

	runner := render.NewMolstar()
	runner.Stdout = io.Discard
	runner.Stderr = a.stderr
	runner.DryRun = flags.dryRun
	runner.Quiet = flags.quiet || !flags.verbose
	if flags.rendererCommand != "" {
		runner.RendererCommand = strings.Fields(flags.rendererCommand)
	}
	selection, err := selectWorkerRenderer(flags.rendererMode, flags.workerCommand, 1, runner, a.stderr, flags.dryRun)
	if err != nil {
		return markError(kindRuntime, err)
	}
	if selection.Close != nil {
		defer selection.Close()
	}
	var pooled render.ImageRenderer
	if selection.Pool != nil {
		pooled = selection.Pool
	}

	report := benchReport{
		OK:           true,
		Label:        strings.TrimSpace(flags.label),
		Input:        input,
		OutputDir:    outDir,
		Iterations:   flags.iterations,
		Warmup:       flags.warmup,
		RendererMode: selection.Mode,
		CLI:          buildVersionReport(ctx, &versionFlags{skipRuntime: true}).CLI,
		Environment:  buildBenchEnvironment(),
		StartedAt:    time.Now().UTC().Format(time.RFC3339Nano),
	}
	start := time.Now()
	totalRuns := flags.warmup + flags.iterations
	for i := 0; i < totalRuns; i++ {
		warmup := i < flags.warmup
		runIndex := i - flags.warmup + 1
		if warmup {
			runIndex = i + 1
		}
		run := a.runBenchIteration(ctx, input, baseJob, outDir, size, i+1, warmup, flags, pooled)
		report.Runs = append(report.Runs, run)
		if !run.OK {
			report.OK = false
			if !flags.continueOnError {
				break
			}
		}
		_ = runIndex
	}
	report.DurationMS = time.Since(start).Milliseconds()
	report.Summary = summarizeBenchRuns(report.Runs)
	if flags.baseline != "" {
		comparison, err := compareBenchSnapshot(flags.baseline, report.Summary, flags.maxRegression)
		if err != nil {
			return markError(kindInvalidInput, err)
		}
		report.Comparison = &comparison
		report.OK = report.OK && comparison.OK
	}
	if flags.reportOut != "" {
		data, err := marshalJSON(report)
		if err != nil {
			return markError(kindInternal, err)
		}
		outputReport, err := writeReportTransactional(flags.reportOut, data)
		if err != nil {
			return markError(kindRender, err)
		}
		report.ReportFile = &outputReport
	}

	if flags.jsonReport {
		if err := writeJSON(a.stdout, report); err != nil {
			return markError(kindRender, err)
		}
		if !report.OK {
			return alreadyReported(markError(kindRender, benchFailureError(report)))
		}
		return nil
	}
	fmt.Fprintf(a.stdout, "bench %s: %d/%d succeeded, avg %.1f ms, p50 %d ms, p95 %d ms, artifacts %s\n",
		statusWord(report.OK),
		report.Summary.Succeeded,
		flags.iterations,
		report.Summary.AvgMS,
		report.Summary.P50MS,
		report.Summary.P95MS,
		outDir,
	)
	if report.Comparison != nil {
		fmt.Fprintf(a.stdout, "comparison %s: avg %.1f%%, p95 %.1f%% against %s\n",
			statusWord(report.Comparison.OK),
			report.Comparison.AvgDeltaPercent,
			report.Comparison.P95DeltaPercent,
			report.Comparison.BaselinePath,
		)
	}
	if report.ReportFile != nil {
		fmt.Fprintf(a.stdout, "report %s\n", report.ReportFile.Path)
	}
	if !report.OK {
		return markError(kindRender, benchFailureError(report))
	}
	return nil
}

func parseRegressionPercent(value string) (float64, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, fmt.Errorf("--fail-regression requires a percentage")
	}
	trimmed = strings.TrimSuffix(trimmed, "%")
	percent, err := strconv.ParseFloat(strings.TrimSpace(trimmed), 64)
	if err != nil {
		return 0, fmt.Errorf("--fail-regression must be a number or percentage")
	}
	if math.IsNaN(percent) || math.IsInf(percent, 0) || percent < 0 {
		return 0, fmt.Errorf("--fail-regression must be a non-negative percentage")
	}
	return percent, nil
}

func (a app) runBenchIteration(ctx context.Context, input string, baseJob job.Job, outDir string, size []int, sequence int, warmup bool, flags *benchFlags, pooled render.ImageRenderer) benchRunReport {
	start := time.Now()
	j := benchJobForIteration(baseJob, outDir, size, sequence, warmup)
	rendererCommand := flags.rendererCommand
	run := benchRunReport{
		Index:     sequence,
		Warmup:    warmup,
		StartedAt: start.UTC().Format(time.RFC3339Nano),
	}
	report, err := a.executeRenderJobWithRenderer(ctx, input, j, rendererCommand, flags.dryRun, flags.quiet, flags.verbose, pooled)
	run.DurationMS = time.Since(start).Milliseconds()
	if err != nil {
		run.OK = false
		body := newErrorBody(err)
		run.Error = &body
		run.Report = &report
		return run
	}
	run.OK = true
	run.Report = &report
	return run
}

func (a app) benchBaseJob(input string, flags *benchFlags, outDir string, size []int) (job.Job, error) {
	if input == "" || input == "demo" {
		return benchDemoJob(outDir, size)
	}
	renderFlags := &renderFlags{
		out:             filepath.Join(outDir, "bench-input.png"),
		provider:        flags.provider,
		format:          flags.format,
		assembly:        flags.assembly,
		background:      flags.background,
		size:            flags.size,
		preset:          flags.preset,
		rendererCommand: flags.rendererCommand,
		rendererMode:    flags.rendererMode,
		workerCommand:   flags.workerCommand,
		dryRun:          flags.dryRun,
		quiet:           flags.quiet,
		verbose:         flags.verbose,
		runtime:         flags.runtime,
	}
	j, err := a.loadOrBuildJob(input, renderFlags)
	if err != nil {
		return job.Job{}, err
	}
	if len(imageOutputsFromJob(j)) == 0 {
		j.Outputs = append(j.Outputs, job.Output{Type: "image", Path: filepath.Join(outDir, "bench-input.png"), Size: size})
	}
	return j, nil
}

func benchDemoJob(outDir string, size []int) (job.Job, error) {
	cifPath := filepath.Join(outDir, "bench-demo.cif")
	if err := os.WriteFile(cifPath, []byte(oneAtomCIF), 0o644); err != nil {
		return job.Job{}, err
	}
	return job.Job{
		Version: 1,
		Runtime: job.Runtime{Strict: true, Profile: "locked", AllowPaths: []string{outDir}},
		Inputs: map[string]job.Input{
			"demo": {Path: cifPath, Format: "mmcif"},
		},
		Scene: job.Scene{
			Canvas: job.Canvas{Background: "white"},
			Structures: []job.Structure{{
				Ref:    "demo",
				Source: "demo",
				Components: []job.Component{{
					Ref:            "all",
					Select:         "all",
					Representation: job.Representation{Type: "spacefill", Color: "#cc3399"},
				}},
			}},
			Camera: job.Camera{Focus: "all"},
		},
		Outputs: []job.Output{{Type: "image", Path: filepath.Join(outDir, "bench-demo.png"), Size: size}},
	}, nil
}

func benchJobForIteration(base job.Job, outDir string, size []int, sequence int, warmup bool) job.Job {
	j := base
	outputs := imageOutputsFromJob(base)
	if len(outputs) == 0 {
		outputs = []job.Output{{Type: "image", Path: filepath.Join(outDir, "bench.png"), Size: size}}
	}
	prefix := "run"
	if warmup {
		prefix = "warmup"
	}
	for i := range outputs {
		ext := filepath.Ext(outputs[i].Path)
		if ext == "" {
			ext = ".png"
		}
		outputs[i].Path = filepath.Join(outDir, fmt.Sprintf("%s-%03d-%02d%s", prefix, sequence, i+1, ext))
		outputs[i].Size = append([]int{}, size...)
		if outputs[i].Type == "" {
			outputs[i].Type = outputTypeFromPath(outputs[i].Path)
		}
	}
	j.Outputs = outputs
	return j
}

func benchOutputDir(value string) (string, func(), error) {
	if strings.TrimSpace(value) != "" {
		if err := os.MkdirAll(value, 0o755); err != nil {
			return "", func() {}, err
		}
		abs, err := filepath.Abs(value)
		if err != nil {
			return "", func() {}, err
		}
		return abs, func() {}, nil
	}
	dir, err := os.MkdirTemp("", "headlessmolstar-bench-*")
	if err != nil {
		return "", func() {}, err
	}
	return dir, func() {}, nil
}

func summarizeBenchRuns(runs []benchRunReport) benchSummary {
	var measured []int64
	var summary benchSummary
	for _, run := range runs {
		if run.Warmup {
			continue
		}
		if run.OK {
			summary.Succeeded++
			measured = append(measured, run.DurationMS)
		} else {
			summary.Failed++
		}
	}
	if len(measured) == 0 {
		return summary
	}
	sort.Slice(measured, func(i, j int) bool { return measured[i] < measured[j] })
	var total int64
	for _, value := range measured {
		total += value
	}
	summary.MinMS = measured[0]
	summary.MaxMS = measured[len(measured)-1]
	summary.AvgMS = float64(total) / float64(len(measured))
	summary.P50MS = percentile(measured, 0.50)
	summary.P95MS = percentile(measured, 0.95)
	return summary
}

func buildBenchEnvironment() benchEnvironment {
	executable, _ := os.Executable()
	return benchEnvironment{
		GOOS:       runtime.GOOS,
		GOARCH:     runtime.GOARCH,
		CPUs:       runtime.NumCPU(),
		GoVersion:  runtime.Version(),
		Executable: executable,
	}
}

func compareBenchSnapshot(path string, current benchSummary, maxRegressionPercent float64) (benchComparison, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return benchComparison{}, err
	}
	var baseline benchReport
	if err := json.Unmarshal(data, &baseline); err != nil {
		return benchComparison{}, err
	}
	comparison := benchComparison{
		BaselinePath:         path,
		BaselineAvgMS:        baseline.Summary.AvgMS,
		CurrentAvgMS:         current.AvgMS,
		AvgDeltaPercent:      percentDelta(baseline.Summary.AvgMS, current.AvgMS),
		BaselineP95MS:        baseline.Summary.P95MS,
		CurrentP95MS:         current.P95MS,
		P95DeltaPercent:      percentDelta(float64(baseline.Summary.P95MS), float64(current.P95MS)),
		MaxRegressionPercent: maxRegressionPercent,
		OK:                   true,
	}
	if baseline.Summary.Succeeded == 0 {
		return benchComparison{}, fmt.Errorf("baseline benchmark has no successful measured runs: %s", path)
	}
	if current.Succeeded == 0 {
		comparison.OK = false
		return comparison, nil
	}
	if comparison.AvgDeltaPercent > maxRegressionPercent || comparison.P95DeltaPercent > maxRegressionPercent {
		comparison.OK = false
	}
	return comparison, nil
}

func percentDelta(baseline float64, current float64) float64 {
	if baseline <= 0 {
		if current <= 0 {
			return 0
		}
		return 100
	}
	return ((current - baseline) / baseline) * 100
}

func benchFailureError(report benchReport) error {
	if report.Summary.Failed > 0 {
		return fmt.Errorf("bench failed: %d failed iteration(s)", report.Summary.Failed)
	}
	if report.Comparison != nil && !report.Comparison.OK {
		return fmt.Errorf("bench regression exceeded %.1f%% threshold", report.Comparison.MaxRegressionPercent)
	}
	return fmt.Errorf("bench failed")
}

func percentile(values []int64, p float64) int64 {
	if len(values) == 0 {
		return 0
	}
	index := int(math.Ceil(p*float64(len(values)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index]
}

func statusWord(ok bool) string {
	if ok {
		return "ok"
	}
	return "failed"
}
