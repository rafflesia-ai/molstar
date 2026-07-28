package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/rafflesia-ai/molstar/internal/job"
	"github.com/rafflesia-ai/molstar/internal/mvs"
	"github.com/rafflesia-ai/molstar/internal/render"
)

type renderFlags struct {
	out        string
	mvsOut     string
	stateOut   string
	provider   string
	format     string
	assembly   string
	background string
	focus      string
	view       string
	size       string
	// sizeExplicit reports that the caller actually chose `size`, rather than
	// inheriting the flag default. With --out, a job or recipe's declared output
	// size wins unless this is set. Callers that build renderFlags directly must
	// set it alongside `size`, or their size is silently ignored.
	sizeExplicit      bool
	preset            string
	selectors         []string
	representations   []string
	colors            []string
	rendererCommand   string
	rendererMode      string
	workerCommand     string
	dryRun            bool
	quiet             bool
	verbose           bool
	demo              bool
	jsonReport        bool
	compact           bool
	diagnose          bool
	reportOut         string
	showReport        bool
	openOutput        bool
	explain           bool
	runLabel          string
	noLog             bool
	logAssets         bool
	logAssetMaxBytes  int64
	logAssetsMaxBytes int64
	ciArtifact        string
	runtime           runtimeFlags
}

type renderReport struct {
	OK           bool                   `json:"ok"`
	Input        string                 `json:"input"`
	MVS          string                 `json:"mvs,omitempty"`
	Outputs      []string               `json:"outputs,omitempty"`
	OutputFiles  []outputReport         `json:"output_files,omitempty"`
	Warnings     []string               `json:"warnings,omitempty"`
	Themes       []mvs.ThemeExtension   `json:"themes,omitempty"`
	CachedInputs []job.CachedInput      `json:"cached_inputs,omitempty"`
	Commands     []render.CommandResult `json:"commands,omitempty"`
	Diagnostics  map[string]any         `json:"diagnostics,omitempty"`
	Stages       []stageReport          `json:"stages,omitempty"`
	RunLog       string                 `json:"run_log,omitempty"`
	RunID        string                 `json:"run_id,omitempty"`
	RunLabel     string                 `json:"run_label,omitempty"`
	Job          *job.Job               `json:"job,omitempty"`
	MVSDocument  *mvs.Document          `json:"mvs_document,omitempty"`
}

type renderExplainReport struct {
	OK              bool                 `json:"ok"`
	Input           string               `json:"input"`
	Kind            string               `json:"kind"`
	Provider        string               `json:"provider,omitempty"`
	Format          string               `json:"format,omitempty"`
	Runtime         job.Runtime          `json:"runtime,omitempty"`
	Inputs          map[string]job.Input `json:"inputs,omitempty"`
	Components      []job.Component      `json:"components,omitempty"`
	Outputs         []job.Output         `json:"outputs,omitempty"`
	Camera          job.Camera           `json:"camera,omitempty"`
	Canvas          job.Canvas           `json:"canvas,omitempty"`
	MVSNodeCount    int                  `json:"mvs_node_count,omitempty"`
	CachedInputs    []job.CachedInput    `json:"cached_inputs,omitempty"`
	Warnings        []string             `json:"warnings,omitempty"`
	RendererMode    string               `json:"renderer_mode,omitempty"`
	NetworkDisabled bool                 `json:"network_disabled,omitempty"`
}

