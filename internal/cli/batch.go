package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/sacha-ichbiah/molstar/internal/job"
	"github.com/sacha-ichbiah/molstar/internal/mvs"
	"github.com/sacha-ichbiah/molstar/internal/render"
)

type batchFlags struct {
	concurrency     int
	outDir          string
	outTemplate     string
	retries         int
	skipExisting    bool
	manifestPath    string
	continueOnError bool
	rendererCommand string
	rendererMode    string
	workerCommand   string
	dryRun          bool
	quiet           bool
	verbose         bool
	jsonReport      bool
	runtime         runtimeFlags
}

type batchReport struct {
	OK           bool                   `json:"ok"`
	Index        int                    `json:"index"`
	ID           string                 `json:"id,omitempty"`
	Attempts     int                    `json:"attempts,omitempty"`
	Skipped      bool                   `json:"skipped,omitempty"`
	Outputs      []string               `json:"outputs,omitempty"`
	OutputFiles  []outputReport         `json:"output_files,omitempty"`
	MVS          string                 `json:"mvs,omitempty"`
	Warnings     []string               `json:"warnings,omitempty"`
	Themes       []mvs.ThemeExtension   `json:"themes,omitempty"`
	Error        string                 `json:"error,omitempty"`
	Commands     []render.CommandResult `json:"commands,omitempty"`
	RendererMode string                 `json:"renderer_mode,omitempty"`
}

type batchSummary struct {
	OK        bool          `json:"ok"`
	Summary   bool          `json:"summary"`
	Total     int           `json:"total"`
	Completed int           `json:"completed"`
	Succeeded int           `json:"succeeded"`
	Failed    int           `json:"failed"`
	Skipped   int           `json:"skipped"`
	Outputs   []string      `json:"outputs,omitempty"`
	Reports   []batchReport `json:"reports,omitempty"`
}

func (a app) batchCommand() *cobra.Command {
	flags := &batchFlags{}
	cmd := &cobra.Command{
		Use:   "batch JOBS",
		Short: "Render a JSONL/YAML/JSON batch of jobs",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("batch", flags.jsonReport, func() error {
				if err := exactArgs(args, 1, "batch"); err != nil {
					return markError(kindInvalidInput, err)
				}
				return a.runBatch(cmd.Context(), args[0], flags, cmd)
			})
		},
	}
	cmd.Flags().IntVar(&flags.concurrency, "concurrency", 1, "number of jobs to render concurrently")
	cmd.Flags().StringVar(&flags.outDir, "out-dir", "", "prepend this directory to relative output paths")
	cmd.Flags().StringVar(&flags.outTemplate, "out", "", "template for image/video outputs, e.g. renders/{id}.png")
	cmd.Flags().IntVar(&flags.retries, "retries", 0, "retry failed jobs up to N additional times")
	cmd.Flags().BoolVar(&flags.skipExisting, "skip-existing", false, "skip jobs whose declared outputs already exist and validate")
	cmd.Flags().StringVar(&flags.manifestPath, "manifest", "", "write a final batch summary JSON manifest to this path")
	cmd.Flags().BoolVar(&flags.continueOnError, "continue-on-error", false, "continue after a failed job")
	cmd.Flags().StringVar(&flags.rendererCommand, "renderer-command", "", "renderer command override")
	cmd.Flags().StringVar(&flags.rendererMode, "renderer-mode", "subprocess", "renderer mode: subprocess, worker, or auto")
	cmd.Flags().StringVar(&flags.workerCommand, "worker-command", "", "persistent renderer worker command override")
	cmd.Flags().BoolVar(&flags.dryRun, "dry-run", false, "print external renderer commands without running them")
	cmd.Flags().BoolVar(&flags.quiet, "quiet", false, "suppress renderer progress logs")
	cmd.Flags().BoolVar(&flags.verbose, "verbose", false, "show renderer progress logs even with --json")
	cmd.Flags().BoolVar(&flags.jsonReport, "json", true, "write JSON report lines to stdout")
	bindRuntimeFlags(cmd, &flags.runtime)
	return cmd
}

