package cli

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sacha-ichbiah/molstar/internal/job"
	"github.com/sacha-ichbiah/molstar/internal/mvs"
)

type diagnoseReport struct {
	OK              bool          `json:"ok"`
	Source          string        `json:"source"`
	SourcePath      string        `json:"source_path,omitempty"`
	RunID           string        `json:"run_id,omitempty"`
	Failed          bool          `json:"failed"`
	Error           *errorBody    `json:"error,omitempty"`
	LikelyCause     string        `json:"likely_cause,omitempty"`
	RendererStatus  string        `json:"renderer_status,omitempty"`
	Replayable      bool          `json:"replayable"`
	FullyReplayable bool          `json:"fully_replayable"`
	Replay          *runLogReplay `json:"replay,omitempty"`
	NextCommand     string        `json:"next_command,omitempty"`
	Diagnostics     []string      `json:"diagnostics,omitempty"`
	ArtifactFiles   []string      `json:"artifact_files,omitempty"`
}

func (a app) diagnoseCommand() *cobra.Command {
	var dir string
	var ciArtifact string
	var bundle bool
	var out string
	var includeInputs bool
	var maxInputBytes int64
	var maxSingleInputBytes int64
	var redactPaths bool
	var redactEnv bool
	var redactInputs bool
	var jsonReport bool
	cmd := &cobra.Command{
		Use:   "diagnose RUN_ID",
		Short: "Explain a failed run log or CI artifact",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("diagnose", jsonReport, func() error {
				if bundle {
					options := diagnoseBundleOptions{
						Assets: runLogAssetPolicy{
							IncludeInputs:       includeInputs && !redactInputs,
							MaxInputBytes:       maxInputBytes,
							MaxSingleInputBytes: maxSingleInputBytes,
						},
						Redactions: issueBundleRedactions{
							Paths:  redactPaths,
							Env:    redactEnv,
							Inputs: redactInputs,
						},
					}
					report, err := a.writeDiagnoseBundle(cmd.Context(), dir, ciArtifact, args, out, options)
					if err != nil {
						return err
					}
					if jsonReport {
						return writeJSON(a.stdout, report)
					}
					fmt.Fprintln(a.stdout, report["output"])
					return nil
				}
				report, err := a.buildDiagnoseReport(dir, ciArtifact, args)
				if err != nil {
					return err
				}
				if jsonReport {
					return writeJSON(a.stdout, report)
				}
				return a.writeDiagnoseSummary(report)
			})
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "run history directory; defaults to .molstar-runs or MOLSTAR_RUNS_DIR")
	cmd.Flags().StringVar(&ciArtifact, "ci-artifact", "", "CI artifact directory or ci-artifact.json file")
	cmd.Flags().BoolVar(&bundle, "bundle", false, "write a diagnostic issue bundle zip for RUN_ID")
	cmd.Flags().StringVarP(&out, "out", "o", "", "diagnostic bundle output path; defaults to RUN_ID.issue.zip")
	cmd.Flags().BoolVar(&includeInputs, "include-inputs", true, "include embedded run-log input bytes in diagnostic bundles")
	cmd.Flags().Int64Var(&maxInputBytes, "max-input-bytes", defaultRunLogMaxInputBytes, "maximum total local input bytes to include; 0 disables the limit")
	cmd.Flags().Int64Var(&maxSingleInputBytes, "max-single-input-bytes", defaultRunLogMaxSingleInputBytes, "maximum bytes per local input to include; 0 disables the per-input limit")
	cmd.Flags().BoolVar(&redactPaths, "redact-paths", false, "redact common local filesystem path prefixes from diagnostic bundles")
	cmd.Flags().BoolVar(&redactEnv, "redact-env", false, "redact secret-like environment variable values from diagnostic bundles")
	cmd.Flags().BoolVar(&redactInputs, "redact-inputs", false, "omit embedded local input bytes from diagnostic bundles")
	cmd.Flags().BoolVar(&jsonReport, "json", false, "write machine-readable output")
	return cmd
}