func (a app) renderCommand() *cobra.Command {
	flags := &renderFlags{
		logAssets:         true,
		logAssetMaxBytes:  1 << 20,
		logAssetsMaxBytes: 8 << 20,
	}
	cmd := &cobra.Command{
		Use:   "render INPUT",
		Short: "Render an identifier, job, or MVS scene",
		RunE: func(cmd *cobra.Command, args []string) error {
			if flags.diagnose {
				flags.jsonReport = true
			}
			flags.sizeExplicit = cmd.Flags().Changed("size")
			return a.runWithJSONErrors("render", flags.jsonReport, func() error {
				if flags.demo {
					if len(args) != 0 {
						return markError(kindInvalidInput, fmt.Errorf("render --demo does not accept an input argument"))
					}
					return a.runRender(cmd.Context(), "demo", flags, cmd)
				}
				if err := exactArgs(args, 1, "render"); err != nil {
					return markError(kindInvalidInput, err)
				}
				return a.runRender(cmd.Context(), args[0], flags, cmd)
			})
		},
	}
	cmd.Flags().StringVarP(&flags.out, "out", "o", "", "output image/movie path")
	cmd.Flags().StringVar(&flags.mvsOut, "mvs", "", "write compiled MVSJ scene")
	cmd.Flags().StringVar(&flags.stateOut, "state", "", "write Mol* .molj state next to the rendered image or to this path")
	cmd.Flags().StringVar(&flags.provider, "provider", "pdbe", "identifier provider: pdbe, rcsb, alphafold")
	cmd.Flags().StringVar(&flags.format, "format", "", "input format override")
	cmd.Flags().StringVar(&flags.assembly, "assembly", "", "assembly identifier")
	cmd.Flags().StringVar(&flags.background, "background", "white", "canvas background color")
	cmd.Flags().StringVar(&flags.focus, "focus", "", "component ref or selector to focus")
	cmd.Flags().StringVar(&flags.view, "view", "", "standard view: front, back, top, bottom, left, right")
	cmd.Flags().StringVar(&flags.size, "size", "800x800", "output size as WIDTHxHEIGHT")
	cmd.Flags().StringVar(&flags.preset, "preset", "default", "render preset: default, ligand, polymer, surface, confidence, overview")
	cmd.Flags().StringArrayVar(&flags.selectors, "select", nil, "component selector; repeat to add components")
	cmd.Flags().StringArrayVar(&flags.representations, "repr", nil, "representation for matching --select")
	cmd.Flags().StringArrayVar(&flags.colors, "color", nil, "color or high-level theme for matching --select")
	cmd.Flags().StringVar(&flags.rendererCommand, "renderer-command", "", "renderer command override; defaults to MOLSTAR_RENDER, PATH, or local node_modules/.bin/mvs-render")
	cmd.Flags().StringVar(&flags.rendererMode, "renderer-mode", "subprocess", "renderer mode: subprocess, worker, or auto")
	cmd.Flags().StringVar(&flags.workerCommand, "worker-command", "", "persistent renderer worker command override")
	cmd.Flags().BoolVar(&flags.dryRun, "dry-run", false, "print external renderer commands without running them")
	cmd.Flags().BoolVar(&flags.quiet, "quiet", false, "suppress renderer progress logs")
	cmd.Flags().BoolVar(&flags.verbose, "verbose", false, "show renderer progress logs even with --json")
	cmd.Flags().BoolVar(&flags.demo, "demo", false, "render a tiny built-in local fixture without network")
	cmd.Flags().BoolVar(&flags.jsonReport, "json", false, "write a machine-readable report to stdout")
	cmd.Flags().BoolVar(&flags.compact, "compact", false, "omit replay-heavy fields from JSON stdout")
	cmd.Flags().BoolVar(&flags.diagnose, "diagnose", false, "force JSON and include renderer capability diagnostics")
	cmd.Flags().StringVar(&flags.reportOut, "report", "", "write a machine-readable render report to this path")
	cmd.Flags().BoolVar(&flags.showReport, "show-report", false, "print a compact human-readable render summary")
	cmd.Flags().BoolVar(&flags.openOutput, "open", false, "open the first rendered output with the system viewer")
	cmd.Flags().BoolVar(&flags.explain, "explain", false, "explain the resolved render job without rendering")
	cmd.Flags().StringVar(&flags.runLabel, "run-label", "", "label stored with the local run log")
	cmd.Flags().BoolVar(&flags.noLog, "no-log", false, "do not write a local .molstar-runs report")
	cmd.Flags().BoolVar(&flags.logAssets, "log-assets", true, "embed local input bytes in run logs for replay")
	cmd.Flags().Int64Var(&flags.logAssetMaxBytes, "log-asset-max-bytes", defaultRunLogMaxSingleInputBytes, "maximum bytes per embedded run-log asset")
	cmd.Flags().Int64Var(&flags.logAssetsMaxBytes, "log-assets-max-bytes", defaultRunLogMaxInputBytes, "maximum total embedded run-log asset bytes")
	cmd.Flags().StringVar(&flags.ciArtifact, "ci-artifact", "", "directory for diagnostic artifacts when rendering fails")
	bindRuntimeFlags(cmd, &flags.runtime)
	return cmd
}

