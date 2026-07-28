package cli

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/rafflesia-ai/molstar/internal/job"
	"github.com/rafflesia-ai/molstar/internal/mvs"
	"github.com/rafflesia-ai/molstar/internal/render"
)

type runLogEnvelope struct {
	OK        bool          `json:"ok"`
	ID        string        `json:"id"`
	Command   string        `json:"command"`
	Label     string        `json:"label,omitempty"`
	CreatedAt string        `json:"created_at"`
	Report    renderReport  `json:"report"`
	Assets    []runLogAsset `json:"assets,omitempty"`
	Replay    runLogReplay  `json:"replay"`
}

type runLogAsset struct {
	Ref    string `json:"ref"`
	Name   string `json:"name"`
	Format string `json:"format,omitempty"`
	Data   []byte `json:"data"`
}

type runLogReplay struct {
	Replayable         bool     `json:"replayable"`
	FullyReplayable    bool     `json:"fully_replayable"`
	IncludesInputs     bool     `json:"includes_inputs"`
	EmbeddedInputBytes int64    `json:"embedded_input_bytes,omitempty"`
	EmbeddedInputs     []string `json:"embedded_inputs,omitempty"`
	ExternalInputs     []string `json:"external_inputs,omitempty"`
	MissingInputs      []string `json:"missing_inputs,omitempty"`
	Warnings           []string `json:"warnings,omitempty"`
}

type runLogAssetPolicy struct {
	IncludeInputs       bool
	MaxInputBytes       int64
	MaxSingleInputBytes int64
}

type runLogSummary struct {
	ID              string `json:"id"`
	OK              bool   `json:"ok"`
	Command         string `json:"command"`
	Label           string `json:"label,omitempty"`
	Input           string `json:"input,omitempty"`
	FirstOutput     string `json:"first_output,omitempty"`
	DurationMS      int64  `json:"duration_ms,omitempty"`
	Replayable      bool   `json:"replayable"`
	FullyReplayable bool   `json:"fully_replayable"`
	CreatedAt       string `json:"created_at"`
	Path            string `json:"path"`
}

const (
	defaultRunLogMaxSingleInputBytes int64 = 10 << 20
	defaultRunLogMaxInputBytes       int64 = 50 << 20
)

func (a app) logsCommand() *cobra.Command {
	var last bool
	var dir string
	var jsonReport bool
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Inspect local Mol* CLI run history",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("logs", jsonReport, func() error {
				if len(args) != 0 {
					return markError(kindInvalidInput, fmt.Errorf("logs does not accept positional arguments"))
				}
				if !last {
					return markError(kindInvalidInput, fmt.Errorf("logs expects a subcommand or --last"))
				}
				entry, path, err := readLastRunLog(dir)
				if err != nil {
					return markError(kindInvalidInput, err)
				}
				if jsonReport {
					return writeJSON(a.stdout, map[string]any{"ok": true, "path": path, "run": entry})
				}
				fmt.Fprintf(a.stdout, "run\t%s\n", entry.ID)
				fmt.Fprintf(a.stdout, "created\t%s\n", entry.CreatedAt)
				fmt.Fprintf(a.stdout, "command\t%s\n", entry.Command)
				if entry.Label != "" {
					fmt.Fprintf(a.stdout, "label\t%s\n", entry.Label)
				}
				writeRunLogReplaySummary(a.stdout, entry.Replay)
				fmt.Fprintf(a.stdout, "path\t%s\n", path)
				return a.writeRenderSummary(entry.Report)
			})
		},
	}
	cmd.Flags().BoolVar(&last, "last", false, "show the most recent local run")
	cmd.Flags().StringVar(&dir, "dir", "", "run history directory; defaults to .molstar-runs or MOLSTAR_RUNS_DIR")
	cmd.Flags().BoolVar(&jsonReport, "json", false, "write machine-readable output")
	cmd.AddCommand(a.logsListCommand())
	cmd.AddCommand(a.logsShowCommand())
	cmd.AddCommand(a.logsExportCommand())
	cmd.AddCommand(a.logsImportCommand())
	cmd.AddCommand(a.logsRerunCommand())
	cmd.AddCommand(a.logsVerifyCommand())
	cmd.AddCommand(a.logsPruneCommand())
	return cmd
}

func (a app) logsListCommand() *cobra.Command {
	var dir string
	var failedOnly bool
	var limit int
	var jsonReport bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List recent local runs",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("logs list", jsonReport, func() error {
				if len(args) != 0 {
					return markError(kindInvalidInput, fmt.Errorf("logs list does not accept positional arguments"))
				}
				entries, err := listRunLogs(dir)
				if err != nil {
					return markError(kindInvalidInput, err)
				}
				summaries := make([]runLogSummary, 0, len(entries))
				for _, entry := range entries {
					if failedOnly && entry.Envelope.OK {
						continue
					}
					summaries = append(summaries, summarizeRunLog(entry))
				}
				sort.SliceStable(summaries, func(i, j int) bool {
					return summaries[i].CreatedAt > summaries[j].CreatedAt
				})
				if limit > 0 && len(summaries) > limit {
					summaries = summaries[:limit]
				}
				if jsonReport {
					return writeJSON(a.stdout, map[string]any{"ok": true, "runs": summaries})
				}
				for _, summary := range summaries {
					status := "ok"
					if !summary.OK {
						status = "failed"
					}
					replay := "not-replayable"
					if summary.FullyReplayable {
						replay = "fully-replayable"
					} else if summary.Replayable {
						replay = "replayable"
					}
					fmt.Fprintf(a.stdout, "%s\t%s\t%s\t%s", summary.ID, status, replay, summary.Command)
					if summary.Label != "" {
						fmt.Fprintf(a.stdout, "\t%s", summary.Label)
					}
					if summary.Input != "" {
						fmt.Fprintf(a.stdout, "\t%s", summary.Input)
					}
					if summary.FirstOutput != "" {
						fmt.Fprintf(a.stdout, "\t%s", summary.FirstOutput)
					}
					if summary.DurationMS > 0 {
						fmt.Fprintf(a.stdout, "\t%dms", summary.DurationMS)
					}
					fmt.Fprintf(a.stdout, "\t%s\n", summary.Path)
				}
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "run history directory; defaults to .molstar-runs or MOLSTAR_RUNS_DIR")
	cmd.Flags().BoolVar(&failedOnly, "failed", false, "only list failed runs")
	cmd.Flags().IntVar(&limit, "limit", 20, "maximum number of runs to list; 0 means all")
	cmd.Flags().BoolVar(&jsonReport, "json", false, "write machine-readable output")
	return cmd
}