func (a app) buildDiagnoseReport(dir string, ciArtifact string, args []string) (diagnoseReport, error) {
	ciArtifact = strings.TrimSpace(ciArtifact)
	if ciArtifact != "" {
		if len(args) != 0 {
			return diagnoseReport{}, markError(kindInvalidInput, fmt.Errorf("diagnose --ci-artifact does not accept RUN_ID"))
		}
		return diagnoseCIArtifact(ciArtifact)
	}
	if err := exactArgs(args, 1, "diagnose"); err != nil {
		return diagnoseReport{}, markError(kindInvalidInput, err)
	}
	target := strings.TrimSpace(args[0])
	if target == "" {
		return diagnoseReport{}, markError(kindInvalidInput, fmt.Errorf("empty run id or artifact path"))
	}
	if isCIArtifactPath(target) {
		return diagnoseCIArtifact(target)
	}
	return diagnoseRunLog(dir, target)
}

func diagnoseRunLog(dir string, id string) (diagnoseReport, error) {
	entry, err := readRunLogByID(dir, id)
	if err != nil {
		return diagnoseReport{}, markError(kindInvalidInput, err)
	}
	envelope := normalizeRunLogEnvelope(entry.Envelope)
	runErr := errorFromRenderReport(envelope.Report)
	var body *errorBody
	var diagnostics []string
	if runErr != nil {
		errorBody := newErrorBody(runErr)
		body = &errorBody
		diagnostics = append(diagnostics, errorBody.Diagnosis...)
	}
	next := fmt.Sprintf("molstar logs show %s --rerun --out-dir reruns/%s", envelope.ID, envelope.ID)
	if !envelope.Replay.Replayable {
		next = fmt.Sprintf("molstar logs export %s --include-inputs=true --out %s.molrun", envelope.ID, envelope.ID)
	}
	diagnostics = append(diagnostics, envelope.Replay.Warnings...)
	return diagnoseReport{
		OK:              true,
		Source:          "run_log",
		SourcePath:      entry.Path,
		RunID:           envelope.ID,
		Failed:          !envelope.OK || !envelope.Report.OK,
		Error:           body,
		LikelyCause:     likelyCause(runErr),
		RendererStatus:  rendererStatus(envelope.Report),
		Replayable:      envelope.Replay.Replayable,
		FullyReplayable: envelope.Replay.FullyReplayable,
		Replay:          &envelope.Replay,
		NextCommand:     next,
		Diagnostics:     uniqueStrings(diagnostics),
	}, nil
}

func diagnoseCIArtifact(path string) (diagnoseReport, error) {
	dir, summaryPath := normalizeCIArtifactPath(path)
	data, err := os.ReadFile(summaryPath)
	if err != nil {
		return diagnoseReport{}, markError(kindInvalidInput, err)
	}
	var summary ciArtifactReport
	if err := json.Unmarshal(data, &summary); err != nil {
		return diagnoseReport{}, markError(kindInvalidInput, fmt.Errorf("decode CI artifact summary: %w", err))
	}
	var renderReport renderReport
	renderReportPath := filepath.Join(dir, "render-report.json")
	if data, err := os.ReadFile(renderReportPath); err == nil {
		_ = json.Unmarshal(data, &renderReport)
	}
	if renderReport.Job == nil {
		jobPath := filepath.Join(dir, "job.json")
		if data, err := os.ReadFile(jobPath); err == nil {
			var j job.Job
			if json.Unmarshal(data, &j) == nil {
				renderReport.Job = &j
			}
		}
	}
	replay := replayInfoForEnvelope(runLogEnvelope{Report: renderReport})
	errBody := summary.Error
	var body *errorBody
	if errBody.Message != "" {
		body = &errBody
	}
	diagnostics := []string{}
	if body != nil {
		diagnostics = append(diagnostics, body.Diagnosis...)
	}
	diagnostics = append(diagnostics, replay.Warnings...)
	doctorStatus := diagnoseDoctorStatus(filepath.Join(dir, "doctor.json"))
	next := "molstar doctor --json"
	jobPath := filepath.Join(dir, "job.json")
	if _, err := os.Stat(jobPath); err == nil {
		next = fmt.Sprintf("molstar job explain %s --json", jobPath)
	}
	if replay.Replayable {
		next = fmt.Sprintf("molstar render %s --json", jobPath)
	}
	files := summary.Files
	if len(files) == 0 {
		files = listCIArtifactFiles(dir)
	}
	return diagnoseReport{
		OK:              true,
		Source:          "ci_artifact",
		SourcePath:      dir,
		Failed:          !summary.OK,
		Error:           body,
		LikelyCause:     likelyCauseFromBody(body),
		RendererStatus:  firstNonEmpty(doctorStatus, rendererStatus(renderReport)),
		Replayable:      replay.Replayable,
		FullyReplayable: replay.FullyReplayable,
		Replay:          &replay,
		NextCommand:     next,
		Diagnostics:     uniqueStrings(diagnostics),
		ArtifactFiles:   files,
	}, nil
}