func (a app) runBatch(ctx context.Context, path string, flags *batchFlags, cmd *cobra.Command) error {
	if flags.quiet && flags.verbose {
		return markError(kindInvalidInput, fmt.Errorf("--quiet and --verbose cannot be used together"))
	}
	var jobs []job.Job
	var err error
	if path == "-" {
		data, name, err := a.readInput(path)
		if err != nil {
			return markError(kindInvalidInput, err)
		}
		jobs, err = job.LoadManyBytes(data, name)
	} else {
		jobs, err = job.LoadMany(path)
	}
	if err != nil {
		return markError(kindInvalidInput, err)
	}
	for i := range jobs {
		applyRuntimeFlags(cmd, &jobs[i], flags.runtime)
	}
	if err := detectOutputCollisions(jobs, flags); err != nil {
		return markError(kindInvalidInput, err)
	}
	if flags.concurrency < 1 {
		flags.concurrency = 1
	}
	baseRunner := a.batchRunner(flags)
	workerStderr := a.stderr
	if baseRunner.Quiet {
		workerStderr = nil
	}
	selection, err := selectWorkerRenderer(flags.rendererMode, flags.workerCommand, flags.concurrency, baseRunner, workerStderr, flags.dryRun)
	if err != nil {
		return markError(kindRuntime, err)
	}
	if selection.Close != nil {
		defer selection.Close()
	}
	if selection.FallbackError != nil {
		fmt.Fprintf(a.stderr, "renderer warning: worker pool failed (%s); using subprocess renderer\n", selection.FallbackError)
	}

	type work struct {
		index int
		job   job.Job
	}
	workCh := make(chan work)
	reportCh := make(chan batchReport)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	for i := 0; i < flags.concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range workCh {
				report := a.runBatchJobWithRetries(ctx, item.index, item.job, flags, baseRunner, selection)
				if report.Error != "" && !flags.continueOnError {
					cancel()
				}
				reportCh <- report
			}
		}()
	}

	go func() {
		defer close(workCh)
		for i, j := range jobs {
			select {
			case <-ctx.Done():
				return
			case workCh <- work{index: i, job: j}:
			}
		}
	}()

	go func() {
		wg.Wait()
		close(reportCh)
	}()

	var reports []batchReport
	for report := range reportCh {
		reports = append(reports, report)
		if report.Error != "" {
			// handled after summary construction
		}
		if flags.jsonReport {
			data, _ := json.Marshal(report)
			fmt.Fprintln(a.stdout, string(data))
		} else if report.Error != "" {
			fmt.Fprintf(a.stderr, "job %d failed: %s\n", report.Index, report.Error)
		} else if report.Skipped {
			fmt.Fprintf(a.stdout, "job %d skipped\n", report.Index)
		} else {
			fmt.Fprintf(a.stdout, "job %d ok\n", report.Index)
		}
	}
	summary := summarizeBatch(len(jobs), reports)
	if flags.manifestPath != "" {
		if err := writeJSONFile(flags.manifestPath, summary); err != nil {
			return markError(kindRender, err)
		}
	}
	if flags.jsonReport {
		data, _ := json.Marshal(summary)
		fmt.Fprintln(a.stdout, string(data))
	} else {
		fmt.Fprintf(a.stdout, "summary total=%d completed=%d ok=%d skipped=%d failed=%d\n", summary.Total, summary.Completed, summary.Succeeded, summary.Skipped, summary.Failed)
	}
	if !summary.OK {
		return markError(kindRender, fmt.Errorf("one or more batch jobs failed"))
	}
	return nil
}

func (a app) batchRunner(flags *batchFlags) render.Molstar {
	runner := render.NewMolstar()
	if flags.jsonReport {
		runner.Stdout = a.stderr
	} else {
		runner.Stdout = a.stdout
	}
	runner.Stderr = a.stderr
	runner.DryRun = flags.dryRun
	runner.Quiet = flags.quiet || (flags.jsonReport && !flags.verbose)
	if flags.rendererCommand != "" {
		runner.RendererCommand = strings.Fields(flags.rendererCommand)
	}
	return runner
}

func (a app) runBatchJobWithRetries(ctx context.Context, index int, j job.Job, flags *batchFlags, baseRunner render.Molstar, selection workerRendererSelection) batchReport {
	attempts := flags.retries + 1
	if attempts < 1 {
		attempts = 1
	}
	var last batchReport
	for attempt := 1; attempt <= attempts; attempt++ {
		report := a.runBatchJob(ctx, index, j, flags, baseRunner, selection)
		report.Attempts = attempt
		if report.OK || report.Skipped || attempt == attempts {
			return report
		}
		last = report
	}
	return last
}

