package cli

import (
	"context"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rafflesia-ai/molstar/internal/job"
	"github.com/rafflesia-ai/molstar/internal/mvs"
)

//go:embed testdata/compat/*
var compatFixtureFS embed.FS

type compatCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
	Error  string `json:"error,omitempty"`
}

type compatFixture struct {
	Name string
	Job  job.Job
	Data []byte
	Path string
}

func (a app) compatCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "compat",
		Short: "Run local compatibility corpus checks",
	}
	cmd.AddCommand(a.compatCheckCommand())
	return cmd
}

func (a app) compatCheckCommand() *cobra.Command {
	var jsonReport bool
	var renderProbe bool
	var outDir string
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Check the local headless job/MVS/render compatibility corpus",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("compat check", jsonReport, func() error {
				if len(args) != 0 {
					return markError(kindInvalidInput, fmt.Errorf("compat check does not accept positional arguments"))
				}
				checks, outputDir := a.runCompatChecks(cmd.Context(), renderProbe, outDir)
				ok := true
				for _, check := range checks {
					if !check.OK {
						ok = false
					}
				}
				report := map[string]any{"ok": ok, "checks": checks, "output_dir": outputDir}
				if jsonReport {
					if err := writeJSON(a.stdout, report); err != nil {
						return markError(kindInternal, err)
					}
					if !ok {
						return alreadyReported(markError(kindValidation, fmt.Errorf("compatibility checks failed")))
					}
					return nil
				}
				for _, check := range checks {
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
				if strings.TrimSpace(outDir) != "" {
					fmt.Fprintf(a.stdout, "artifacts %s\n", outputDir)
				}
				if !ok {
					return markError(kindValidation, fmt.Errorf("compatibility checks failed"))
				}
				return nil
			})
		},
	}
	cmd.Flags().BoolVar(&jsonReport, "json", false, "write JSON report")
	cmd.Flags().BoolVar(&renderProbe, "render", false, "include the real local render probe")
	cmd.Flags().StringVar(&outDir, "out-dir", "", "directory for compatibility artifacts; defaults to a temporary directory")
	return cmd
}

func (a app) runCompatChecks(ctx context.Context, renderProbe bool, outDir string) ([]compatCheck, string) {
	dir, cleanup, err := compatOutputDir(outDir)
	if err != nil {
		return []compatCheck{{Name: "output_dir", OK: false, Error: err.Error()}}, ""
	}
	defer cleanup()
	fixtures, err := loadCompatFixtures(dir)
	if err != nil {
		return []compatCheck{{Name: "fixture", OK: false, Error: err.Error()}}, dir
	}
	checks := []compatCheck{}
	for _, fixture := range fixtures {
		fixture := fixture
		checks = append(checks,
			checkCompat(fixture.Name+" job schema", func() error {
				return job.ValidateSchemaBytes(fixture.Data, fixture.Path)
			}),
			checkCompat(fixture.Name+" compile mvs", func() error {
				_, err := mvs.Compile(fixture.Job)
				return err
			}),
			checkCompat(fixture.Name+" write mvsx", func() error {
				compiled, err := mvs.Compile(fixture.Job)
				if err != nil {
					return err
				}
				_, err = writeMVSXTransactional(filepath.Join(dir, fixture.Name+".mvsx"), fixture.Job, compiled.Document)
				return err
			}),
			checkCompat(fixture.Name+" inspect fallback", func() error {
				_, err := a.runInspect(ctx, fixture.Path, &inspectFlags{jsonReport: true, semantic: "false"}, a.rootCommand())
				return err
			}),
		)
	}
	if renderProbe && len(fixtures) > 0 {
		fixture := fixtures[0]
		checks = append(checks, checkCompat("render probe", func() error {
			report, err := a.executeRenderJob(ctx, "compat", fixture.Job, "", false)
			if err != nil {
				return err
			}
			if len(report.OutputFiles) == 0 || !report.OutputFiles[0].Verified {
				return fmt.Errorf("render output was not verified")
			}
			return nil
		}))
		checks = append(checks, checkCompat("semantic inspect", func() error {
			report, err := a.runInspect(ctx, fixture.Path, &inspectFlags{jsonReport: true, semantic: "true", strictSemantic: true}, a.rootCommand())
			if err != nil {
				return err
			}
			semantic, ok := report["semantic"].(map[string]any)
			if !ok || semantic["ok"] != true {
				return fmt.Errorf("semantic inspect did not report ok")
			}
			return nil
		}))
	}
	return checks, dir
}

func compatOutputDir(value string) (string, func(), error) {
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
	dir, err := os.MkdirTemp("", "headlessmolstar-compat-*")
	if err != nil {
		return "", func() {}, err
	}
	return dir, func() { _ = os.RemoveAll(dir) }, nil
}

func loadCompatFixtures(dir string) ([]compatFixture, error) {
	model, err := compatFixtureFS.ReadFile("testdata/compat/one.cif")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "one.cif"), model, 0o644); err != nil {
		return nil, err
	}
	entries, err := compatFixtureFS.ReadDir("testdata/compat")
	if err != nil {
		return nil, err
	}
	var fixtures []compatFixture
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".job.json") {
			continue
		}
		raw, err := compatFixtureFS.ReadFile("testdata/compat/" + entry.Name())
		if err != nil {
			return nil, err
		}
		name := strings.TrimSuffix(entry.Name(), ".job.json")
		data := []byte(strings.ReplaceAll(string(raw), "${FIXTURE_DIR}", filepath.ToSlash(dir)))
		path := filepath.Join(dir, entry.Name())
		if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
			return nil, err
		}
		j, err := job.LoadRenderBytes(data, entry.Name())
		if err != nil {
			return nil, err
		}
		fixtures = append(fixtures, compatFixture{Name: name, Job: j, Data: data, Path: path})
	}
	return fixtures, nil
}

func checkCompat(name string, run func() error) compatCheck {
	if err := run(); err != nil {
		return compatCheck{Name: name, OK: false, Error: err.Error()}
	}
	return compatCheck{Name: name, OK: true}
}