func (a app) runRender(ctx context.Context, input string, flags *renderFlags, cmd *cobra.Command) (err error) {
	report := renderReport{OK: true, Input: input, RunLabel: strings.TrimSpace(flags.runLabel)}
	// Writes the run log at most once and stamps run_log/run_id onto the report,
	// so every consumer of the report (stdout, --report file, summary) sees them.
	ensureRunLog := func() {
		if report.RunLog != "" {
			return
		}
		if path, logErr := maybeWriteRunLogWithOptions("render", report, !flags.noLog, runLogOptionsFromRenderFlags(flags)); logErr == nil {
			report.RunLog = path
			report.RunID = runLogIDFromPath(path)
		}
	}
	defer func() {
		if err == nil {
			return
		}
		report.OK = false
		if !flags.noLog && report.RunLog == "" {
			if path, logErr := writeRunLogWithOptions("render", report, runLogOptionsFromRenderFlags(flags)); logErr == nil {
				report.RunLog = path
				report.RunID = runLogIDFromPath(path)
			}
		}
		if strings.TrimSpace(flags.ciArtifact) != "" {
			_ = a.writeCIArtifact(ctx, flags.ciArtifact, input, report, err)
		}
	}()
	runner := render.NewMolstar()
	if flags.jsonReport {
		runner.Stdout = a.stderr
	} else {
		runner.Stdout = a.stdout
	}
	runner.Stderr = a.stderr
	runner.DryRun = flags.dryRun
	runner.Quiet = flags.quiet || (flags.jsonReport && !flags.verbose)
	if flags.dryRun && !shouldPrintRenderDryRunNotes(flags) {
		runner.Stderr = nil
	}
	if flags.rendererCommand != "" {
		runner.RendererCommand = strings.Fields(flags.rendererCommand)
	}
	if flags.quiet && flags.verbose {
		return markError(kindInvalidInput, fmt.Errorf("--quiet and --verbose cannot be used together"))
	}
	if flags.explain {
		return a.explainRender(ctx, input, flags, cmd)
	}
	if flags.diagnose {
		report.Diagnostics = map[string]any{
			"mode":         "subprocess",
			"capabilities": runner.Capabilities(ctx),
		}
	}

	var scenePath string
	var cleanup func()
	var outputs []job.Output
	stateOut := flags.stateOut
	runtime := runtimeFromFlags(cmd, job.Runtime{}, flags.runtime)
	if flags.demo {
		j, demoCleanup, err := demoJob(flags)
		if err != nil {
			return markError(kindInvalidInput, err)
		}
		defer demoCleanup()
		applyRuntimeFlags(cmd, &j, flags.runtime)
		runtime = j.Runtime
		stageStart := time.Now()
		j, runtimeReport, err := prepareJob(ctx, j)
		report.finishStage("prepare_runtime", "demo", stageStart, err)
		if err != nil {
			return markPrepareError(err)
		}
		report.CachedInputs = runtimeReport.CachedInputs
		report.Warnings = append(report.Warnings, runtimeReport.Warnings...)
		scenePath, cleanup, outputs, stateOut, err = a.compileJobForRender(j, flags, stateOut, &report)
		if err != nil {
			return err
		}
	} else if input == "-" {
		data, name, err := a.readInput(input)
		if err != nil {
			return markError(kindInvalidInput, err)
		}
		if mvs.IsDocumentBytes(data) {
			scenePath, cleanup, err = writeTempMVS(data)
			if err != nil {
				return markError(kindInvalidInput, err)
			}
			if doc, decodeErr := mvs.Decode(data); decodeErr == nil {
				report.MVSDocument = &doc
			}
			outputs, err = outputsForMVSInput(name, flags)
			if err != nil {
				return markError(kindInvalidInput, err)
			}
		} else {
			j, err := a.loadJobOrRecipeBytes(data, name, true)
			if err != nil {
				return markError(kindInvalidInput, err)
			}
			applyRuntimeFlags(cmd, &j, flags.runtime)
			runtime = j.Runtime
			stageStart := time.Now()
			j, runtimeReport, err := prepareJob(ctx, j)
			report.finishStage("prepare_runtime", name, stageStart, err)
			if err != nil {
				return markPrepareError(err)
			}
			report.CachedInputs = runtimeReport.CachedInputs
			report.Warnings = append(report.Warnings, runtimeReport.Warnings...)
			scenePath, cleanup, outputs, stateOut, err = a.compileJobForRender(j, flags, stateOut, &report)
			if err != nil {
				return err
			}
		}
	} else if isMVSPath(input) {
		scenePath = input
		if data, _, readErr := a.readInput(input); readErr == nil {
			if doc, decodeErr := mvs.Decode(data); decodeErr == nil {
				report.MVSDocument = &doc
			}
		}
		var err error
		outputs, err = outputsForMVSInput(input, flags)
		if err != nil {
			return markError(kindInvalidInput, err)
		}
	} else {
		j, err := a.loadOrBuildJob(input, flags)
		if err != nil {
			return markError(kindInvalidInput, err)
		}
		applyRuntimeFlags(cmd, &j, flags.runtime)
		runtime = j.Runtime
		stageStart := time.Now()
		j, runtimeReport, err := prepareJob(ctx, j)
		report.finishStage("prepare_runtime", input, stageStart, err)
		if err != nil {
			return markPrepareError(err)
		}
		report.CachedInputs = runtimeReport.CachedInputs
		report.Warnings = append(report.Warnings, runtimeReport.Warnings...)
		scenePath, cleanup, outputs, stateOut, err = a.compileJobForRender(j, flags, stateOut, &report)
		if err != nil {
			return err
		}
	}
	if cleanup != nil {
		defer cleanup()
	}
	if len(outputs) == 0 {
		return markError(kindInvalidInput, fmt.Errorf("no image or video outputs configured"))
	}
	if err := (job.Job{Runtime: runtime, Outputs: outputs}).ValidateRuntimeLimits(); err != nil {
		return markError(kindRuntime, err)
	}
	ctx, cancel := contextWithRuntimeTimeout(ctx, runtime)
	defer cancel()
	workerStderr := a.stderr
	if runner.Quiet {
		workerStderr = nil
	}
	selection, err := selectWorkerRenderer(flags.rendererMode, flags.workerCommand, 1, runner, workerStderr, flags.dryRun)
	if err != nil {
		return markError(kindRuntime, err)
	}
	if selection.Close != nil {
		defer selection.Close()
	}
	renderer := render.ImageRenderer(runner)
	if selection.Pool != nil {
		renderer = selection.Pool
	}
	if report.Diagnostics == nil {
		report.Diagnostics = map[string]any{}
	}
	report.Diagnostics["renderer_mode"] = selection.Mode
	if selection.FallbackError != nil {
		report.Diagnostics["worker_fallback"] = selection.FallbackError.Error()
		report.Warnings = append(report.Warnings, "worker fallback: "+selection.FallbackError.Error())
	}
	for i, output := range outputs {
		saveMolj := stateOut != "" && i == 0
		stageStart := time.Now()
		result, outputReports, err := renderTransactional(ctx, renderer, scenePath, output, saveMolj, stateOut, flags.dryRun)
		report.finishStage("render_output", output.Path, stageStart, err)
		report.Commands = append(report.Commands, result)
		if err != nil {
			return markError(kindRender, err)
		}
		for _, outputReport := range outputReports {
			report.Outputs = append(report.Outputs, outputReport.Path)
			report.OutputFiles = append(report.OutputFiles, outputReport)
		}
	}
	if flags.reportOut != "" {
		// Write the run log before serializing so the report file carries
		// run_log/run_id. The documented agent loop reads RUN_ID out of --report
		// to drive `logs export` and `diagnose`.
		ensureRunLog()
		data, err := marshalJSON(report)
		if err != nil {
			return markError(kindInternal, err)
		}
		stageStart := time.Now()
		outputReport, err := writeReportTransactional(flags.reportOut, data)
		report.finishStage("write_report", flags.reportOut, stageStart, err)
		if err != nil {
			return markError(kindRender, err)
		}
		report.Outputs = append(report.Outputs, outputReport.Path)
		report.OutputFiles = append(report.OutputFiles, outputReport)
	}
	if flags.openOutput {
		if flags.dryRun {
			if shouldPrintRenderDryRunNotes(flags) {
				fmt.Fprintln(a.stderr, "dry-run: would open first rendered output")
			}
		} else if err := openFirstRenderableOutput(report.OutputFiles); err != nil {
			return markError(kindRuntime, err)
		}
	}
	if flags.jsonReport {
		ensureRunLog()
		stdoutReport := report
		if flags.compact {
			compactRenderReport(&stdoutReport)
		}
		return writeJSON(a.stdout, stdoutReport)
	}
	ensureRunLog()
	if flags.showReport {
		return a.writeRenderSummary(report)
	}
	return nil
}