func (a app) logsShowCommand() *cobra.Command {
	var dir string
	var last bool
	var jsonReport bool
	var openOutput bool
	var rerun bool
	var outDir string
	cmd := &cobra.Command{
		Use:   "show RUN_ID",
		Short: "Show a saved local run report",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("logs show", jsonReport, func() error {
				var entry runLogEntry
				var err error
				if last {
					if len(args) != 0 {
						return markError(kindInvalidInput, fmt.Errorf("logs show --last does not accept RUN_ID"))
					}
					envelope, path, err := readLastRunLog(dir)
					if err != nil {
						return markError(kindInvalidInput, err)
					}
					entry = runLogEntry{Envelope: envelope, Path: path}
				} else {
					if err := exactArgs(args, 1, "logs show"); err != nil {
						return markError(kindInvalidInput, err)
					}
					entry, err = readRunLogByID(dir, args[0])
					if err != nil {
						return markError(kindInvalidInput, err)
					}
				}
				if openOutput {
					if err := openFirstRenderableOutput(entry.Envelope.Report.OutputFiles); err != nil {
						return markError(kindRuntime, err)
					}
				}
				if rerun {
					report, err := a.rerunRunLog(cmd.Context(), entry.Envelope, outDir)
					if err != nil {
						return err
					}
					if jsonReport {
						return writeJSON(a.stdout, map[string]any{"ok": true, "path": entry.Path, "run": entry.Envelope, "rerun": report})
					}
					fmt.Fprintf(a.stdout, "rerun\t%s\n", renderStatusWord(report.OK))
					return a.writeRenderSummary(report)
				}
				if jsonReport {
					return writeJSON(a.stdout, map[string]any{"ok": true, "path": entry.Path, "run": entry.Envelope})
				}
				fmt.Fprintf(a.stdout, "run\t%s\n", entry.Envelope.ID)
				fmt.Fprintf(a.stdout, "created\t%s\n", entry.Envelope.CreatedAt)
				fmt.Fprintf(a.stdout, "command\t%s\n", entry.Envelope.Command)
				if entry.Envelope.Label != "" {
					fmt.Fprintf(a.stdout, "label\t%s\n", entry.Envelope.Label)
				}
				writeRunLogReplaySummary(a.stdout, entry.Envelope.Replay)
				fmt.Fprintf(a.stdout, "path\t%s\n", entry.Path)
				return a.writeRenderSummary(entry.Envelope.Report)
			})
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "run history directory; defaults to .molstar-runs or MOLSTAR_RUNS_DIR")
	cmd.Flags().BoolVar(&last, "last", false, "show the most recent local run")
	cmd.Flags().BoolVar(&openOutput, "open-output", false, "open the first image/video output from the saved report")
	cmd.Flags().BoolVar(&rerun, "rerun", false, "rerun the saved normalized job or MVS scene")
	cmd.Flags().StringVar(&outDir, "out-dir", "", "directory for rerun outputs; defaults to original output paths")
	cmd.Flags().BoolVar(&jsonReport, "json", false, "write machine-readable output")
	return cmd
}

