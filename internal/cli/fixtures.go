package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/sacha-ichbiah/molstar/internal/job"
	"github.com/sacha-ichbiah/molstar/internal/mvs"
)

type fixtureDefinition struct {
	Name        string   `json:"name" yaml:"name"`
	Description string   `json:"description" yaml:"description"`
	Network     bool     `json:"network" yaml:"network"`
	Outputs     []string `json:"outputs,omitempty" yaml:"outputs,omitempty"`
}

type fixtureResult struct {
	Name       string                `json:"name"`
	OK         bool                  `json:"ok"`
	Network    bool                  `json:"network"`
	Outputs    []string              `json:"outputs,omitempty"`
	Golden     []fixtureGoldenResult `json:"golden,omitempty"`
	Error      string                `json:"error,omitempty"`
	DurationMS int64                 `json:"duration_ms"`
}

type fixtureGoldenResult struct {
	Output string `json:"output"`
	Type   string `json:"type"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
	Error  string `json:"error,omitempty"`
}

func (a app) fixturesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fixtures",
		Short: "Run the headless CLI fixture corpus",
	}
	cmd.AddCommand(a.fixturesListCommand())
	cmd.AddCommand(a.fixturesVerifyCommand())
	return cmd
}

func (a app) fixturesListCommand() *cobra.Command {
	var jsonReport bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List fixture corpus entries",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("fixtures list", jsonReport, func() error {
				if len(args) != 0 {
					return markError(kindInvalidInput, fmt.Errorf("fixtures list does not accept positional arguments"))
				}
				fixtures := allFixtureDefinitions()
				if jsonReport {
					return writeJSON(a.stdout, map[string]any{"ok": true, "fixtures": fixtures})
				}
				for _, fixture := range fixtures {
					scope := "local"
					if fixture.Network {
						scope = "network"
					}
					fmt.Fprintf(a.stdout, "%-14s %-7s %s\n", fixture.Name, scope, fixture.Description)
				}
				return nil
			})
		},
	}
	cmd.Flags().BoolVar(&jsonReport, "json", false, "write JSON report")
	return cmd
}

func (a app) fixturesVerifyCommand() *cobra.Command {
	var outDir string
	var includeNetwork bool
	var dryRun bool
	var keepGoing bool
	var golden bool
	var jsonReport bool
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Verify the fixture corpus end to end",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("fixtures verify", jsonReport, func() error {
				if len(args) != 0 {
					return markError(kindInvalidInput, fmt.Errorf("fixtures verify does not accept positional arguments"))
				}
				report, err := a.runFixtureVerification(cmd.Context(), outDir, includeNetwork, dryRun, keepGoing, golden)
				if jsonReport {
					if writeErr := writeJSON(a.stdout, report); writeErr != nil {
						return markError(kindInternal, writeErr)
					}
					if err != nil {
						return alreadyReported(err)
					}
					return nil
				}
				for _, result := range report["fixtures"].([]fixtureResult) {
					status := "OK"
					if !result.OK {
						status = "FAIL"
					}
					fmt.Fprintf(a.stdout, "%-5s %s\n", status, result.Name)
					for _, golden := range result.Golden {
						goldenStatus := "OK"
						if !golden.OK {
							goldenStatus = "FAIL"
						}
						fmt.Fprintf(a.stdout, "      %-5s golden %s", goldenStatus, golden.Output)
						if golden.Detail != "" {
							fmt.Fprintf(a.stdout, " (%s)", golden.Detail)
						}
						if golden.Error != "" {
							fmt.Fprintf(a.stdout, " - %s", singleLine(golden.Error))
						}
						fmt.Fprintln(a.stdout)
					}
				}
				return err
			})
		},
	}
	cmd.Flags().StringVar(&outDir, "out-dir", "outputs/fixtures", "directory for fixture outputs")
	cmd.Flags().BoolVar(&includeNetwork, "network", false, "include public remote fixtures that require network access")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the planned fixture set without rendering")
	cmd.Flags().BoolVar(&golden, "golden", false, "verify deterministic fixture output metadata and visual hashes")
	cmd.Flags().BoolVar(&keepGoing, "keep-going", false, "continue after a fixture fails")
	cmd.Flags().BoolVar(&jsonReport, "json", false, "write JSON report")
	return cmd
}

func allFixtureDefinitions() []fixtureDefinition {
	return []fixtureDefinition{
		{Name: "demo", Description: "Render the built-in one-atom local demo.", Outputs: []string{"demo.png"}},
		{Name: "local-cif", Description: "Render a local mmCIF job with locked path permissions.", Outputs: []string{"local-cif.png"}},
		{Name: "mvsx", Description: "Compile a local job into a bundled MVSX archive.", Outputs: []string{"local-scene.mvsx"}},
		{Name: "ligand", Description: "Render the public 1CBS ligand recipe.", Network: true, Outputs: []string{"ligand.png"}},
		{Name: "surface", Description: "Render the public 1CBS surface recipe.", Network: true, Outputs: []string{"surface.png"}},
		{Name: "confidence", Description: "Render the public AlphaFold confidence recipe.", Network: true, Outputs: []string{"confidence.png"}},
	}
}

func (a app) runFixtureVerification(ctx context.Context, outDir string, includeNetwork bool, dryRun bool, keepGoing bool, golden bool) (map[string]any, error) {
	if outDir == "" {
		outDir = "outputs/fixtures"
	}
	if !dryRun {
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			return nil, markError(kindInvalidInput, err)
		}
	}
	definitions := []fixtureDefinition{}
	for _, definition := range allFixtureDefinitions() {
		if definition.Network && !includeNetwork {
			continue
		}
		definitions = append(definitions, definition)
	}
	results := []fixtureResult{}
	ok := true
	for _, definition := range definitions {
		start := time.Now()
		result := fixtureResult{Name: definition.Name, Network: definition.Network, Outputs: fixtureOutputPaths(outDir, definition), OK: true}
		if !dryRun {
			if err := a.runFixture(ctx, outDir, definition); err != nil {
				result.OK = false
				result.Error = err.Error()
				ok = false
			}
			if result.OK && golden {
				goldenResults, err := verifyFixtureGolden(outDir, definition)
				result.Golden = goldenResults
				if err != nil {
					result.OK = false
					result.Error = err.Error()
					ok = false
				}
			}
		}
		result.DurationMS = time.Since(start).Milliseconds()
		results = append(results, result)
		if !result.OK && !keepGoing {
			break
		}
	}
	report := map[string]any{
		"ok":       ok,
		"out_dir":  outDir,
		"network":  includeNetwork,
		"dry_run":  dryRun,
		"golden":   golden,
		"fixtures": results,
	}
	if !ok {
		return report, markError(kindRender, fmt.Errorf("fixture verification failed"))
	}
	return report, nil
}

func verifyFixtureGolden(outDir string, definition fixtureDefinition) ([]fixtureGoldenResult, error) {
	switch definition.Name {
	case "demo", "local-cif":
		path := filepath.Join(outDir, definition.Outputs[0])
		output := job.Output{Type: "image", Path: path, Size: []int{96, 72}}
		report, err := verifyOutput(path, output)
		result := fixtureGoldenResult{Output: path, Type: "image", OK: err == nil}
		if err == nil {
			err = checkOutputReportAgainstGolden(report, demoVisualGolden)
		}
		if err != nil {
			result.OK = false
			result.Error = err.Error()
			return []fixtureGoldenResult{result}, err
		}
		result.Detail = "average_hash=" + report.AverageHash
		return []fixtureGoldenResult{result}, nil
	case "mvsx":
		path := filepath.Join(outDir, "local-scene.mvsx")
		err := mvs.ValidateMVSX(path, 0)
		result := fixtureGoldenResult{Output: path, Type: "mvsx", OK: err == nil}
		if err != nil {
			result.Error = err.Error()
			return []fixtureGoldenResult{result}, err
		}
		report, err := exportedFileReport(path, "mvsx")
		if err != nil {
			result.OK = false
			result.Error = err.Error()
			return []fixtureGoldenResult{result}, err
		}
		result.Detail = fmt.Sprintf("sha256=%s bytes=%d", report.SHA256, report.Bytes)
		return []fixtureGoldenResult{result}, nil
	default:
		results := make([]fixtureGoldenResult, 0, len(definition.Outputs))
		for _, output := range definition.Outputs {
			path := filepath.Join(outDir, output)
			report, err := verifyOutput(path, job.Output{Type: outputTypeFromPath(path), Path: path, Size: []int{320, 240}})
			result := fixtureGoldenResult{Output: path, Type: outputTypeFromPath(path), OK: err == nil}
			if err != nil {
				result.Error = err.Error()
				results = append(results, result)
				return results, err
			}
			result.Detail = fmt.Sprintf("average_hash=%s", report.AverageHash)
			results = append(results, result)
		}
		return results, nil
	}
}

func fixtureOutputPaths(outDir string, definition fixtureDefinition) []string {
	paths := make([]string, 0, len(definition.Outputs))
	for _, output := range definition.Outputs {
		paths = append(paths, filepath.Join(outDir, output))
	}
	return paths
}

func (a app) runFixture(ctx context.Context, outDir string, definition fixtureDefinition) error {
	switch definition.Name {
	case "demo":
		return a.runRender(ctx, "demo", &renderFlags{
			out:        filepath.Join(outDir, "demo.png"),
			size:       "96x72",
			background: "white",
			demo:       true,
			quiet:      true,
		}, a.rootCommand())
	case "local-cif":
		jobPath, err := writeLocalFixtureJob(outDir, filepath.Join(outDir, "local-cif.png"))
		if err != nil {
			return err
		}
		return a.runRender(ctx, jobPath, &renderFlags{size: "96x72", quiet: true}, a.rootCommand())
	case "mvsx":
		jobPath, err := writeLocalFixtureJob(outDir, filepath.Join(outDir, "local-scene.mvsx"))
		if err != nil {
			return err
		}
		data, _, err := a.readInput(jobPath)
		if err != nil {
			return err
		}
		j, err := a.loadJobOrRecipeBytes(data, jobPath, false)
		if err != nil {
			return err
		}
		prepared, _, err := prepareJob(ctx, j)
		if err != nil {
			return err
		}
		compiled, err := mvs.Compile(prepared)
		if err != nil {
			return err
		}
		_, err = writeMVSXTransactional(filepath.Join(outDir, "local-scene.mvsx"), prepared, compiled.Document)
		return err
	case "ligand":
		return a.runRender(ctx, "examples/ligand.recipe.yaml", &renderFlags{out: filepath.Join(outDir, "ligand.png"), size: "320x240", quiet: true, runtime: runtimeFlags{cache: filepath.Join(outDir, "cache")}}, a.rootCommand())
	case "surface":
		return a.runRender(ctx, "examples/surface.recipe.yaml", &renderFlags{out: filepath.Join(outDir, "surface.png"), size: "320x240", quiet: true, runtime: runtimeFlags{cache: filepath.Join(outDir, "cache")}}, a.rootCommand())
	case "confidence":
		return a.runRender(ctx, "examples/alphafold-confidence.recipe.yaml", &renderFlags{out: filepath.Join(outDir, "confidence.png"), size: "320x240", quiet: true, runtime: runtimeFlags{cache: filepath.Join(outDir, "cache")}}, a.rootCommand())
	default:
		return fmt.Errorf("unknown fixture %q", definition.Name)
	}
}

func writeLocalFixtureJob(dir string, output string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	cifPath := filepath.Join(dir, "local.cif")
	if err := os.WriteFile(cifPath, []byte(oneAtomCIF), 0o644); err != nil {
		return "", err
	}
	j := job.Job{
		Version: 1,
		Runtime: job.Runtime{Profile: "locked", Strict: true, AllowPaths: []string{dir}},
		Inputs: map[string]job.Input{
			"input": {Path: cifPath, Format: "mmcif"},
		},
		Scene: job.Scene{
			Canvas: job.Canvas{Background: "white"},
			Structures: []job.Structure{{
				Ref:    "local",
				Source: "input",
				Components: []job.Component{{
					Ref:            "all",
					Select:         "all",
					Representation: job.Representation{Type: "spacefill", Color: "#cc3399"},
				}},
			}},
			Camera: job.Camera{Focus: "all"},
		},
		Outputs: []job.Output{{Type: outputTypeFromPath(output), Path: output, Size: []int{96, 72}}},
	}
	path := filepath.Join(dir, "local.job.json")
	return path, writeJSONFile(path, j)
}
