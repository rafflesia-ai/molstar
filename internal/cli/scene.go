package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rafflesia-ai/molstar/internal/mvs"
	"github.com/rafflesia-ai/molstar/internal/render"
)

func (a app) sceneCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scene",
		Short: "Compile and validate scene files",
	}
	cmd.AddCommand(a.sceneCompileCommand())
	cmd.AddCommand(a.sceneValidateCommand())
	return cmd
}

func (a app) sceneCompileCommand() *cobra.Command {
	var out string
	var jsonReport bool
	var runtime runtimeFlags
	cmd := &cobra.Command{
		Use:   "compile JOB",
		Short: "Compile a headless job YAML/JSON file to MVSJ",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("scene compile", jsonReport, func() error {
				if err := exactArgs(args, 1, "scene compile"); err != nil {
					return markError(kindInvalidInput, err)
				}
				data, name, err := a.readInput(args[0])
				if err != nil {
					return markError(kindInvalidInput, err)
				}
				j, err := a.loadJobOrRecipeBytes(data, name, false)
				if err != nil {
					return markError(kindInvalidInput, err)
				}
				applyRuntimeFlags(cmd, &j, runtime)
				j, runtimeReport, err := prepareJob(cmd.Context(), j)
				if err != nil {
					return markError(kindRuntime, err)
				}
				compiled, err := mvs.Compile(j)
				if err != nil {
					return markError(kindInvalidScene, err)
				}
				target := out
				if target == "" && args[0] != "-" {
					target = strings.TrimSuffix(args[0], filepath.Ext(args[0])) + ".mvsj"
				}
				if jsonReport && (target == "" || target == "-") {
					return markError(kindInvalidInput, fmt.Errorf("scene compile --json requires --out to be a file path"))
				}
				var compiledOutputReport *outputReport
				if strings.EqualFold(filepath.Ext(target), ".mvsx") {
					if target == "" || target == "-" {
						return markError(kindInvalidInput, fmt.Errorf("scene compile mvsx output requires --out to be a file path"))
					}
					report, err := writeMVSXTransactional(target, j, compiled.Document)
					if err != nil {
						return markError(kindRender, err)
					}
					compiledOutputReport = &report
				} else {
					data, err = mvs.Marshal(compiled.Document)
					if err != nil {
						return markError(kindInvalidScene, err)
					}
					if target == "" || target == "-" {
						if err := writeBytesPath(a.stdout, target, data); err != nil {
							return markError(kindRender, err)
						}
					} else {
						report, err := writeMVSJTransactional(target, compiled.Document)
						if err != nil {
							return markError(kindRender, err)
						}
						compiledOutputReport = &report
					}
				}
				for _, warning := range compiled.Warnings {
					fmt.Fprintln(a.stderr, "warning:", warning)
				}
				if jsonReport {
					report := map[string]any{
						"ok":            true,
						"output":        target,
						"warnings":      compiled.Warnings,
						"themes":        compiled.ThemeExtensions,
						"cached_inputs": runtimeReport.CachedInputs,
					}
					if compiledOutputReport != nil {
						report["output_file"] = compiledOutputReport
					}
					return writeJSON(a.stdout, report)
				}
				if target != "" && target != "-" {
					fmt.Fprintln(a.stdout, target)
				}
				return nil
			})
		},
	}
	cmd.Flags().StringVarP(&out, "out", "o", "", "output .mvsj path; use - for stdout")
	cmd.Flags().BoolVar(&jsonReport, "json", false, "write a machine-readable report to stdout")
	bindRuntimeFlags(cmd, &runtime)
	return cmd
}

func (a app) sceneValidateCommand() *cobra.Command {
	var noExtra bool
	var validateCommand string
	var jsonReport bool
	cmd := &cobra.Command{
		Use:   "validate FILE",
		Short: "Validate a job or MVSJ scene",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("scene validate", jsonReport, func() error {
				if err := exactArgs(args, 1, "scene validate"); err != nil {
					return markError(kindInvalidInput, err)
				}
				path := args[0]
				if path == "-" {
					data, name, err := a.readInput(path)
					if err != nil {
						return markError(kindInvalidInput, err)
					}
					if mvs.IsDocumentBytes(data) {
						scenePath, cleanup, err := writeTempMVS(data)
						if err != nil {
							return markError(kindInvalidInput, err)
						}
						defer cleanup()
						if err := a.validateMVS(cmd, scenePath, noExtra, validateCommand); err != nil {
							return err
						}
						return a.writeValidateOK(jsonReport, name)
					}
					j, err := a.loadJobOrRecipeBytes(data, name, false)
					if err != nil {
						return markError(kindInvalidInput, err)
					}
					if _, err := mvs.Compile(j); err != nil {
						return markError(kindInvalidScene, err)
					}
					return a.writeValidateOK(jsonReport, name)
				}
				if isMVSPath(path) {
					if err := a.validateMVS(cmd, path, noExtra, validateCommand); err != nil {
						return err
					}
					return a.writeValidateOK(jsonReport, path)
				}
				data, name, err := a.readInput(path)
				if err != nil {
					return markError(kindInvalidInput, err)
				}
				j, err := a.loadJobOrRecipeBytes(data, name, false)
				if err != nil {
					return markError(kindInvalidInput, err)
				}
				if _, err := mvs.Compile(j); err != nil {
					return markError(kindInvalidScene, err)
				}
				return a.writeValidateOK(jsonReport, path)
			})
		},
	}
	cmd.Flags().BoolVar(&noExtra, "no-extra", false, "treat extra MVS node params as validation issues")
	cmd.Flags().StringVar(&validateCommand, "validate-command", "", "validator command override")
	cmd.Flags().BoolVar(&jsonReport, "json", false, "write a machine-readable report to stdout")
	return cmd
}

func (a app) validateMVS(cmd *cobra.Command, path string, noExtra bool, validateCommand string) error {
	runner := render.NewMolstar()
	runner.Stdout = nil
	runner.Stderr = a.stderr
	if validateCommand != "" {
		runner.ValidateCommand = strings.Fields(validateCommand)
	}
	if _, err := runner.ValidateMVS(cmd.Context(), path, noExtra); err != nil {
		return markError(kindValidation, err)
	}
	return nil
}

func (a app) writeValidateOK(jsonReport bool, path string) error {
	if jsonReport {
		return writeJSON(a.stdout, map[string]any{
			"ok":   true,
			"file": path,
		})
	}
	fmt.Fprintf(a.stdout, "OK     %s\n", path)
	return nil
}