func (a app) logsExportCommand() *cobra.Command {
	var dir string
	var out string
	var includeInputs bool
	var maxInputBytes int64
	var maxSingleInputBytes int64
	var jsonReport bool
	cmd := &cobra.Command{
		Use:   "export RUN_ID",
		Short: "Export a run log bundle",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("logs export", jsonReport, func() error {
				if err := exactArgs(args, 1, "logs export"); err != nil {
					return markError(kindInvalidInput, err)
				}
				entry, err := readRunLogByID(dir, args[0])
				if err != nil {
					return markError(kindInvalidInput, err)
				}
				target := out
				if strings.TrimSpace(target) == "" {
					target = entry.Envelope.ID + ".molrun"
				}
				policy := runLogAssetPolicy{IncludeInputs: includeInputs, MaxInputBytes: maxInputBytes, MaxSingleInputBytes: maxSingleInputBytes}
				envelope := prepareRunLogForExport(entry.Envelope, policy)
				if err := exportRunBundle(target, envelope); err != nil {
					return markError(kindRender, err)
				}
				if jsonReport {
					return writeJSON(a.stdout, map[string]any{"ok": true, "output": target, "id": envelope.ID, "replay": envelope.Replay})
				}
				for _, warning := range envelope.Replay.Warnings {
					fmt.Fprintf(a.stderr, "warning: %s\n", warning)
				}
				fmt.Fprintln(a.stdout, target)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "run history directory; defaults to .molstar-runs or MOLSTAR_RUNS_DIR")
	cmd.Flags().StringVarP(&out, "out", "o", "", "output .molrun bundle")
	cmd.Flags().BoolVar(&includeInputs, "include-inputs", true, "embed local input files in the exported bundle")
	cmd.Flags().Int64Var(&maxInputBytes, "max-input-bytes", defaultRunLogMaxInputBytes, "maximum total local input bytes to embed; 0 disables the limit")
	cmd.Flags().Int64Var(&maxSingleInputBytes, "max-single-input-bytes", defaultRunLogMaxSingleInputBytes, "maximum bytes per local input to embed; 0 disables the per-input limit")
	cmd.Flags().BoolVar(&jsonReport, "json", false, "write machine-readable output")
	return cmd
}

func (a app) logsImportCommand() *cobra.Command {
	var dir string
	var jsonReport bool
	cmd := &cobra.Command{
		Use:   "import BUNDLE",
		Short: "Import a .molrun bundle into local run history",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("logs import", jsonReport, func() error {
				if err := exactArgs(args, 1, "logs import"); err != nil {
					return markError(kindInvalidInput, err)
				}
				path, envelope, err := importRunBundle(dir, args[0])
				if err != nil {
					return markError(kindInvalidInput, err)
				}
				if jsonReport {
					return writeJSON(a.stdout, map[string]any{"ok": true, "path": path, "run": envelope})
				}
				fmt.Fprintln(a.stdout, path)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "run history directory; defaults to .molstar-runs or MOLSTAR_RUNS_DIR")
	cmd.Flags().BoolVar(&jsonReport, "json", false, "write machine-readable output")
	return cmd
}

func (a app) logsRerunCommand() *cobra.Command {
	var outDir string
	var jsonReport bool
	cmd := &cobra.Command{
		Use:   "rerun BUNDLE",
		Short: "Rerun a .molrun bundle without importing it",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("logs rerun", jsonReport, func() error {
				if err := exactArgs(args, 1, "logs rerun"); err != nil {
					return markError(kindInvalidInput, err)
				}
				envelope, files, err := readRunBundle(args[0])
				if err != nil {
					return markError(kindInvalidInput, err)
				}
				report, err := a.rerunRunLog(cmd.Context(), envelope, outDir)
				if err != nil {
					return err
				}
				if jsonReport {
					return writeJSON(a.stdout, map[string]any{
						"ok":     true,
						"bundle": args[0],
						"files":  files,
						"run":    envelope,
						"rerun":  report,
					})
				}
				fmt.Fprintf(a.stdout, "bundle\t%s\n", args[0])
				fmt.Fprintf(a.stdout, "run\t%s\n", envelope.ID)
				fmt.Fprintf(a.stdout, "rerun\t%s\n", renderStatusWord(report.OK))
				return a.writeRenderSummary(report)
			})
		},
	}
	cmd.Flags().StringVar(&outDir, "out-dir", "", "directory for rerun outputs; defaults to original output paths")
	cmd.Flags().BoolVar(&jsonReport, "json", false, "write machine-readable output")
	return cmd
}

type runLogVerifyReport struct {
	OK              bool         `json:"ok"`
	Bundle          string       `json:"bundle"`
	ID              string       `json:"id,omitempty"`
	Replayable      bool         `json:"replayable"`
	FullyReplayable bool         `json:"fully_replayable"`
	Replay          runLogReplay `json:"replay"`
	Files           []string     `json:"files,omitempty"`
	ExpectedOutputs []string     `json:"expected_outputs,omitempty"`
	Warnings        []string     `json:"warnings,omitempty"`
}

func (a app) logsVerifyCommand() *cobra.Command {
	var jsonReport bool
	var strict bool
	cmd := &cobra.Command{
		Use:   "verify BUNDLE",
		Short: "Verify a .molrun bundle without importing it",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("logs verify", jsonReport, func() error {
				if err := exactArgs(args, 1, "logs verify"); err != nil {
					return markError(kindInvalidInput, err)
				}
				envelope, files, err := readRunBundle(args[0])
				if err != nil {
					return markError(kindInvalidInput, err)
				}
				report := verifyRunBundleReport(args[0], envelope, files)
				if jsonReport {
					strictFailed := strict && !report.Replayable
					if strictFailed {
						report.OK = false
					}
					if err := writeJSON(a.stdout, report); err != nil {
						return markError(kindInternal, err)
					}
					if strictFailed {
						return alreadyReported(markError(kindValidation, fmt.Errorf("bundle is not replayable")))
					}
					return nil
				}
				fmt.Fprintf(a.stdout, "bundle\t%s\n", report.Bundle)
				if report.ID != "" {
					fmt.Fprintf(a.stdout, "run\t%s\n", report.ID)
				}
				writeRunLogReplaySummary(a.stdout, report.Replay)
				for _, path := range report.ExpectedOutputs {
					fmt.Fprintf(a.stdout, "expected_output\t%s\n", path)
				}
				if strict && !report.Replayable {
					return markError(kindValidation, fmt.Errorf("bundle is not replayable"))
				}
				return nil
			})
		},
	}
	cmd.Flags().BoolVar(&jsonReport, "json", false, "write machine-readable output")
	cmd.Flags().BoolVar(&strict, "strict", false, "fail when the bundle is not replayable")
	return cmd
}

func (a app) logsPruneCommand() *cobra.Command {
	var dir string
	var olderThan string
	var dryRun bool
	var jsonReport bool
	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Remove old local run logs",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("logs prune", jsonReport, func() error {
				if len(args) != 0 {
					return markError(kindInvalidInput, fmt.Errorf("logs prune does not accept positional arguments"))
				}
				if strings.TrimSpace(olderThan) == "" {
					return markError(kindInvalidInput, fmt.Errorf("logs prune requires --older-than"))
				}
				duration, err := parseRunLogAge(olderThan)
				if err != nil {
					return markError(kindInvalidInput, err)
				}
				removed, err := pruneRunLogs(dir, time.Now().UTC().Add(-duration), dryRun)
				if err != nil {
					return markError(kindInvalidInput, err)
				}
				if jsonReport {
					return writeJSON(a.stdout, map[string]any{"ok": true, "dry_run": dryRun, "removed": removed})
				}
				for _, path := range removed {
					if dryRun {
						fmt.Fprintf(a.stdout, "would remove\t%s\n", path)
					} else {
						fmt.Fprintf(a.stdout, "removed\t%s\n", path)
					}
				}
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "run history directory; defaults to .molstar-runs or MOLSTAR_RUNS_DIR")
	cmd.Flags().StringVar(&olderThan, "older-than", "", "remove logs older than this age, e.g. 14d, 48h")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print logs that would be removed without deleting")
	cmd.Flags().BoolVar(&jsonReport, "json", false, "write machine-readable output")
	return cmd
}

type runLogEntry struct {
	Envelope runLogEnvelope
	Path     string
}

func maybeWriteRunLog(command string, report renderReport, enabled bool) (string, error) {
	if !enabled {
		return "", nil
	}
	return writeRunLog(command, report)
}

func writeRunLog(command string, report renderReport) (string, error) {
	return writeRunLogWithOptions(command, report, defaultRunLogAssetPolicy())
}

func maybeWriteRunLogWithOptions(command string, report renderReport, enabled bool, policy runLogAssetPolicy) (string, error) {
	if !enabled {
		return "", nil
	}
	return writeRunLogWithOptions(command, report, policy)
}

func writeRunLogWithOptions(command string, report renderReport, policy runLogAssetPolicy) (string, error) {
	dir := runLogDir("")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	now := time.Now().UTC()
	id := now.Format("20060102T150405.000000000Z")
	path := filepath.Join(dir, id+".json")
	assets, replay := collectRunLogAssets(report, policy)
	envelope := runLogEnvelope{
		OK:        report.OK,
		ID:        id,
		Command:   command,
		Label:     strings.TrimSpace(report.RunLabel),
		CreatedAt: now.Format(time.RFC3339Nano),
		Report:    report,
		Assets:    assets,
		Replay:    replay,
	}
	data, err := marshalJSON(envelope)
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, data, 0o644)
}