func (a app) runBatchJob(ctx context.Context, index int, j job.Job, flags *batchFlags, baseRunner render.Molstar, selection workerRendererSelection) batchReport {
	report := batchReport{OK: true, Index: index, ID: jobID(j, index), RendererMode: selection.Mode}
	if flags.outTemplate != "" {
		j = withOutputTemplate(j, flags.outTemplate, index)
	}
	if flags.outDir != "" {
		j = withOutputDir(j, flags.outDir)
	}
	if flags.skipExisting {
		outputReports, ok, err := existingOutputsReport(j)
		if err != nil {
			report.OK = false
			report.Error = err.Error()
			return report
		}
		if ok {
			report.Skipped = true
			report.OutputFiles = outputReports
			for _, outputReport := range outputReports {
				report.Outputs = append(report.Outputs, outputReport.Path)
			}
			return report
		}
	}
	ctx, cancel := contextWithRuntimeTimeout(ctx, j.Runtime)
	defer cancel()
	j, runtimeReport, err := job.PrepareRuntime(ctx, j)
	if err != nil {
		report.OK = false
		report.Error = err.Error()
		return report
	}
	compiled, err := mvs.Compile(j)
	if err != nil {
		report.OK = false
		report.Error = err.Error()
		return report
	}
	report.Warnings = compiled.Warnings
	report.Themes = compiled.ThemeExtensions
	for _, cached := range runtimeReport.CachedInputs {
		report.Warnings = append(report.Warnings, fmt.Sprintf("cached %s -> %s", cached.Ref, cached.Path))
	}
	tmp, err := os.CreateTemp("", fmt.Sprintf("headlessmolstar-%d-*.mvsj", index))
	if err != nil {
		report.OK = false
		report.Error = err.Error()
		return report
	}
	scenePath := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(scenePath)
	if err := mvs.WriteFile(scenePath, compiled.Document); err != nil {
		report.OK = false
		report.Error = err.Error()
		return report
	}
	report.MVS = scenePath

	runner := baseRunner
	renderer := render.ImageRenderer(runner)
	if selection.Pool != nil {
		renderer = selection.Pool
	}
	stateOut := ""
	hasRenderOutput := false
	for _, output := range j.Outputs {
		kind := output.NormalizedType()
		switch kind {
		case "mvsj", "mvsx", "image", "video":
			if kind == "image" || kind == "video" {
				hasRenderOutput = true
			}
		case "molj":
			if stateOut == "" {
				stateOut = output.Path
			}
		default:
			report.OK = false
			report.Error = fmt.Sprintf("outputs: unsupported output type %q", output.Type)
			return report
		}
	}
	if !hasRenderOutput && len(j.Outputs) == 0 {
		report.OK = false
		report.Error = "no outputs configured"
		return report
	}
	if !hasRenderOutput && stateOut != "" {
		report.OK = false
		report.Error = "molj output requires at least one image or video output"
		return report
	}
	rendered := 0
	for _, output := range j.Outputs {
		switch output.NormalizedType() {
		case "mvsj":
			if flags.dryRun {
				fmt.Fprintln(a.stderr, "dry-run: would write", output.Path)
			} else {
				outputReport, err := writeMVSJTransactional(output.Path, compiled.Document)
				if err != nil {
					report.OK = false
					report.Error = err.Error()
					return report
				}
				report.OutputFiles = append(report.OutputFiles, outputReport)
			}
			report.Outputs = append(report.Outputs, output.Path)
		case "mvsx":
			if flags.dryRun {
				fmt.Fprintln(a.stderr, "dry-run: would write", output.Path)
			} else {
				outputReport, err := writeMVSXTransactional(output.Path, j, compiled.Document)
				if err != nil {
					report.OK = false
					report.Error = err.Error()
					return report
				}
				report.OutputFiles = append(report.OutputFiles, outputReport)
			}
			report.Outputs = append(report.Outputs, output.Path)
		case "image", "video":
			saveMolj := stateOut != "" && rendered == 0
			result, outputReports, err := renderTransactional(ctx, renderer, scenePath, output, saveMolj, stateOut, flags.dryRun)
			report.Commands = append(report.Commands, result)
			if err != nil {
				report.OK = false
				report.Error = err.Error()
				return report
			}
			for _, outputReport := range outputReports {
				report.Outputs = append(report.Outputs, outputReport.Path)
				report.OutputFiles = append(report.OutputFiles, outputReport)
			}
			rendered++
		}
	}
	return report
}

func summarizeBatch(total int, reports []batchReport) batchSummary {
	summary := batchSummary{OK: true, Summary: true, Total: total, Completed: len(reports), Reports: reports}
	for _, report := range reports {
		if report.Error != "" || !report.OK {
			summary.OK = false
			summary.Failed++
		} else {
			summary.Succeeded++
		}
		if report.Skipped {
			summary.Skipped++
		}
		summary.Outputs = append(summary.Outputs, report.Outputs...)
	}
	if len(reports) < total {
		summary.OK = false
		summary.Failed += total - len(reports)
	}
	return summary
}

func existingOutputsReport(j job.Job) ([]outputReport, bool, error) {
	if len(j.Outputs) == 0 {
		return nil, false, nil
	}
	var reports []outputReport
	for _, output := range j.Outputs {
		if strings.TrimSpace(output.Path) == "" {
			return nil, false, nil
		}
		if _, err := os.Stat(output.Path); err != nil {
			if os.IsNotExist(err) {
				return nil, false, nil
			}
			return nil, false, err
		}
		switch output.NormalizedType() {
		case "image", "video":
			report, err := verifyOutput(output.Path, output)
			if err != nil {
				return nil, false, err
			}
			reports = append(reports, report)
		case "mvsj", "mvsx", "molj":
			report, err := exportedFileReport(output.Path, output.NormalizedType())
			if err != nil {
				return nil, false, err
			}
			reports = append(reports, report)
		default:
			return nil, false, nil
		}
	}
	return reports, true, nil
}