func (a app) explainRender(ctx context.Context, input string, flags *renderFlags, cmd *cobra.Command) error {
	report, err := a.buildRenderExplainReport(ctx, input, flags, cmd)
	if err != nil {
		return err
	}
	if flags.jsonReport {
		return writeJSON(a.stdout, report)
	}
	return a.writeRenderExplainSummary(report)
}

func (a app) buildRenderExplainReport(ctx context.Context, input string, flags *renderFlags, cmd *cobra.Command) (renderExplainReport, error) {
	report := renderExplainReport{OK: true, Input: input, RendererMode: flags.rendererMode}
	if flags.demo {
		j, cleanup, err := demoJob(flags)
		if err != nil {
			return report, markError(kindInvalidInput, err)
		}
		defer cleanup()
		applyRuntimeFlags(cmd, &j, flags.runtime)
		return explainPreparedJob(ctx, "demo", j, report)
	}
	if input == "-" {
		data, name, err := a.readInput(input)
		if err != nil {
			return report, markError(kindInvalidInput, err)
		}
		if mvs.IsDocumentBytes(data) {
			doc, err := mvs.Decode(data)
			if err != nil {
				return report, markError(kindInvalidScene, err)
			}
			report.Kind = "mvs"
			report.MVSNodeCount = countMVSNodes(doc.Root)
			return report, nil
		}
		j, err := a.loadJobOrRecipeBytes(data, name, true)
		if err != nil {
			return report, markError(kindInvalidInput, err)
		}
		applyRuntimeFlags(cmd, &j, flags.runtime)
		return explainPreparedJob(ctx, "job", j, report)
	}
	if isMVSPath(input) {
		data, _, err := a.readInput(input)
		if err != nil {
			return report, markError(kindInvalidInput, err)
		}
		doc, err := mvs.Decode(data)
		if err != nil {
			return report, markError(kindInvalidScene, err)
		}
		outputs, err := outputsForMVSInput(input, flags)
		if err != nil {
			return report, markError(kindInvalidInput, err)
		}
		report.Kind = "mvs"
		report.Outputs = outputs
		report.MVSNodeCount = countMVSNodes(doc.Root)
		return report, nil
	}
	j, err := a.loadOrBuildJob(input, flags)
	if err != nil {
		return report, markError(kindInvalidInput, err)
	}
	applyRuntimeFlags(cmd, &j, flags.runtime)
	return explainPreparedJob(ctx, "job", j, report)
}

func explainPreparedJob(ctx context.Context, kind string, j job.Job, report renderExplainReport) (renderExplainReport, error) {
	prepared, runtimeReport, err := prepareJob(ctx, j)
	if err != nil {
		return report, markPrepareError(err)
	}
	compiled, err := mvs.Compile(prepared)
	if err != nil {
		return report, markError(kindInvalidScene, err)
	}
	report.Kind = kind
	report.Runtime = prepared.Runtime
	report.Inputs = prepared.Inputs
	report.Outputs = prepared.Outputs
	report.CachedInputs = runtimeReport.CachedInputs
	report.Warnings = append(report.Warnings, runtimeReport.Warnings...)
	report.Warnings = append(report.Warnings, compiled.Warnings...)
	report.MVSNodeCount = countMVSNodes(compiled.Document.Root)
	if len(prepared.Scene.Structures) > 0 {
		report.Components = prepared.Scene.Structures[0].Components
	}
	report.Camera = prepared.Scene.Camera
	report.Canvas = prepared.Scene.Canvas
	if network := prepared.Runtime.Network; network != nil && !*network {
		report.NetworkDisabled = true
	}
	for _, input := range prepared.Inputs {
		report.Provider = input.Provider
		report.Format = input.ResolvedFormat()
		break
	}
	return report, nil
}