func runLogIDFromPath(path string) string {
	base := filepath.Base(strings.TrimSpace(path))
	ext := filepath.Ext(base)
	if ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	return base
}

func defaultRunLogAssetPolicy() runLogAssetPolicy {
	return runLogAssetPolicy{
		IncludeInputs:       true,
		MaxInputBytes:       defaultRunLogMaxInputBytes,
		MaxSingleInputBytes: defaultRunLogMaxSingleInputBytes,
	}
}

func runLogOptionsFromRenderFlags(flags *renderFlags) runLogAssetPolicy {
	if flags == nil {
		return defaultRunLogAssetPolicy()
	}
	return runLogAssetPolicy{
		IncludeInputs:       flags.logAssets,
		MaxInputBytes:       flags.logAssetsMaxBytes,
		MaxSingleInputBytes: flags.logAssetMaxBytes,
	}
}

func (a app) rerunRunLog(ctx context.Context, envelope runLogEnvelope, outDir string) (renderReport, error) {
	if envelope.Report.Job != nil {
		j := *envelope.Report.Job
		cleanup, err := restoreRunLogAssets(&j, envelope.Assets)
		if err != nil {
			return renderReport{}, markError(kindRuntime, err)
		}
		defer cleanup()
		if err := validateRunLogInputsAvailable(j, envelope.Assets); err != nil {
			return renderReport{}, markError(kindInvalidInput, err)
		}
		if strings.TrimSpace(outDir) != "" {
			j.Outputs = rewriteJobOutputsToDir(j.Outputs, outDir)
		}
		return a.executeRenderJob(ctx, "rerun:"+envelope.ID, j, "", false)
	}
	if envelope.Report.MVSDocument != nil {
		return a.rerunMVSRunLog(ctx, envelope, outDir)
	}
	return renderReport{}, markError(kindInvalidInput, fmt.Errorf("run %s does not include a replayable job", envelope.ID))
}

func (a app) rerunMVSRunLog(ctx context.Context, envelope runLogEnvelope, outDir string) (renderReport, error) {
	replay := replayInfoForEnvelope(envelope)
	if !replay.Replayable {
		return renderReport{}, markError(kindInvalidInput, fmt.Errorf("run %s includes an MVS scene but it is not replayable: %s", envelope.ID, strings.Join(replay.Warnings, "; ")))
	}
	outputs, stateOut := outputsFromRunLogReport(envelope.Report)
	if len(outputs) == 0 {
		return renderReport{}, markError(kindInvalidInput, fmt.Errorf("run %s includes an MVS scene but no image or video outputs to rerun", envelope.ID))
	}
	if strings.TrimSpace(outDir) != "" {
		outputs = rewriteJobOutputsToDir(outputs, outDir)
		if strings.TrimSpace(stateOut) != "" {
			stateOut = filepath.Join(outDir, filepath.Base(stateOut))
		}
	}
	if err := (job.Job{Outputs: outputs}).ValidateRuntimeLimits(); err != nil {
		return renderReport{}, markError(kindRuntime, err)
	}
	data, err := mvs.Marshal(*envelope.Report.MVSDocument)
	if err != nil {
		return renderReport{}, markError(kindInvalidScene, err)
	}
	stageStart := time.Now()
	scenePath, cleanup, err := writeTempMVS(data)
	report := renderReport{OK: true, Input: "rerun:" + envelope.ID, MVSDocument: envelope.Report.MVSDocument}
	report.finishStage("write_temp_mvs", "", stageStart, err)
	if err != nil {
		return report, markError(kindInvalidScene, err)
	}
	defer cleanup()
	report.MVS = scenePath

	runner := render.NewMolstar()
	runner.Stdout = a.stderr
	runner.Stderr = a.stderr
	runner.Quiet = true
	report.Diagnostics = map[string]any{"renderer_mode": "subprocess", "source": "mvs_document"}

	for i, output := range outputs {
		saveMolj := stateOut != "" && i == 0
		stageStart := time.Now()
		result, outputReports, err := renderTransactional(ctx, runner, scenePath, output, saveMolj, stateOut, false)
		report.finishStage("render_output", output.Path, stageStart, err)
		report.Commands = append(report.Commands, result)
		if err != nil {
			return report, markError(kindRender, err)
		}
		for _, outputReport := range outputReports {
			report.Outputs = append(report.Outputs, outputReport.Path)
			report.OutputFiles = append(report.OutputFiles, outputReport)
		}
	}
	return report, nil
}

func outputsFromRunLogReport(report renderReport) ([]job.Output, string) {
	var outputs []job.Output
	stateOut := ""
	seen := map[string]bool{}
	add := func(path string, outputType string, width int, height int) {
		path = strings.TrimSpace(path)
		if path == "" || seen[path] {
			return
		}
		normalized := job.Output{Type: firstNonEmpty(outputType, outputTypeFromPath(path)), Path: path}.NormalizedType()
		switch normalized {
		case "molj":
			if stateOut == "" {
				stateOut = path
			}
			return
		case "image", "video":
		default:
			return
		}
		seen[path] = true
		output := job.Output{Type: normalized, Path: path}
		if width > 0 && height > 0 {
			output.Size = []int{width, height}
		}
		outputs = append(outputs, output)
	}
	for _, outputFile := range report.OutputFiles {
		add(outputFile.Path, outputFile.Type, outputFile.Width, outputFile.Height)
	}
	if len(outputs) == 0 {
		for _, path := range report.Outputs {
			add(path, outputTypeFromPath(path), 0, 0)
		}
	}
	return outputs, stateOut
}