func withOutputDir(j job.Job, outDir string) job.Job {
	for i := range j.Outputs {
		if j.Outputs[i].Path != "" && !filepath.IsAbs(j.Outputs[i].Path) {
			j.Outputs[i].Path = filepath.Join(outDir, j.Outputs[i].Path)
		}
	}
	return j
}

func withOutputTemplate(j job.Job, template string, index int) job.Job {
	for i := range j.Outputs {
		kind := j.Outputs[i].NormalizedType()
		if kind != "image" && kind != "video" {
			continue
		}
		j.Outputs[i].Path = renderOutputTemplate(template, j, j.Outputs[i], index)
	}
	return j
}

func renderOutputTemplate(template string, j job.Job, output job.Output, index int) string {
	id := jobID(j, index)
	input := firstInputLabel(j)
	ext := strings.TrimPrefix(filepath.Ext(output.Path), ".")
	if ext == "" {
		if output.NormalizedType() == "video" {
			ext = "mp4"
		} else {
			ext = "png"
		}
	}
	result := template
	replacements := map[string]string{
		"{id}":    sanitizeRef(id),
		"{index}": fmt.Sprintf("%d", index),
		"{input}": sanitizeRef(input),
		"{type}":  output.NormalizedType(),
		"{ext}":   ext,
	}
	for old, value := range replacements {
		result = strings.ReplaceAll(result, old, value)
	}
	return result
}

func jobID(j job.Job, index int) string {
	for _, ref := range sortedInputRefs(j.Inputs) {
		input := j.Inputs[ref]
		if input.ID != "" {
			return input.ID
		}
		if input.Path != "" {
			return strings.TrimSuffix(filepath.Base(input.Path), filepath.Ext(input.Path))
		}
		if input.URL != "" {
			base := filepath.Base(strings.Split(input.URL, "?")[0])
			if base != "" && base != "." && base != "/" {
				return strings.TrimSuffix(base, filepath.Ext(base))
			}
		}
		if ref != "" {
			return ref
		}
	}
	return fmt.Sprintf("job-%d", index)
}

func firstInputLabel(j job.Job) string {
	for _, ref := range sortedInputRefs(j.Inputs) {
		input := j.Inputs[ref]
		if input.ID != "" {
			return input.ID
		}
		if input.Path != "" {
			return filepath.Base(input.Path)
		}
		if input.URL != "" {
			return filepath.Base(strings.Split(input.URL, "?")[0])
		}
		if ref != "" {
			return ref
		}
	}
	return "input"
}

// sortedInputRefs returns the input map keys in a stable order so that jobID,
// firstInputLabel, and template expansion are deterministic across runs for
// jobs with more than one input (map iteration order is otherwise random).
func sortedInputRefs(inputs map[string]job.Input) []string {
	refs := make([]string, 0, len(inputs))
	for ref := range inputs {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	return refs
}

// resolvedPrimaryOutputs returns the image/video output paths a job would write
// after applying the --out template and --out-dir, without mutating the job.
func resolvedPrimaryOutputs(j job.Job, flags *batchFlags, index int) []string {
	var paths []string
	for _, out := range j.Outputs {
		kind := out.NormalizedType()
		if kind != "image" && kind != "video" {
			continue
		}
		path := out.Path
		if flags.outTemplate != "" {
			path = renderOutputTemplate(flags.outTemplate, j, out, index)
		}
		if flags.outDir != "" && path != "" && !filepath.IsAbs(path) {
			path = filepath.Join(flags.outDir, path)
		}
		if path != "" {
			paths = append(paths, filepath.Clean(path))
		}
	}
	return paths
}

// detectOutputCollisions fails fast when two distinct jobs in the batch resolve
// to the same primary output path. Without this guard the later job silently
// overwrites the earlier one — e.g. rendering the same structure with two
// presets under a "{id}" template. The fix is to add "{index}" (or distinct
// paths) so each job writes somewhere unique.
func detectOutputCollisions(jobs []job.Job, flags *batchFlags) error {
	seen := make(map[string]int)
	for i, j := range jobs {
		jobPaths := make(map[string]bool)
		for _, path := range resolvedPrimaryOutputs(j, flags, i) {
			if jobPaths[path] {
				continue // duplicate within a single job's own spec
			}
			jobPaths[path] = true
			if prev, ok := seen[path]; ok {
				return fmt.Errorf("jobs %d and %d both write output %q; add {index} to --out or give the jobs distinct output paths", prev, i, path)
			}
			seen[path] = i
		}
	}
	return nil
}