func countMVSNodes(node mvs.Node) int {
	total := 1
	for _, child := range node.Children {
		total += countMVSNodes(child)
	}
	return total
}

func (a app) writeRenderExplainSummary(report renderExplainReport) error {
	fmt.Fprintf(a.stdout, "render plan\t%s\n", report.Kind)
	fmt.Fprintf(a.stdout, "input\t%s\n", report.Input)
	if report.Provider != "" {
		fmt.Fprintf(a.stdout, "provider\t%s\n", report.Provider)
	}
	if report.Format != "" {
		fmt.Fprintf(a.stdout, "format\t%s\n", report.Format)
	}
	fmt.Fprintf(a.stdout, "renderer\t%s\n", report.RendererMode)
	if report.Runtime.Cache != "" {
		fmt.Fprintf(a.stdout, "cache\t%s\n", report.Runtime.Cache)
	}
	if report.NetworkDisabled {
		fmt.Fprintln(a.stdout, "network\tdisabled")
	}
	for _, component := range report.Components {
		fmt.Fprintf(a.stdout, "component\t%s\t%s\t%s", component.Ref, component.Select, component.Representation.Type)
		if component.Representation.Color != "" {
			fmt.Fprintf(a.stdout, "\t%s", component.Representation.Color)
		}
		fmt.Fprintln(a.stdout)
	}
	for _, output := range report.Outputs {
		fmt.Fprintf(a.stdout, "output\t%s\t%s\n", output.NormalizedType(), output.Path)
	}
	fmt.Fprintf(a.stdout, "mvs nodes\t%d\n", report.MVSNodeCount)
	return nil
}

func (a app) writeRenderSummary(report renderReport) error {
	fmt.Fprintf(a.stdout, "render\t%s\n", renderStatusWord(report.OK))
	if report.Input != "" {
		fmt.Fprintf(a.stdout, "input\t%s\n", report.Input)
	}
	if report.MVS != "" {
		fmt.Fprintf(a.stdout, "mvs\t%s\n", report.MVS)
	}
	for _, output := range report.OutputFiles {
		details := []string{}
		if output.Type != "" {
			details = append(details, output.Type)
		}
		if output.Bytes > 0 {
			details = append(details, fmt.Sprintf("%d bytes", output.Bytes))
		}
		if output.Width > 0 && output.Height > 0 {
			details = append(details, fmt.Sprintf("%dx%d", output.Width, output.Height))
		}
		if len(details) == 0 {
			fmt.Fprintf(a.stdout, "output\t%s\n", output.Path)
		} else {
			fmt.Fprintf(a.stdout, "output\t%s\t%s\n", output.Path, strings.Join(details, ", "))
		}
	}
	for _, warning := range report.Warnings {
		fmt.Fprintf(a.stdout, "warning\t%s\n", warning)
	}
	return nil
}

func renderStatusWord(ok bool) string {
	if ok {
		return "ok"
	}
	return "failed"
}

func shouldPrintRenderDryRunNotes(flags *renderFlags) bool {
	return flags.dryRun && !flags.quiet && (!flags.jsonReport || flags.verbose)
}

func compactRenderReport(report *renderReport) {
	report.Job = nil
	report.MVSDocument = nil
	for i := range report.Commands {
		report.Commands[i].Stdout = ""
		report.Commands[i].Stderr = ""
	}
}

func openFirstRenderableOutput(outputs []outputReport) error {
	for _, output := range outputs {
		if output.Path == "" {
			continue
		}
		switch strings.ToLower(output.Type) {
		case "image", "video":
			return openPath(output.Path)
		case "":
			ext := strings.ToLower(filepath.Ext(output.Path))
			if ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".mp4" || ext == ".webm" {
				return openPath(output.Path)
			}
		}
	}
	return fmt.Errorf("no image or video output available to open")
}

func openPath(path string) error {
	var command string
	var args []string
	switch goruntime.GOOS {
	case "darwin":
		command = "open"
		args = []string{path}
	case "windows":
		command = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", path}
	default:
		command = "xdg-open"
		args = []string{path}
	}
	return exec.Command(command, args...).Start()
}