type diagnoseBundleOptions struct {
	Assets     runLogAssetPolicy
	Redactions issueBundleRedactions
}

type issueBundleRedactions struct {
	Paths  bool `json:"paths"`
	Env    bool `json:"env"`
	Inputs bool `json:"inputs"`
}

func (a app) writeDiagnoseBundle(ctx context.Context, dir string, ciArtifact string, args []string, out string, options diagnoseBundleOptions) (map[string]any, error) {
	if err := exactArgs(args, 1, "diagnose --bundle"); err != nil {
		return nil, markError(kindInvalidInput, err)
	}
	if strings.TrimSpace(ciArtifact) != "" && !isCIArtifactPath(ciArtifact) {
		return nil, markError(kindInvalidInput, fmt.Errorf("--ci-artifact must point to a CI artifact directory or ci-artifact.json file"))
	}
	entry, err := readRunLogByID(dir, args[0])
	if err != nil {
		return nil, markError(kindInvalidInput, err)
	}
	envelope := prepareRunLogForExport(entry.Envelope, options.Assets)
	diagnosis, err := diagnoseRunLog(dir, args[0])
	if err != nil {
		return nil, err
	}
	diagnosis.Replayable = envelope.Replay.Replayable
	diagnosis.FullyReplayable = envelope.Replay.FullyReplayable
	diagnosis.Replay = &envelope.Replay
	target := strings.TrimSpace(out)
	if target == "" {
		target = envelope.ID + ".issue.zip"
	}
	files, err := a.writeIssueBundle(ctx, target, entry.Path, envelope, diagnosis, ciArtifact, options)
	if err != nil {
		return nil, markError(kindRender, err)
	}
	return map[string]any{
		"ok":               true,
		"output":           target,
		"run_id":           envelope.ID,
		"files":            files,
		"replayable":       envelope.Replay.Replayable,
		"fully_replayable": envelope.Replay.FullyReplayable,
		"replay":           envelope.Replay,
		"redactions":       options.Redactions,
	}, nil
}