func collectRunLogAssets(report renderReport, policy runLogAssetPolicy) ([]runLogAsset, runLogReplay) {
	replay := runLogReplay{Replayable: true, FullyReplayable: true, IncludesInputs: policy.IncludeInputs}
	if report.Job == nil {
		if report.MVSDocument != nil {
			return nil, replayInfoForMVSDocument(report.MVSDocument)
		}
		return nil, runLogReplay{
			Replayable:      false,
			FullyReplayable: false,
			IncludesInputs:  false,
			Warnings:        []string{"run does not include a normalized job or MVS scene; logs show --rerun is unavailable"},
		}
	}
	assets := []runLogAsset{}
	totalBytes := int64(0)
	for ref, input := range report.Job.Inputs {
		local := input.LocalPath()
		if local == "" {
			if input.RequiresNetwork() {
				replay.FullyReplayable = false
				replay.ExternalInputs = append(replay.ExternalInputs, ref)
				replay.Warnings = append(replay.Warnings, fmt.Sprintf("input %q is remote and will be fetched again during replay", ref))
			}
			continue
		}
		if !policy.IncludeInputs {
			replay.Replayable = false
			replay.FullyReplayable = false
			replay.MissingInputs = append(replay.MissingInputs, ref)
			replay.Warnings = append(replay.Warnings, fmt.Sprintf("input %q is local and was not embedded because include_inputs=false", ref))
			continue
		}
		data, err := os.ReadFile(local)
		if err != nil {
			replay.Replayable = false
			replay.FullyReplayable = false
			replay.MissingInputs = append(replay.MissingInputs, ref)
			replay.Warnings = append(replay.Warnings, fmt.Sprintf("input %q could not be embedded from %s: %v", ref, local, err))
			continue
		}
		if len(data) == 0 {
			replay.Replayable = false
			replay.FullyReplayable = false
			replay.MissingInputs = append(replay.MissingInputs, ref)
			replay.Warnings = append(replay.Warnings, fmt.Sprintf("input %q at %s is empty and was not embedded", ref, local))
			continue
		}
		size := int64(len(data))
		if policy.MaxSingleInputBytes > 0 && size > policy.MaxSingleInputBytes {
			replay.Replayable = false
			replay.FullyReplayable = false
			replay.MissingInputs = append(replay.MissingInputs, ref)
			replay.Warnings = append(replay.Warnings, fmt.Sprintf("input %q was not embedded because it exceeds max_single_input_bytes=%d", ref, policy.MaxSingleInputBytes))
			continue
		}
		if policy.MaxInputBytes > 0 && totalBytes+size > policy.MaxInputBytes {
			replay.Replayable = false
			replay.FullyReplayable = false
			replay.MissingInputs = append(replay.MissingInputs, ref)
			replay.Warnings = append(replay.Warnings, fmt.Sprintf("input %q was not embedded because local inputs exceed max_input_bytes=%d", ref, policy.MaxInputBytes))
			continue
		}
		assets = append(assets, runLogAsset{
			Ref:    ref,
			Name:   filepath.Base(local),
			Format: input.ResolvedFormat(),
			Data:   data,
		})
		totalBytes += size
		replay.EmbeddedInputBytes += size
		replay.EmbeddedInputs = append(replay.EmbeddedInputs, ref)
	}
	if len(assets) == 0 {
		replay.IncludesInputs = false
	}
	replay.MissingInputs = uniqueStrings(replay.MissingInputs)
	replay.ExternalInputs = uniqueStrings(replay.ExternalInputs)
	replay.EmbeddedInputs = uniqueStrings(replay.EmbeddedInputs)
	replay.Warnings = uniqueStrings(replay.Warnings)
	if len(replay.MissingInputs) > 0 {
		replay.Replayable = false
		replay.FullyReplayable = false
	}
	if len(replay.ExternalInputs) > 0 {
		replay.FullyReplayable = false
	}
	return assets, replay
}