func (a app) compileJobForRender(j job.Job, flags *renderFlags, stateOut string, report *renderReport) (string, func(), []job.Output, string, error) {
	jobCopy := j
	report.Job = &jobCopy
	stageStart := time.Now()
	compiled, err := mvs.Compile(j)
	report.finishStage("compile_mvs", "", stageStart, err)
	if err != nil {
		return "", nil, nil, "", markError(kindInvalidScene, err)
	}
	documentCopy := compiled.Document
	report.MVSDocument = &documentCopy
	report.Warnings = append(report.Warnings, compiled.Warnings...)
	report.Themes = append(report.Themes, compiled.ThemeExtensions...)
	for _, output := range j.Outputs {
		switch output.NormalizedType() {
		case "mvsj":
			if flags.dryRun {
				if shouldPrintRenderDryRunNotes(flags) {
					fmt.Fprintln(a.stderr, "dry-run: would write", output.Path)
				}
			} else {
				stageStart := time.Now()
				outputReport, err := writeMVSJTransactional(output.Path, compiled.Document)
				report.finishStage("write_mvsj", output.Path, stageStart, err)
				if err != nil {
					return "", nil, nil, "", markError(kindRender, err)
				}
				report.OutputFiles = append(report.OutputFiles, outputReport)
			}
			report.Outputs = append(report.Outputs, output.Path)
		case "mvsx":
			if flags.dryRun {
				if shouldPrintRenderDryRunNotes(flags) {
					fmt.Fprintln(a.stderr, "dry-run: would write", output.Path)
				}
			} else {
				stageStart := time.Now()
				outputReport, err := writeMVSXTransactional(output.Path, j, compiled.Document)
				report.finishStage("write_mvsx", output.Path, stageStart, err)
				if err != nil {
					return "", nil, nil, "", markError(kindRender, err)
				}
				report.OutputFiles = append(report.OutputFiles, outputReport)
			}
			report.Outputs = append(report.Outputs, output.Path)
		case "molj":
			if stateOut == "" {
				stateOut = output.Path
			}
		case "image", "video":
		default:
			return "", nil, nil, "", markError(kindInvalidInput, fmt.Errorf("outputs: unsupported output type %q", output.Type))
		}
	}

	var scenePath string
	var cleanup func()
	if flags.mvsOut != "" && !flags.dryRun {
		stageStart := time.Now()
		outputReport, err := writeMVSJTransactional(flags.mvsOut, compiled.Document)
		report.finishStage("write_mvsj", flags.mvsOut, stageStart, err)
		if err != nil {
			return "", nil, nil, "", markError(kindRender, err)
		}
		scenePath = flags.mvsOut
		report.MVS = flags.mvsOut
		report.Outputs = append(report.Outputs, flags.mvsOut)
		report.OutputFiles = append(report.OutputFiles, outputReport)
	} else {
		if flags.mvsOut != "" {
			if shouldPrintRenderDryRunNotes(flags) {
				fmt.Fprintln(a.stderr, "dry-run: would write", flags.mvsOut)
			}
			report.Outputs = append(report.Outputs, flags.mvsOut)
		}
		data, err := mvs.Marshal(compiled.Document)
		if err != nil {
			return "", nil, nil, "", markError(kindInvalidScene, err)
		}
		stageStart := time.Now()
		scenePath, cleanup, err = writeTempMVS(data)
		report.finishStage("write_temp_mvs", "", stageStart, err)
		if err != nil {
			return "", nil, nil, "", markError(kindInvalidScene, err)
		}
		report.MVS = scenePath
	}
	for _, warning := range compiled.Warnings {
		fmt.Fprintln(a.stderr, "warning:", warning)
	}
	return scenePath, cleanup, imageOutputsFromJob(j), stateOut, nil
}

func (a app) loadOrBuildJob(input string, flags *renderFlags) (job.Job, error) {
	if job.PathExists(input) && !looksLikeStructureInput(input) {
		data, _, err := a.readInput(input)
		if err != nil {
			return job.Job{}, err
		}
		j, err := a.loadJobOrRecipeBytes(data, input, true)
		if err != nil {
			return job.Job{}, err
		}
		if flags.out != "" {
			// --out redirects the output path. Preserve the job/recipe's declared
			// image size unless the user explicitly passed --size; otherwise a
			// recipe that renders at 1200x900 would be silently downsized to the
			// --size default.
			size := mustParseSize(flags.size)
			if !flags.sizeExplicit {
				if declared := firstOutputSize(j.Outputs); len(declared) == 2 {
					size = declared
				}
			}
			j.Outputs = []job.Output{{Type: outputTypeFromPath(flags.out), Path: flags.out, Size: size}}
		}
		return j, nil
	}
	isURL := strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://")
	if !isURL && !job.PathExists(input) && looksLikeLocalPath(input) {
		return job.Job{}, fmt.Errorf("no such file: %s", input)
	}
	widthHeight, err := parseSize(flags.size)
	if err != nil {
		return job.Job{}, err
	}
	components, focus, err := componentsForPreset(flags)
	if err != nil {
		return job.Job{}, err
	}
	if len(components) == 0 {
		components = []job.Component{
			{Ref: "polymer", Select: "polymer", Representation: job.Representation{Type: "cartoon", Color: "chain"}},
			{Ref: "ligand", Select: "ligand", Representation: job.Representation{Type: "ball-and-stick"}},
		}
	}
	if flags.focus != "" {
		focus = flags.focus
	}
	source := job.Input{ID: input, Provider: flags.provider, Format: flags.format, Assembly: flags.assembly}
	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		source = job.Input{URL: input, Format: flags.format, Assembly: flags.assembly}
	} else if job.PathExists(input) || looksLikeStructureInput(input) {
		source = job.Input{Path: input, Format: flags.format, Assembly: flags.assembly}
	}
	outputPath := flags.out
	if outputPath == "" {
		outputPath = defaultOutputPathForPreset(input, flags.preset)
	}
	return job.Job{
		Version: 1,
		Runtime: job.Runtime{Strict: true},
		Inputs: map[string]job.Input{
			"input": source,
		},
		Scene: job.Scene{
			Canvas: job.Canvas{Background: flags.background},
			Structures: []job.Structure{{
				Ref:        "structure",
				Source:     "input",
				Assembly:   flags.assembly,
				Components: components,
			}},
			Camera: job.Camera{
				Focus: focus,
				View:  flags.view,
			},
		},
		Outputs: []job.Output{{
			Type: outputTypeFromPath(outputPath),
			Path: outputPath,
			Size: widthHeight,
		}},
	}, nil
}

