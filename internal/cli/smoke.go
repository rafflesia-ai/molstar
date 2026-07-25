package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type smokeReport struct {
	OK          bool          `json:"ok"`
	OutDir      string        `json:"out_dir"`
	Checks      []doctorCheck `json:"checks"`
	GeneratedAt string        `json:"generated_at"`
}

func (a app) smokeCommand() *cobra.Command {
	var outDir string
	var jsonReport bool
	cmd := &cobra.Command{
		Use:   "smoke",
		Short: "Run a compact post-install smoke test",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("smoke", jsonReport, func() error {
				if len(args) != 0 {
					return markError(kindInvalidInput, fmt.Errorf("smoke does not accept positional arguments"))
				}
				report := a.runSmoke(cmd.Context(), outDir)
				if jsonReport {
					if err := writeJSON(a.stdout, report); err != nil {
						return markError(kindInternal, err)
					}
					if !report.OK {
						return alreadyReported(markError(kindValidation, fmt.Errorf("smoke failed")))
					}
					return nil
				}
				for _, check := range report.Checks {
					status := "OK"
					if !check.OK {
						status = "FAIL"
					}
					fmt.Fprintf(a.stdout, "%-5s %s", status, check.Name)
					if check.Detail != "" {
						fmt.Fprintf(a.stdout, " (%s)", check.Detail)
					}
					if check.Error != "" {
						fmt.Fprintf(a.stdout, " - %s", singleLine(check.Error))
					}
					fmt.Fprintln(a.stdout)
				}
				if !report.OK {
					return markError(kindValidation, fmt.Errorf("smoke failed"))
				}
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&outDir, "out-dir", "", "directory for smoke artifacts; defaults to a temporary directory")
	cmd.Flags().BoolVar(&jsonReport, "json", false, "write machine-readable output")
	return cmd
}

func (a app) runSmoke(ctx context.Context, outDir string) smokeReport {
	cleanup := func() {}
	if strings.TrimSpace(outDir) == "" {
		dir, err := os.MkdirTemp("", "headlessmolstar-smoke-*")
		if err != nil {
			return smokeReport{OK: false, Checks: []doctorCheck{{Name: "tempdir", OK: false, Error: err.Error()}}}
		}
		outDir = dir
		cleanup = func() { _ = os.RemoveAll(dir) }
	}
	defer cleanup()
	_ = os.MkdirAll(outDir, 0o755)
	report := smokeReport{OK: true, OutDir: outDir, GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	add := func(check doctorCheck) {
		report.Checks = append(report.Checks, check)
		if !check.OK {
			report.OK = false
		}
	}
	add(commandSmokeCheck(ctx, "doctor", "molstar", "doctor", "--skip-probe", "--json"))
	modelPath := filepath.Join(outDir, "one.cif")
	renderPath := filepath.Join(outDir, "smoke.png")
	if err := os.WriteFile(modelPath, []byte(oneAtomCIF), 0o644); err != nil {
		add(doctorCheck{Name: "write fixture", OK: false, Error: err.Error()})
		return report
	}
	j := localSmokeJob(modelPath, renderPath)
	renderReport, err := a.executeRenderJob(ctx, "smoke", j, "", false)
	check := doctorCheck{Name: "render demo", OK: err == nil}
	if err != nil {
		check.Error = err.Error()
	} else if len(renderReport.OutputFiles) == 0 || !renderReport.OutputFiles[0].Verified {
		check.OK = false
		check.Error = "render output was not verified"
	} else {
		check.Detail = fmt.Sprintf("%dx%d PNG", renderReport.OutputFiles[0].Width, renderReport.OutputFiles[0].Height)
	}
	add(check)
	add(commandSmokeCheck(ctx, "docs", "molstar", "docs", "--out", filepath.Join(outDir, "docs")))
	add(commandSmokeCheck(ctx, "completion", "molstar", "completion", "bash"))
	add(commandSmokeCheck(ctx, "mvs compile", "molstar", "scene", "compile", writeSmokeJobFile(outDir, j), "--out", filepath.Join(outDir, "scene.mvsj"), "--json"))
	return report
}

func commandSmokeCheck(ctx context.Context, name string, command string, args ...string) doctorCheck {
	runCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if command == "molstar" {
		if _, err := exec.LookPath(command); err != nil {
			if current, exeErr := os.Executable(); exeErr == nil {
				command = current
			}
		}
	}
	cmd := exec.CommandContext(runCtx, command, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return doctorCheck{Name: name, OK: false, Error: singleLine(string(output) + " " + err.Error())}
	}
	return doctorCheck{Name: name, OK: true}
}

func writeSmokeJobFile(dir string, j any) string {
	path := filepath.Join(dir, "smoke.job.json")
	_ = writeJSONFile(path, j)
	return path
}
