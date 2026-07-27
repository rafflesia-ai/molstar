package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/rafflesia-ai/molstar/internal/job"
)

type quickstartReport struct {
	OK          bool          `json:"ok"`
	OutDir      string        `json:"out_dir"`
	Doctor      *doctorReport `json:"doctor,omitempty"`
	Render      *renderReport `json:"render,omitempty"`
	Next        []string      `json:"next"`
	GeneratedAt string        `json:"generated_at"`
}

func (a app) quickstartCommand() *cobra.Command {
	var outDir string
	var jsonReport bool
	var openOutput bool
	cmd := &cobra.Command{
		Use:   "quickstart",
		Short: "Verify the local install and render a first demo image",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("quickstart", jsonReport, func() error {
				if len(args) != 0 {
					return markError(kindInvalidInput, fmt.Errorf("quickstart does not accept positional arguments"))
				}
				report, err := a.runQuickstart(cmd.Context(), outDir, openOutput)
				if jsonReport {
					if writeErr := writeJSON(a.stdout, report); writeErr != nil {
						return markError(kindInternal, writeErr)
					}
					if err != nil {
						return alreadyReported(err)
					}
					return nil
				}
				a.writeQuickstartSummary(report)
				return err
			})
		},
	}
	cmd.Flags().StringVar(&outDir, "out-dir", "molstar-quickstart", "directory for quickstart outputs")
	cmd.Flags().StringVar(&outDir, "out", "molstar-quickstart", "alias for --out-dir")
	cmd.Flags().BoolVar(&jsonReport, "json", false, "write machine-readable output")
	cmd.Flags().BoolVar(&openOutput, "open", false, "open the demo image after rendering")
	return cmd
}

func (a app) runQuickstart(ctx context.Context, outDir string, openOutput bool) (quickstartReport, error) {
	if strings.TrimSpace(outDir) == "" {
		outDir = "molstar-quickstart"
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return quickstartReport{}, markError(kindInvalidInput, err)
	}
	report := quickstartReport{
		OK:          true,
		OutDir:      outDir,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Next: []string{
			"molstar render 1cbs --preset ligand --cache .molstar-cache --out 1cbs.png --show-report",
			"molstar render --demo --explain",
			"molstar examples",
		},
	}
	doctorOutput, _, doctorErr := a.runSubcommand(ctx, "doctor", "--skip-probe", "--json")
	if doctorErr == nil {
		var doctor doctorReport
		if err := json.Unmarshal([]byte(doctorOutput), &doctor); err == nil {
			report.Doctor = &doctor
			report.OK = report.OK && doctor.OK
		}
	} else {
		report.OK = false
	}
	renderOut := filepath.Join(outDir, "demo.png")
	flags := &renderFlags{out: renderOut, size: "96x72", quiet: true}
	j, cleanup, err := demoJob(flags)
	if err != nil {
		report.OK = false
		return report, markError(kindInvalidInput, err)
	}
	defer cleanup()
	renderReport, renderErr := a.executeRenderJob(ctx, "quickstart", j, "", false)
	if path, err := writeRunLog("quickstart", renderReport); err == nil {
		renderReport.RunLog = path
	}
	report.Render = &renderReport
	if renderErr != nil {
		report.OK = false
		return report, renderErr
	}
	if openOutput {
		if err := openFirstRenderableOutput(renderReport.OutputFiles); err != nil {
			report.OK = false
			return report, markError(kindRuntime, err)
		}
	}
	if !report.OK {
		return report, markError(kindDoctor, fmt.Errorf("quickstart checks failed"))
	}
	return report, nil
}

func (a app) writeQuickstartSummary(report quickstartReport) {
	status := "ok"
	if !report.OK {
		status = "failed"
	}
	fmt.Fprintf(a.stdout, "quickstart\t%s\n", status)
	fmt.Fprintf(a.stdout, "out-dir\t%s\n", report.OutDir)
	if report.Doctor != nil {
		fmt.Fprintf(a.stdout, "doctor\t%s\n", renderStatusWord(report.Doctor.OK))
	}
	if report.Render != nil {
		_ = a.writeRenderSummary(*report.Render)
	}
	fmt.Fprintln(a.stdout, "next:")
	for _, command := range report.Next {
		fmt.Fprintf(a.stdout, "  %s\n", command)
	}
}

func (a app) runSubcommand(ctx context.Context, args ...string) (string, string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	sub := app{stdin: strings.NewReader(""), stdout: &stdout, stderr: &stderr, args: args}
	root := sub.rootCommand()
	root.SetArgs(args)
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	err := root.ExecuteContext(ctx)
	return stdout.String(), stderr.String(), err
}

func localSmokeJob(path string, out string) job.Job {
	return job.Job{
		Version: 1,
		Runtime: job.Runtime{Strict: true, AllowPaths: []string{filepath.Dir(path)}},
		Inputs: map[string]job.Input{
			"input": {Path: path, Format: "mmcif"},
		},
		Scene: job.Scene{
			Canvas: job.Canvas{Background: "white"},
			Structures: []job.Structure{{
				Ref:    "model",
				Source: "input",
				Components: []job.Component{{
					Ref:    "all",
					Select: "all",
					Representation: job.Representation{
						Type:  "spacefill",
						Color: "#cc3399",
					},
				}},
			}},
			Camera: job.Camera{Focus: "all"},
		},
		Outputs: []job.Output{{Type: "image", Path: out, Size: []int{64, 64}}},
	}
}