func demoJob(flags *renderFlags) (job.Job, func(), error) {
	widthHeight, err := parseSize(flags.size)
	if err != nil {
		return job.Job{}, nil, err
	}
	dir, err := os.MkdirTemp("", "headlessmolstar-demo-*")
	if err != nil {
		return job.Job{}, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	cifPath := filepath.Join(dir, "demo.cif")
	if flags != nil && strings.TrimSpace(flags.mvsOut) != "" && !flags.dryRun {
		scenePath := strings.TrimSpace(flags.mvsOut)
		assetDir := filepath.Join(filepath.Dir(scenePath), strings.TrimSuffix(filepath.Base(scenePath), filepath.Ext(scenePath))+"-assets")
		if err := os.MkdirAll(assetDir, 0o755); err != nil {
			cleanup()
			return job.Job{}, nil, err
		}
		cifPath = filepath.Join(assetDir, "demo.cif")
		cleanup = func() {}
	}
	if err := os.WriteFile(cifPath, []byte(oneAtomCIF), 0o644); err != nil {
		cleanup()
		return job.Job{}, nil, err
	}
	outputPath := flags.out
	if outputPath == "" {
		outputPath = "molstar-demo.png"
	}
	background := flags.background
	if background == "" {
		background = "white"
	}
	focus := flags.focus
	if focus == "" {
		focus = "all"
	}
	return job.Job{
		Version: 1,
		Runtime: job.Runtime{Strict: true},
		Inputs: map[string]job.Input{
			"demo": {Path: cifPath, Format: "mmcif"},
		},
		Scene: job.Scene{
			Canvas: job.Canvas{Background: background},
			Structures: []job.Structure{{
				Ref:    "demo",
				Source: "demo",
				Components: []job.Component{{
					Ref:    "all",
					Select: "all",
					Representation: job.Representation{
						Type:  "spacefill",
						Color: "#cc3399",
					},
				}},
			}},
			Camera: job.Camera{
				Focus: focus,
				View:  flags.view,
			},
		},
		Outputs: []job.Output{{
			Type: outputTypeFromPath(outputPath),
			Path: outputPath,
			Size: widthHeight,
		}},
	}, cleanup, nil
}

func prepareJob(ctx context.Context, j job.Job) (job.Job, job.RuntimeReport, error) {
	ctx, cancel := contextWithRuntimeTimeout(ctx, j.Runtime)
	defer cancel()
	return job.PrepareRuntime(ctx, j)
}

func writeMVSXForJob(path string, j job.Job, document mvs.Document) error {
	assets := make([]mvs.Asset, 0, len(j.Inputs))
	replacements := map[string]string{}
	runtime := job.ApplyRuntimeProfile(j.Runtime)
	var totalBytes int64
	for ref, input := range j.Inputs {
		local := input.LocalPath()
		if local == "" {
			return fmt.Errorf("mvsx output requires local or cached input for %q; set runtime.cache for remote inputs", ref)
		}
		resolved, err := input.ResolvedURL()
		if err != nil {
			return err
		}
		ext := filepath.Ext(local)
		if ext == "" {
			ext = "." + input.ResolvedFormat()
		}
		assetName := filepath.ToSlash(filepath.Join("assets", sanitizeRef(ref)+ext))
		info, err := os.Stat(local)
		if err != nil {
			return err
		}
		totalBytes += info.Size()
		assets = append(assets, mvs.Asset{Name: assetName, Path: local})
		replacements[resolved] = assetName
	}
	for _, asset := range j.Assets {
		name, err := mvs.NormalizeAssetName(asset.Name)
		if err != nil {
			return err
		}
		if err := enforceArchiveAssetPathPolicy(asset.Path, runtime); err != nil {
			return err
		}
		info, err := os.Stat(asset.Path)
		if err != nil {
			return err
		}
		totalBytes += info.Size()
		assets = append(assets, mvs.Asset{Name: name, Path: asset.Path})
	}
	if runtime.MaxArchiveBytes > 0 && totalBytes > runtime.MaxArchiveBytes {
		return fmt.Errorf("mvsx assets total %d bytes, exceeding runtime.max_archive_bytes=%d", totalBytes, runtime.MaxArchiveBytes)
	}
	document = mvs.ReplaceDownloadURLs(document, replacements)
	return mvs.WriteMVSX(path, document, assets)
}

func enforceArchiveAssetPathPolicy(path string, runtime job.Runtime) error {
	if len(runtime.AllowPaths) == 0 {
		return nil
	}
	return job.EnforceLocalPathPolicy(job.Input{Path: path}, runtime)
}

func writeTempMVS(data []byte) (string, func(), error) {
	tmp, err := os.CreateTemp("", "headlessmolstar-*.mvsj")
	if err != nil {
		return "", nil, err
	}
	scenePath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(scenePath)
		return "", nil, err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(scenePath)
		return "", nil, err
	}
	return scenePath, func() { _ = os.Remove(scenePath) }, nil
}