func (a app) writeIssueBundle(ctx context.Context, path string, runLogPath string, envelope runLogEnvelope, diagnosis diagnoseReport, ciArtifact string, options diagnoseBundleOptions) ([]string, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	file, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	writer := zip.NewWriter(file)
	var files []string
	redactor := newIssueBundleRedactor(options.Redactions)
	addBytes := func(name string, data []byte) error {
		if !strings.HasPrefix(name, "inputs/") {
			data = redactor.RedactBytes(data)
		}
		entry, err := writer.Create(name)
		if err != nil {
			return err
		}
		if _, err := entry.Write(data); err != nil {
			return err
		}
		files = append(files, name)
		return nil
	}
	addJSON := func(name string, value any) error {
		data, err := marshalJSON(value)
		if err != nil {
			return err
		}
		return addBytes(name, data)
	}
	closeWithError := func(err error) ([]string, error) {
		_ = writer.Close()
		_ = file.Close()
		return files, err
	}
	if err := addJSON("diagnosis.json", diagnosis); err != nil {
		return closeWithError(err)
	}
	if err := addJSON("redactions.json", options.Redactions); err != nil {
		return closeWithError(err)
	}
	if err := addJSON("run.json", envelope); err != nil {
		return closeWithError(err)
	}
	if data, err := os.ReadFile(runLogPath); err == nil {
		if err := addBytes("run-log.json", data); err != nil {
			return closeWithError(err)
		}
	}
	if err := addJSON("render-report.json", envelope.Report); err != nil {
		return closeWithError(err)
	}
	if envelope.Report.Job != nil {
		if err := addJSON("job.json", envelope.Report.Job); err != nil {
			return closeWithError(err)
		}
	}
	if envelope.Report.MVSDocument != nil {
		data, err := mvs.Marshal(*envelope.Report.MVSDocument)
		if err != nil {
			return closeWithError(err)
		}
		if err := addBytes("scene.mvsj", data); err != nil {
			return closeWithError(err)
		}
	} else if envelope.Report.Job != nil {
		compiled, err := mvs.Compile(*envelope.Report.Job)
		if err == nil {
			data, err := mvs.Marshal(compiled.Document)
			if err != nil {
				return closeWithError(err)
			}
			if err := addBytes("scene.mvsj", data); err != nil {
				return closeWithError(err)
			}
		}
	}
	if doctorOut, _, err := a.runSubcommand(ctx, "doctor", "--skip-probe", "--json"); err == nil {
		if err := addBytes("doctor.json", []byte(doctorOut)); err != nil {
			return closeWithError(err)
		}
	} else {
		if err := addJSON("doctor.json", errorReport{OK: false, Command: "doctor", Error: newErrorBody(err)}); err != nil {
			return closeWithError(err)
		}
	}
	if capabilitiesOut, _, err := a.runSubcommand(ctx, "capabilities", "--json", "--timeout", "15s"); err == nil {
		if err := addBytes("capabilities.json", []byte(capabilitiesOut)); err != nil {
			return closeWithError(err)
		}
	} else {
		if err := addJSON("capabilities.json", errorReport{OK: false, Command: "capabilities", Error: newErrorBody(err)}); err != nil {
			return closeWithError(err)
		}
	}
	if strings.TrimSpace(ciArtifact) != "" {
		ciDir, summaryPath := normalizeCIArtifactPath(ciArtifact)
		if data, err := os.ReadFile(summaryPath); err == nil {
			if err := addBytes("ci-artifact/ci-artifact.json", data); err != nil {
				return closeWithError(err)
			}
		}
		for _, name := range []string{"render-report.json", "job.json", "scene.mvsj", "explain.json", "doctor.json"} {
			data, err := os.ReadFile(filepath.Join(ciDir, name))
			if err != nil {
				continue
			}
			if err := addBytes("ci-artifact/"+name, data); err != nil {
				return closeWithError(err)
			}
		}
	}
	if options.Assets.IncludeInputs && !options.Redactions.Inputs {
		var total int64
		for _, asset := range envelope.Assets {
			if asset.Ref == "" || len(asset.Data) == 0 {
				continue
			}
			size := int64(len(asset.Data))
			if options.Assets.MaxSingleInputBytes > 0 && size > options.Assets.MaxSingleInputBytes {
				continue
			}
			if options.Assets.MaxInputBytes > 0 && total+size > options.Assets.MaxInputBytes {
				continue
			}
			total += size
			name := filepath.Base(asset.Name)
			if name == "." || name == "" || name == string(filepath.Separator) {
				name = firstNonEmpty(asset.Ref+"."+asset.Format, asset.Ref+".dat")
			}
			if err := addBytes("inputs/"+sanitizeRef(asset.Ref)+"-"+name, asset.Data); err != nil {
				return closeWithError(err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		_ = file.Close()
		return files, err
	}
	if err := file.Close(); err != nil {
		return files, err
	}
	sort.Strings(files)
	return files, nil
}

func (a app) writeDiagnoseSummary(report diagnoseReport) error {
	fmt.Fprintf(a.stdout, "diagnosis\t%s\n", renderStatusWord(report.OK))
	fmt.Fprintf(a.stdout, "source\t%s", report.Source)
	if report.SourcePath != "" {
		fmt.Fprintf(a.stdout, "\t%s", report.SourcePath)
	}
	fmt.Fprintln(a.stdout)
	if report.RunID != "" {
		fmt.Fprintf(a.stdout, "run\t%s\n", report.RunID)
	}
	fmt.Fprintf(a.stdout, "failed\t%t\n", report.Failed)
	if report.LikelyCause != "" {
		fmt.Fprintf(a.stdout, "cause\t%s\n", report.LikelyCause)
	}
	if report.RendererStatus != "" {
		fmt.Fprintf(a.stdout, "renderer\t%s\n", report.RendererStatus)
	}
	if report.Replay != nil {
		writeRunLogReplaySummary(a.stdout, *report.Replay)
	}
	if report.Error != nil {
		fmt.Fprintf(a.stdout, "error\t%s\n", singleLine(report.Error.Message))
	}
	for _, diagnostic := range report.Diagnostics {
		fmt.Fprintf(a.stdout, "diagnostic\t%s\n", diagnostic)
	}
	if report.NextCommand != "" {
		fmt.Fprintf(a.stdout, "next\t%s\n", report.NextCommand)
	}
	return nil
}

func isCIArtifactPath(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if info.IsDir() {
		_, err := os.Stat(filepath.Join(path, "ci-artifact.json"))
		return err == nil
	}
	return filepath.Base(path) == "ci-artifact.json"
}

func normalizeCIArtifactPath(path string) (string, string) {
	if filepath.Base(path) == "ci-artifact.json" {
		return filepath.Dir(path), path
	}
	return path, filepath.Join(path, "ci-artifact.json")
}

func listCIArtifactFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		files = append(files, filepath.Join(dir, entry.Name()))
	}
	return files
}

type issueBundleRedactor struct {
	replacements []string
}

func newIssueBundleRedactor(policy issueBundleRedactions) issueBundleRedactor {
	var replacements []string
	addReplacement := func(from string, to string) {
		from = strings.TrimSpace(from)
		if from == "" || len(from) < 2 {
			return
		}
		replacements = append(replacements, from, to)
	}
	if policy.Paths {
		if home, err := os.UserHomeDir(); err == nil {
			addReplacement(home, "<home>")
		}
		if cwd, err := os.Getwd(); err == nil {
			addReplacement(cwd, "<cwd>")
		}
		addReplacement(os.TempDir(), "<tmp>")
	}
	if policy.Env {
		for _, item := range os.Environ() {
			key, value, ok := strings.Cut(item, "=")
			if !ok || !secretLikeEnvKey(key) || len(value) < 6 {
				continue
			}
			addReplacement(value, "<redacted-env:"+key+">")
		}
	}
	return issueBundleRedactor{replacements: replacements}
}

func (r issueBundleRedactor) RedactBytes(data []byte) []byte {
	if len(r.replacements) == 0 || len(data) == 0 {
		return data
	}
	text := string(data)
	replacer := strings.NewReplacer(r.replacements...)
	return []byte(replacer.Replace(text))
}

func secretLikeEnvKey(key string) bool {
	upper := strings.ToUpper(strings.TrimSpace(key))
	if upper == "" {
		return false
	}
	for _, token := range []string{"TOKEN", "SECRET", "PASSWORD", "PASS", "AUTH", "CREDENTIAL", "PRIVATE", "API_KEY", "ACCESS_KEY"} {
		if strings.Contains(upper, token) {
			return true
		}
	}
	return false
}

func errorFromRenderReport(report renderReport) error {
	for i := len(report.Stages) - 1; i >= 0; i-- {
		stage := report.Stages[i]
		if stage.OK || strings.TrimSpace(stage.Error) == "" {
			continue
		}
		err := fmt.Errorf("%s: %s", stage.Name, stage.Error)
		switch stage.Name {
		case "compile_mvs", "write_temp_mvs":
			return markError(kindInvalidScene, err)
		case "prepare_runtime":
			return markError(kindRuntime, err)
		case "render_output", "write_mvsj", "write_mvsx", "write_report":
			return markError(kindRender, err)
		default:
			return err
		}
	}
	if report.OK {
		return nil
	}
	for i := len(report.Commands) - 1; i >= 0; i-- {
		command := report.Commands[i]
		detail := strings.TrimSpace(command.Stderr)
		if detail == "" {
			detail = strings.TrimSpace(command.Stdout)
		}
		if detail != "" {
			return fmt.Errorf("%s", detail)
		}
	}
	return fmt.Errorf("render failed without a recorded stage error")
}

func likelyCause(err error) string {
	if err == nil {
		return "run succeeded"
	}
	body := newErrorBody(err)
	return likelyCauseFromBody(&body)
}

func likelyCauseFromBody(body *errorBody) string {
	if body == nil {
		return "run succeeded"
	}
	switch errorKind(body.AgentCode) {
	case "webgl_unavailable", kindRenderer:
		return "renderer unavailable"
	case "invalid_job", kindInvalidInput, kindInvalidScene, kindValidation:
		return "invalid job or scene"
	case "network_blocked", kindNetwork:
		return "network or cache access failed"
	case kindSecurity:
		return "runtime security policy blocked the job"
	case kindServerBusy:
		return "render worker queue is busy"
	case kindCanceled:
		return "render was canceled"
	}
	switch errorKind(body.Code) {
	case kindRenderer, kindRendererABI, kindRender:
		return "renderer failed"
	case kindInvalidInput, kindInvalidScene, kindValidation:
		return "invalid job or scene"
	case kindNetwork:
		return "network or cache access failed"
	case kindSecurity, kindRuntime:
		return "runtime policy blocked the job"
	}
	return "unclassified failure"
}

func rendererStatus(report renderReport) string {
	if len(report.Commands) == 0 {
		if mode, ok := report.Diagnostics["renderer_mode"].(string); ok && mode != "" {
			return mode + " renderer did not run"
		}
		return "renderer did not run"
	}
	last := report.Commands[len(report.Commands)-1]
	if last.Skipped {
		return "dry-run skipped renderer"
	}
	status := "renderer completed"
	if last.ExitCode != 0 {
		status = fmt.Sprintf("renderer exited with code %d", last.ExitCode)
	}
	if last.Worker {
		status += fmt.Sprintf(" via worker %d", last.WorkerID)
	}
	if len(last.FallbackOf) > 0 {
		status += " after fallback"
	}
	return status
}

func diagnoseDoctorStatus(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var report doctorReport
	if err := json.Unmarshal(data, &report); err != nil {
		return ""
	}
	parts := []string{}
	if report.Renderer.Primary.Available {
		if report.Renderer.Primary.OK {
			parts = append(parts, "primary ok")
		} else if report.Renderer.Primary.Error != "" {
			parts = append(parts, "primary failed: "+singleLine(report.Renderer.Primary.Error))
		} else {
			parts = append(parts, "primary available")
		}
	} else {
		parts = append(parts, "primary unavailable")
	}
	if report.Renderer.Fallback.Available {
		if report.Renderer.Fallback.OK {
			parts = append(parts, "fallback ok")
		} else {
			parts = append(parts, "fallback available")
		}
	}
	return strings.Join(parts, ", ")
}