func restoreRunLogAssets(j *job.Job, assets []runLogAsset) (func(), error) {
	if len(assets) == 0 {
		return func() {}, nil
	}
	dir, err := os.MkdirTemp("", "headlessmolstar-rerun-assets-*")
	if err != nil {
		return nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	for _, asset := range assets {
		if asset.Ref == "" || len(asset.Data) == 0 {
			continue
		}
		name := filepath.Base(asset.Name)
		if name == "." || name == string(filepath.Separator) || name == "" {
			name = asset.Ref + "." + firstNonEmpty(asset.Format, "dat")
		}
		path := filepath.Join(dir, sanitizeRef(asset.Ref)+"-"+name)
		if err := os.WriteFile(path, asset.Data, 0o644); err != nil {
			cleanup()
			return nil, err
		}
		input := j.Inputs[asset.Ref]
		input.ID = ""
		input.URL = ""
		input.Provider = ""
		input.Path = path
		if input.Format == "" {
			input.Format = asset.Format
		}
		j.Inputs[asset.Ref] = input
	}
	return cleanup, nil
}

func validateRunLogInputsAvailable(j job.Job, assets []runLogAsset) error {
	restored := map[string]bool{}
	for _, asset := range assets {
		if asset.Ref != "" && len(asset.Data) > 0 {
			restored[asset.Ref] = true
		}
	}
	var missing []string
	for ref, input := range j.Inputs {
		local := input.LocalPath()
		if local == "" || restored[ref] {
			continue
		}
		if _, err := os.Stat(local); err != nil {
			missing = append(missing, ref)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("run is not replayable because local input refs are missing: %s; export with --include-inputs=true while the source files still exist", strings.Join(missing, ", "))
	}
	return nil
}

func writeRunLogReplaySummary(writer io.Writer, replay runLogReplay) {
	status := "not-replayable"
	if replay.FullyReplayable {
		status = "fully-replayable"
	} else if replay.Replayable {
		status = "replayable"
	}
	fmt.Fprintf(writer, "replay\t%s\n", status)
	if replay.EmbeddedInputBytes > 0 {
		fmt.Fprintf(writer, "replay_inputs\t%d bytes\n", replay.EmbeddedInputBytes)
	}
	for _, warning := range replay.Warnings {
		fmt.Fprintf(writer, "warning\t%s\n", warning)
	}
}

func prepareRunLogForExport(envelope runLogEnvelope, policy runLogAssetPolicy) runLogEnvelope {
	envelope = normalizeRunLogEnvelope(envelope)
	if envelope.Report.Job == nil {
		if envelope.Report.MVSDocument != nil {
			envelope.Assets = nil
			envelope.Replay = replayInfoForEnvelope(envelope)
			return envelope
		}
		envelope.Assets = nil
		envelope.Replay = runLogReplay{
			Replayable:      false,
			FullyReplayable: false,
			IncludesInputs:  false,
			Warnings:        []string{"run does not include a normalized job or MVS scene; logs show --rerun is unavailable"},
		}
		return envelope
	}
	if !policy.IncludeInputs {
		envelope.Assets = nil
		_, replay := collectRunLogAssets(envelope.Report, policy)
		envelope.Replay = replay
		return envelope
	}
	var kept []runLogAsset
	var total int64
	var dropped []string
	for _, asset := range envelope.Assets {
		size := int64(len(asset.Data))
		if policy.MaxSingleInputBytes > 0 && size > policy.MaxSingleInputBytes {
			dropped = append(dropped, asset.Ref)
			continue
		}
		if policy.MaxInputBytes > 0 && total+size > policy.MaxInputBytes {
			dropped = append(dropped, asset.Ref)
			continue
		}
		kept = append(kept, asset)
		total += size
	}
	envelope.Assets = kept
	envelope.Replay = replayInfoForEnvelope(envelope)
	if len(dropped) > 0 {
		sort.Strings(dropped)
		envelope.Replay.Replayable = false
		envelope.Replay.FullyReplayable = false
		envelope.Replay.MissingInputs = uniqueStrings(append(envelope.Replay.MissingInputs, dropped...))
		envelope.Replay.Warnings = uniqueStrings(append(envelope.Replay.Warnings, fmt.Sprintf("inputs were omitted from export because local input size limits were exceeded: %s", strings.Join(dropped, ", "))))
	}
	return envelope
}

func normalizeRunLogEnvelope(envelope runLogEnvelope) runLogEnvelope {
	if envelope.Label == "" {
		envelope.Label = envelope.Report.RunLabel
	}
	if envelope.Report.Job == nil {
		envelope.Replay = replayInfoForEnvelope(envelope)
		return envelope
	}
	if len(envelope.Replay.Warnings) == 0 && len(envelope.Assets) == 0 && envelope.Report.Job != nil {
		envelope.Replay = replayInfoForEnvelope(envelope)
	}
	if envelope.Replay.Replayable || envelope.Replay.FullyReplayable || len(envelope.Replay.Warnings) > 0 {
		return envelope
	}
	envelope.Replay = replayInfoForEnvelope(envelope)
	return envelope
}

func replayInfoForEnvelope(envelope runLogEnvelope) runLogReplay {
	replay := runLogReplay{Replayable: true, FullyReplayable: true, IncludesInputs: len(envelope.Assets) > 0}
	if envelope.Report.Job == nil {
		if envelope.Report.MVSDocument != nil {
			return replayInfoForMVSDocument(envelope.Report.MVSDocument)
		}
		return runLogReplay{
			Replayable:      false,
			FullyReplayable: false,
			IncludesInputs:  false,
			Warnings:        []string{"run does not include a normalized job or MVS scene; logs show --rerun is unavailable"},
		}
	}
	embedded := map[string]int64{}
	for _, asset := range envelope.Assets {
		if asset.Ref == "" || len(asset.Data) == 0 {
			continue
		}
		embedded[asset.Ref] += int64(len(asset.Data))
		replay.EmbeddedInputBytes += int64(len(asset.Data))
		replay.EmbeddedInputs = append(replay.EmbeddedInputs, asset.Ref)
	}
	for ref, input := range envelope.Report.Job.Inputs {
		if input.LocalPath() != "" {
			if embedded[ref] == 0 {
				replay.Replayable = false
				replay.FullyReplayable = false
				replay.MissingInputs = append(replay.MissingInputs, ref)
				replay.Warnings = append(replay.Warnings, fmt.Sprintf("input %q is local but is not embedded in this log or bundle", ref))
			}
			continue
		}
		if input.RequiresNetwork() {
			replay.FullyReplayable = false
			replay.ExternalInputs = append(replay.ExternalInputs, ref)
			replay.Warnings = append(replay.Warnings, fmt.Sprintf("input %q is remote and will be fetched again during replay", ref))
		}
	}
	replay.EmbeddedInputs = uniqueStrings(replay.EmbeddedInputs)
	replay.MissingInputs = uniqueStrings(replay.MissingInputs)
	replay.ExternalInputs = uniqueStrings(replay.ExternalInputs)
	replay.Warnings = uniqueStrings(replay.Warnings)
	if len(replay.MissingInputs) > 0 {
		replay.Replayable = false
		replay.FullyReplayable = false
	}
	if len(replay.ExternalInputs) > 0 {
		replay.FullyReplayable = false
	}
	if len(envelope.Assets) == 0 {
		replay.IncludesInputs = false
	}
	return replay
}

func replayInfoForMVSDocument(document *mvs.Document) runLogReplay {
	replay := runLogReplay{Replayable: true, FullyReplayable: true, IncludesInputs: false}
	if document == nil {
		replay.Replayable = false
		replay.FullyReplayable = false
		replay.Warnings = append(replay.Warnings, "run does not include a normalized job or MVS scene; logs show --rerun is unavailable")
		return replay
	}
	downloads := collectMVSDownloadURLs(document.Root)
	for _, raw := range downloads {
		ref := "mvs:" + raw
		local, ok := localPathFromMVSDownloadURL(raw)
		if ok {
			replay.FullyReplayable = false
			if _, err := os.Stat(local); err != nil {
				replay.Replayable = false
				replay.MissingInputs = append(replay.MissingInputs, ref)
				replay.Warnings = append(replay.Warnings, fmt.Sprintf("MVS scene references missing local file %s", local))
			} else {
				replay.ExternalInputs = append(replay.ExternalInputs, ref)
				replay.Warnings = append(replay.Warnings, fmt.Sprintf("MVS scene references local file %s; replay requires it to remain available", local))
			}
			continue
		}
		if parsed, err := url.Parse(raw); err == nil && parsed.Scheme != "" {
			replay.FullyReplayable = false
			replay.ExternalInputs = append(replay.ExternalInputs, ref)
			replay.Warnings = append(replay.Warnings, fmt.Sprintf("MVS scene references remote URL %s and will fetch it during replay", raw))
		}
	}
	replay.MissingInputs = uniqueStrings(replay.MissingInputs)
	replay.ExternalInputs = uniqueStrings(replay.ExternalInputs)
	replay.Warnings = uniqueStrings(replay.Warnings)
	if len(replay.MissingInputs) > 0 {
		replay.Replayable = false
		replay.FullyReplayable = false
	}
	if len(replay.ExternalInputs) > 0 {
		replay.FullyReplayable = false
	}
	return replay
}

func collectMVSDownloadURLs(node mvs.Node) []string {
	var urls []string
	if node.Kind == "download" {
		if value, ok := node.Params["url"].(string); ok && strings.TrimSpace(value) != "" {
			urls = append(urls, strings.TrimSpace(value))
		}
	}
	for _, child := range node.Children {
		urls = append(urls, collectMVSDownloadURLs(child)...)
	}
	return urls
}

func localPathFromMVSDownloadURL(raw string) (string, bool) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	if parsed.Scheme == "file" {
		return filepath.FromSlash(parsed.Path), true
	}
	if parsed.Scheme == "" && filepath.IsAbs(raw) {
		return raw, true
	}
	return "", false
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func rewriteJobOutputsToDir(outputs []job.Output, dir string) []job.Output {
	rewritten := make([]job.Output, 0, len(outputs))
	for i, output := range outputs {
		if output.Path == "" {
			output.Path = fmt.Sprintf("output-%d.%s", i, outputExt(output))
		}
		output.Path = filepath.Join(dir, filepath.Base(output.Path))
		rewritten = append(rewritten, output)
	}
	return rewritten
}

func outputExt(output job.Output) string {
	switch output.NormalizedType() {
	case "mvsj":
		return "mvsj"
	case "mvsx":
		return "mvsx"
	case "molj":
		return "molj"
	case "video":
		return "mp4"
	default:
		return "png"
	}
}

func exportRunBundle(path string, envelope runLogEnvelope) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	writer := zip.NewWriter(file)
	addJSON := func(name string, value any) error {
		entry, err := writer.Create(name)
		if err != nil {
			return err
		}
		data, err := marshalJSON(value)
		if err != nil {
			return err
		}
		_, err = entry.Write(data)
		return err
	}
	if err := addJSON("run.json", envelope); err != nil {
		_ = writer.Close()
		_ = file.Close()
		return err
	}
	if envelope.Report.Job != nil {
		if err := addJSON("job.json", envelope.Report.Job); err != nil {
			_ = writer.Close()
			_ = file.Close()
			return err
		}
	}
	if envelope.Report.MVSDocument != nil {
		entry, err := writer.Create("scene.mvsj")
		if err != nil {
			_ = writer.Close()
			_ = file.Close()
			return err
		}
		data, err := mvs.Marshal(*envelope.Report.MVSDocument)
		if err != nil {
			_ = writer.Close()
			_ = file.Close()
			return err
		}
		if _, err := entry.Write(data); err != nil {
			_ = writer.Close()
			_ = file.Close()
			return err
		}
	}
	if err := writer.Close(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func importRunBundle(dir string, bundle string) (string, runLogEnvelope, error) {
	envelope, _, err := readRunBundle(bundle)
	if err != nil {
		return "", runLogEnvelope{}, err
	}
	targetDir := runLogDir(dir)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", runLogEnvelope{}, err
	}
	target := filepath.Join(targetDir, envelope.ID+".json")
	data, err := marshalJSON(envelope)
	if err != nil {
		return "", runLogEnvelope{}, err
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		return "", runLogEnvelope{}, err
	}
	return target, envelope, nil
}

func readRunBundle(bundle string) (runLogEnvelope, []string, error) {
	reader, err := zip.OpenReader(bundle)
	if err != nil {
		return runLogEnvelope{}, nil, err
	}
	defer reader.Close()
	var envelope runLogEnvelope
	files := make([]string, 0, len(reader.File))
	decoded := false
	// Enumerate every entry: `files` drives the sidecar checks in
	// verifyRunBundleReport, so stopping at run.json made verify report bundles
	// this tool had just written as missing their job.json/scene.mvsj sidecars.
	for _, file := range reader.File {
		files = append(files, file.Name)
		if file.Name != "run.json" || decoded {
			continue
		}
		opened, err := file.Open()
		if err != nil {
			return runLogEnvelope{}, files, err
		}
		data, err := io.ReadAll(opened)
		_ = opened.Close()
		if err != nil {
			return runLogEnvelope{}, files, err
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			return runLogEnvelope{}, files, err
		}
		decoded = true
	}
	if envelope.ID == "" {
		return runLogEnvelope{}, files, fmt.Errorf("bundle is missing run.json")
	}
	sort.Strings(files)
	return normalizeRunLogEnvelope(envelope), files, nil
}

func verifyRunBundleReport(bundle string, envelope runLogEnvelope, files []string) runLogVerifyReport {
	replay := replayInfoForEnvelope(envelope)
	expected := expectedOutputsFromRunReport(envelope.Report)
	warnings := append([]string{}, replay.Warnings...)
	if !containsString(files, "job.json") && envelope.Report.Job != nil {
		warnings = append(warnings, "bundle run.json includes a job but job.json sidecar is missing")
	}
	if !containsString(files, "scene.mvsj") && envelope.Report.MVSDocument != nil {
		warnings = append(warnings, "bundle run.json includes an MVS document but scene.mvsj sidecar is missing")
	}
	return runLogVerifyReport{
		OK:              true,
		Bundle:          bundle,
		ID:              envelope.ID,
		Replayable:      replay.Replayable,
		FullyReplayable: replay.FullyReplayable,
		Replay:          replay,
		Files:           files,
		ExpectedOutputs: expected,
		Warnings:        uniqueStrings(warnings),
	}
}

func expectedOutputsFromRunReport(report renderReport) []string {
	seen := map[string]bool{}
	var outputs []string
	for _, output := range report.OutputFiles {
		if output.Path != "" && !seen[output.Path] {
			seen[output.Path] = true
			outputs = append(outputs, output.Path)
		}
	}
	for _, output := range report.Outputs {
		if output != "" && !seen[output] {
			seen[output] = true
			outputs = append(outputs, output)
		}
	}
	sort.Strings(outputs)
	return outputs
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func readLastRunLog(dir string) (runLogEnvelope, string, error) {
	entries, err := listRunLogs(dir)
	if err != nil {
		return runLogEnvelope{}, "", err
	}
	if len(entries) == 0 {
		return runLogEnvelope{}, "", fmt.Errorf("no run logs found in %s", runLogDir(dir))
	}
	last := entries[len(entries)-1]
	return last.Envelope, last.Path, nil
}

func readRunLogByID(dir string, id string) (runLogEntry, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return runLogEntry{}, fmt.Errorf("empty run id")
	}
	entries, err := listRunLogs(dir)
	if err != nil {
		return runLogEntry{}, err
	}
	for _, entry := range entries {
		if entry.Envelope.ID == id || strings.TrimSuffix(filepath.Base(entry.Path), filepath.Ext(entry.Path)) == id {
			return entry, nil
		}
	}
	return runLogEntry{}, fmt.Errorf("run %q not found in %s", id, runLogDir(dir))
}

func listRunLogs(dir string) ([]runLogEntry, error) {
	dir = runLogDir(dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range entries {
		// Skip directories, non-JSON files, and hidden/AppleDouble "._*" files
		// that macOS leaves in a run directory on a non-native filesystem.
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		files = append(files, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(files)
	out := make([]runLogEntry, 0, len(files))
	for _, path := range files {
		entry, err := readRunLogFile(path)
		if err != nil {
			continue
		}
		out = append(out, runLogEntry{Envelope: entry, Path: path})
	}
	return out, nil
}

func readRunLogFile(path string) (runLogEnvelope, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return runLogEnvelope{}, err
	}
	var envelope runLogEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return runLogEnvelope{}, err
	}
	if envelope.ID == "" {
		envelope.ID = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	envelope = normalizeRunLogEnvelope(envelope)
	return envelope, nil
}

func summarizeRunLog(entry runLogEntry) runLogSummary {
	report := entry.Envelope.Report
	return runLogSummary{
		ID:              entry.Envelope.ID,
		OK:              entry.Envelope.OK,
		Command:         entry.Envelope.Command,
		Label:           entry.Envelope.Label,
		Input:           report.Input,
		FirstOutput:     firstRunOutput(report),
		DurationMS:      runDurationMS(report),
		Replayable:      entry.Envelope.Replay.Replayable,
		FullyReplayable: entry.Envelope.Replay.FullyReplayable,
		CreatedAt:       entry.Envelope.CreatedAt,
		Path:            entry.Path,
	}
}

func firstRunOutput(report renderReport) string {
	if len(report.OutputFiles) > 0 {
		return report.OutputFiles[0].Path
	}
	if len(report.Outputs) > 0 {
		return report.Outputs[0]
	}
	return ""
}

func runDurationMS(report renderReport) int64 {
	var total int64
	for _, stage := range report.Stages {
		if stage.DurationMS > 0 {
			total += stage.DurationMS
		}
	}
	return total
}

func pruneRunLogs(dir string, cutoff time.Time, dryRun bool) ([]string, error) {
	entries, err := listRunLogs(dir)
	if err != nil {
		return nil, err
	}
	var removed []string
	for _, entry := range entries {
		created, err := time.Parse(time.RFC3339Nano, entry.Envelope.CreatedAt)
		if err != nil {
			info, statErr := os.Stat(entry.Path)
			if statErr != nil {
				continue
			}
			created = info.ModTime()
		}
		if created.After(cutoff) || created.Equal(cutoff) {
			continue
		}
		removed = append(removed, entry.Path)
		if !dryRun {
			if err := os.Remove(entry.Path); err != nil {
				return removed, err
			}
		}
	}
	return removed, nil
}

// parseRetentionDuration parses a retention age. It extends Go's duration
// syntax with a "d" (days) suffix, because every retention flag in the CLI is
// naturally expressed in days. All of them share this parser: `logs prune`
// accepted "14d" while `cache prune --older-than` and `jobs prune --ttl`
// rejected it, so the same value worked or failed depending on the subcommand.
func parseRetentionDuration(value string) (time.Duration, error) {
	trimmed := strings.TrimSpace(value)
	if days, ok := strings.CutSuffix(trimmed, "d"); ok {
		parsed, err := time.ParseDuration(days + "h")
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q: use a value such as 14d or 48h", value)
		}
		return parsed * 24, nil
	}
	parsed, err := time.ParseDuration(trimmed)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: use a value such as 14d or 48h", value)
	}
	return parsed, nil
}

func parseRunLogAge(value string) (time.Duration, error) {
	return parseRetentionDuration(value)
}

func runLogDir(dir string) string {
	if strings.TrimSpace(dir) != "" {
		return dir
	}
	if env := strings.TrimSpace(os.Getenv("MOLSTAR_RUNS_DIR")); env != "" {
		return env
	}
	return ".molstar-runs"
}