func buildComponents(flags *renderFlags) []job.Component {
	var components []job.Component
	for i, selector := range flags.selectors {
		repr := valueAt(flags.representations, i, "cartoon")
		color := valueAt(flags.colors, i, "")
		ref := sanitizeRef(selector)
		components = append(components, job.Component{
			Ref:    ref,
			Select: selector,
			Representation: job.Representation{
				Type:  repr,
				Color: color,
			},
		})
	}
	return components
}

func componentsForPreset(flags *renderFlags) ([]job.Component, string, error) {
	if len(flags.selectors) > 0 {
		return buildComponents(flags), flags.focus, nil
	}
	preset, ok := presetDefinitionByName(flags.preset)
	if !ok {
		return nil, "", fmt.Errorf("unsupported preset %q", flags.preset)
	}
	return preset.Components, preset.Focus, nil
}

func outputsForMVSInput(input string, flags *renderFlags) ([]job.Output, error) {
	widthHeight, err := parseSize(flags.size)
	if err != nil {
		return nil, err
	}
	outputPath := flags.out
	if outputPath == "" {
		outputPath = defaultOutputPath(input)
	}
	return []job.Output{{Type: outputTypeFromPath(outputPath), Path: outputPath, Size: widthHeight}}, nil
}

func imageOutputsFromJob(j job.Job) []job.Output {
	var outputs []job.Output
	for _, output := range j.Outputs {
		switch output.NormalizedType() {
		case "image", "video":
			outputs = append(outputs, output)
		}
	}
	return outputs
}

func moveState(imagePath, statePath string, dryRun bool) error {
	if statePath == "" || dryRun {
		return nil
	}
	defaultPath := replaceExt(imagePath, ".molj")
	if defaultPath == statePath {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		return err
	}
	return os.Rename(defaultPath, statePath)
}

func isMVSPath(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".mvsj" || ext == ".mvsx"
}

// firstOutputSize returns the WIDTHxHEIGHT of the first output that declares
// one, or nil. It lets --out redirect the path without discarding a recipe or
// job's configured image size.
func firstOutputSize(outputs []job.Output) []int {
	for _, o := range outputs {
		if len(o.Size) == 2 {
			return o.Size
		}
	}
	return nil
}

func looksLikeStructureInput(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".cif", ".bcif", ".pdb", ".pdbqt", ".gro", ".xyz", ".mol", ".sdf", ".mol2":
		return true
	default:
		return false
	}
}

// looksLikeLocalPath reports whether input was clearly meant as a local file
// (it has a path separator or a job/scene/structure file extension) rather
// than a network identifier such as "1cbs". It lets render fail fast with a
// clear "no such file" error instead of fetching a missing file path as an
// identifier and surfacing an opaque downstream 404.
func looksLikeLocalPath(input string) bool {
	if strings.ContainsAny(input, "/\\") {
		return true
	}
	switch strings.ToLower(filepath.Ext(input)) {
	case ".yaml", ".yml", ".json", ".mvsj", ".mvsx",
		".cif", ".bcif", ".pdb", ".pdbqt", ".gro", ".xyz", ".mol", ".sdf", ".mol2":
		return true
	default:
		return false
	}
}

func parseSize(value string) ([]int, error) {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(value)), "x")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid size %q, expected WIDTHxHEIGHT", value)
	}
	var width, height int
	if _, err := fmt.Sscanf(parts[0], "%d", &width); err != nil {
		return nil, fmt.Errorf("invalid width in size %q", value)
	}
	if _, err := fmt.Sscanf(parts[1], "%d", &height); err != nil {
		return nil, fmt.Errorf("invalid height in size %q", value)
	}
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("invalid size %q, dimensions must be positive", value)
	}
	return []int{width, height}, nil
}

func mustParseSize(value string) []int {
	size, err := parseSize(value)
	if err != nil {
		return nil
	}
	return size
}

func outputTypeFromPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp4":
		return "video"
	case ".mvsj":
		return "mvsj"
	case ".mvsx":
		return "mvsx"
	case ".molj":
		return "molj"
	default:
		return "image"
	}
}

func defaultOutputPath(input string) string {
	base := filepath.Base(input)
	ext := filepath.Ext(base)
	if ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	if base == "" || base == "." || base == string(filepath.Separator) {
		base = "scene"
	}
	return base + ".png"
}

func defaultOutputPathForPreset(input string, preset string) string {
	path := defaultOutputPath(input)
	normalized := strings.ToLower(strings.TrimSpace(preset))
	if normalized == "" || normalized == "default" {
		return path
	}
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	return base + "-" + sanitizeRef(normalized) + ext
}

func replaceExt(path string, ext string) string {
	old := filepath.Ext(path)
	return strings.TrimSuffix(path, old) + ext
}

func valueAt(values []string, index int, fallback string) string {
	if index < len(values) && values[index] != "" {
		return values[index]
	}
	if len(values) == 1 && values[0] != "" {
		return values[0]
	}
	return fallback
}

func sanitizeRef(value string) string {
	ref := strings.ToLower(strings.TrimSpace(value))
	ref = strings.ReplaceAll(ref, " ", "_")
	ref = strings.ReplaceAll(ref, "-", "_")
	if ref == "" {
		return "component"
	}
	return ref
}
