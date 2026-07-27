package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rafflesia-ai/molstar/internal/job"
	"github.com/rafflesia-ai/molstar/internal/mvs"
	"github.com/rafflesia-ai/molstar/internal/recipe"
)

func (a app) recipeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "recipe",
		Short: "Create and compile friendly render recipes",
	}
	cmd.AddCommand(a.recipeInitCommand())
	cmd.AddCommand(a.recipeSchemaCommand())
	cmd.AddCommand(a.recipeValidateCommand())
	cmd.AddCommand(a.recipeExplainCommand())
	cmd.AddCommand(a.recipeCompileCommand())
	return cmd
}

func (a app) recipeSchemaCommand() *cobra.Command {
	var out string
	var info bool
	var jsonReport bool
	cmd := &cobra.Command{
		Use:   "schema",
		Short: "Print the JSON Schema for recipe specs",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("recipe schema", jsonReport, func() error {
				if len(args) != 0 {
					return markError(kindInvalidInput, fmt.Errorf("recipe schema does not accept positional arguments"))
				}
				value := any(recipe.JSONSchema())
				if info {
					value = recipe.SchemaInfo()
				}
				data, err := marshalJSON(value)
				if err != nil {
					return markError(kindInternal, err)
				}
				if err := writeBytesPath(a.stdout, out, data); err != nil {
					return markError(kindRender, err)
				}
				return nil
			})
		},
	}
	cmd.Flags().StringVarP(&out, "out", "o", "-", "schema output path; use - for stdout")
	cmd.Flags().BoolVar(&info, "info", false, "print schema versioning and compatibility metadata")
	cmd.Flags().BoolVar(&jsonReport, "json", false, "write JSON errors if the command fails")
	return cmd
}

func (a app) recipeInitCommand() *cobra.Command {
	var out string
	var format string
	var id string
	var path string
	var url string
	var provider string
	var size string
	var imageOut string
	cmd := &cobra.Command{
		Use:   "init [PRESET]",
		Short: "Write a starter recipe",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 1 {
				return markError(kindInvalidInput, fmt.Errorf("recipe init accepts at most one preset"))
			}
			preset := "ligand"
			if len(args) == 1 {
				preset = args[0]
			}
			if _, ok := presetDefinitionByName(preset); !ok {
				return markError(kindInvalidInput, fmt.Errorf("unsupported preset %q", preset))
			}
			parsedSize, err := parseSize(size)
			if err != nil {
				return markError(kindInvalidInput, err)
			}
			input := job.Input{ID: id, Provider: provider}
			if path != "" {
				input = job.Input{Path: path}
			}
			if url != "" {
				input = job.Input{URL: url}
			}
			if input.ID == "" && input.Path == "" && input.URL == "" {
				input.ID = "1cbs"
				input.Provider = provider
			}
			if input.Provider == "" && input.ID != "" {
				input.Provider = "pdbe"
			}
			if imageOut == "" {
				imageOut = defaultOutputPathForPreset(recipeInputLabel(input), preset)
			}
			r := recipe.Recipe{
				Version:    1,
				Kind:       "recipe",
				Name:       preset + " render",
				Preset:     preset,
				Input:      input,
				Runtime:    job.Runtime{Profile: "ci", Cache: ".molstar-cache", Strict: true},
				Background: "white",
				Focus:      presetDefaultFocus(preset),
				View:       "front",
				Zoom:       1.2,
				Size:       parsedSize,
				Outputs: []job.Output{{
					Type: "image",
					Path: imageOut,
					Size: parsedSize,
				}},
			}
			switch strings.ToLower(format) {
			case "", "yaml", "yml":
				data, err := marshalYAML(r)
				if err != nil {
					return markError(kindInternal, err)
				}
				return markError(kindRender, writeBytesPath(a.stdout, out, data))
			case "json":
				data, err := marshalJSON(r)
				if err != nil {
					return markError(kindInternal, err)
				}
				return markError(kindRender, writeBytesPath(a.stdout, out, data))
			default:
				return markError(kindInvalidInput, fmt.Errorf("unsupported format %q", format))
			}
		},
	}
	cmd.Flags().StringVarP(&out, "out", "o", "-", "recipe output path; use - for stdout")
	cmd.Flags().StringVar(&format, "format", "yaml", "output format: yaml or json")
	cmd.Flags().StringVar(&id, "id", "1cbs", "PDB/AlphaFold-style identifier")
	cmd.Flags().StringVar(&path, "path", "", "local input structure path")
	cmd.Flags().StringVar(&url, "url", "", "remote input structure URL")
	cmd.Flags().StringVar(&provider, "provider", "pdbe", "identifier provider")
	cmd.Flags().StringVar(&size, "size", "1200x900", "image size as WIDTHxHEIGHT")
	cmd.Flags().StringVar(&imageOut, "image-out", "", "image output path stored in the recipe")
	return cmd
}

func (a app) recipeValidateCommand() *cobra.Command {
	var jsonReport bool
	var schema bool
	cmd := &cobra.Command{
		Use:   "validate RECIPE",
		Short: "Validate a recipe and its compiled job",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("recipe validate", jsonReport, func() error {
				if err := exactArgs(args, 1, "recipe validate"); err != nil {
					return markError(kindInvalidInput, err)
				}
				data, name, err := a.readInput(args[0])
				if err != nil {
					return markError(kindInvalidInput, err)
				}
				var r recipe.Recipe
				if schema {
					r, err = recipe.LoadSchemaBytes(data, name)
				} else {
					r, err = recipe.LoadBytes(data, name)
				}
				if err != nil {
					return markError(kindValidation, err)
				}
				j, err := a.recipeToJob(r)
				if err != nil {
					return markError(kindValidation, err)
				}
				if err := j.ValidateRender(); err != nil {
					return markError(kindValidation, err)
				}
				if _, err := mvs.Compile(j); err != nil {
					return markError(kindInvalidScene, err)
				}
				if jsonReport {
					return writeJSON(a.stdout, map[string]any{"ok": true, "file": name, "preset": normalizedPreset(r.Preset), "schema": schema})
				}
				fmt.Fprintf(a.stdout, "OK     %s\n", name)
				return nil
			})
		},
	}
	cmd.Flags().BoolVar(&schema, "schema", false, "validate with the recipe JSON Schema before semantic checks")
	cmd.Flags().BoolVar(&jsonReport, "json", false, "write JSON report")
	return cmd
}

func (a app) recipeExplainCommand() *cobra.Command {
	var jsonReport bool
	var schema bool
	cmd := &cobra.Command{
		Use:   "explain RECIPE",
		Short: "Explain how a recipe compiles to a job and scene",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("recipe explain", jsonReport, func() error {
				if err := exactArgs(args, 1, "recipe explain"); err != nil {
					return markError(kindInvalidInput, err)
				}
				data, name, err := a.readInput(args[0])
				if err != nil {
					return markError(kindInvalidInput, err)
				}
				var r recipe.Recipe
				if schema {
					r, err = recipe.LoadSchemaBytes(data, name)
				} else {
					r, err = recipe.LoadBytes(data, name)
				}
				if err != nil {
					return markError(kindValidation, err)
				}
				j, err := a.recipeToJob(r)
				if err != nil {
					return markError(kindValidation, err)
				}
				if err := j.ValidateRender(); err != nil {
					return markError(kindValidation, err)
				}
				nodes, err := mvsCompileForExplain(j)
				if err != nil {
					return markError(kindInvalidScene, err)
				}
				report := map[string]any{
					"ok":     true,
					"file":   name,
					"schema": schema,
					"recipe": map[string]any{
						"name":           r.Name,
						"preset":         normalizedPreset(r.Preset),
						"input":          r.NormalizedInput(),
						"background":     firstNonEmpty(r.Background, "white"),
						"focus":          firstNonEmpty(r.Focus, presetDefaultFocus(normalizedPreset(r.Preset))),
						"view":           r.View,
						"zoom":           r.Zoom,
						"components":     len(j.Scene.Structures[0].Components),
						"outputs":        len(j.Outputs),
						"structure_type": j.Scene.Structures[0].Type,
					},
					"job": explainJobReport(j),
					"mvs": map[string]any{
						"nodes": nodes,
					},
				}
				if jsonReport {
					return writeJSON(a.stdout, report)
				}
				return writeYAML(a.stdout, report)
			})
		},
	}
	cmd.Flags().BoolVar(&schema, "schema", false, "validate with the recipe JSON Schema before explaining")
	cmd.Flags().BoolVar(&jsonReport, "json", true, "write JSON report")
	return cmd
}

func (a app) recipeCompileCommand() *cobra.Command {
	var out string
	var jsonReport bool
	var runtime runtimeFlags
	cmd := &cobra.Command{
		Use:   "compile RECIPE",
		Short: "Compile a recipe to MVSJ/MVSX or a canonical job file",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("recipe compile", jsonReport, func() error {
				if err := exactArgs(args, 1, "recipe compile"); err != nil {
					return markError(kindInvalidInput, err)
				}
				data, name, err := a.readInput(args[0])
				if err != nil {
					return markError(kindInvalidInput, err)
				}
				r, err := recipe.LoadBytes(data, name)
				if err != nil {
					return markError(kindInvalidInput, err)
				}
				j, err := a.recipeToJob(r)
				if err != nil {
					return markError(kindInvalidInput, err)
				}
				applyRuntimeFlags(cmd, &j, runtime)
				target := out
				if target == "" && args[0] != "-" {
					target = strings.TrimSuffix(args[0], filepath.Ext(args[0])) + ".mvsj"
				}
				if target == "" {
					target = "-"
				}
				ext := strings.ToLower(filepath.Ext(target))
				switch ext {
				case ".json":
					data, err := marshalJSON(j)
					if err != nil {
						return markError(kindInternal, err)
					}
					if err := writeBytesPath(a.stdout, target, data); err != nil {
						return markError(kindRender, err)
					}
					if jsonReport {
						return writeJSON(a.stdout, map[string]any{"ok": true, "output": target, "type": "job"})
					}
					return nil
				case ".yaml", ".yml":
					data, err := marshalYAML(j)
					if err != nil {
						return markError(kindInternal, err)
					}
					if err := writeBytesPath(a.stdout, target, data); err != nil {
						return markError(kindRender, err)
					}
					if jsonReport {
						return writeJSON(a.stdout, map[string]any{"ok": true, "output": target, "type": "job"})
					}
					return nil
				}
				j, runtimeReport, err := prepareJob(cmd.Context(), j)
				if err != nil {
					return markError(kindRuntime, err)
				}
				compiled, err := mvs.Compile(j)
				if err != nil {
					return markError(kindInvalidScene, err)
				}
				var outputFile *outputReport
				if strings.EqualFold(ext, ".mvsx") {
					if target == "-" {
						return markError(kindInvalidInput, fmt.Errorf("recipe compile mvsx output requires --out to be a file path"))
					}
					report, err := writeMVSXTransactional(target, j, compiled.Document)
					if err != nil {
						return markError(kindRender, err)
					}
					outputFile = &report
				} else {
					if target == "-" {
						data, err := mvs.Marshal(compiled.Document)
						if err != nil {
							return markError(kindInvalidScene, err)
						}
						if err := writeBytesPath(a.stdout, target, data); err != nil {
							return markError(kindRender, err)
						}
					} else {
						report, err := writeMVSJTransactional(target, compiled.Document)
						if err != nil {
							return markError(kindRender, err)
						}
						outputFile = &report
					}
				}
				for _, warning := range compiled.Warnings {
					fmt.Fprintln(a.stderr, "warning:", warning)
				}
				if jsonReport {
					report := map[string]any{
						"ok":            true,
						"output":        target,
						"type":          "mvs",
						"warnings":      compiled.Warnings,
						"themes":        compiled.ThemeExtensions,
						"cached_inputs": runtimeReport.CachedInputs,
					}
					if outputFile != nil {
						report["output_file"] = outputFile
					}
					return writeJSON(a.stdout, report)
				}
				if target != "-" {
					fmt.Fprintln(a.stdout, target)
				}
				return nil
			})
		},
	}
	cmd.Flags().StringVarP(&out, "out", "o", "", "output path; .mvsj/.mvsx writes a scene, .json/.yaml writes a job")
	cmd.Flags().BoolVar(&jsonReport, "json", false, "write JSON report")
	bindRuntimeFlags(cmd, &runtime)
	return cmd
}

func (a app) recipeToJob(r recipe.Recipe) (job.Job, error) {
	preset := normalizedPreset(r.Preset)
	presetDef, ok := presetDefinitionByName(preset)
	if !ok {
		return job.Job{}, fmt.Errorf("unsupported preset %q", r.Preset)
	}
	components := r.Components
	if len(components) == 0 {
		components = presetDef.Components
	}
	focus := firstNonEmpty(r.Focus, presetDef.Focus)
	input := r.NormalizedInput()
	background := firstNonEmpty(r.Background, "white")
	size := r.Size
	if len(size) == 0 {
		size = []int{1200, 900}
	}
	outputs := r.Outputs
	if len(outputs) == 0 {
		outputs = []job.Output{{
			Type: "image",
			Path: defaultOutputPathForPreset(recipeInputLabel(input), preset),
			Size: size,
		}}
	}
	for i := range outputs {
		if len(outputs[i].Size) == 0 && outputs[i].NormalizedType() == "image" {
			outputs[i].Size = size
		}
	}
	runtime := r.Runtime
	if !runtime.Strict {
		runtime.Strict = true
	}
	return job.Job{
		Version: 1,
		Runtime: runtime,
		Inputs: map[string]job.Input{
			"input": input,
		},
		Scene: job.Scene{
			Canvas: job.Canvas{Background: background},
			Structures: []job.Structure{{
				Ref:        "structure",
				Source:     "input",
				Type:       r.StructureType,
				Assembly:   firstNonEmpty(r.Assembly, input.Assembly),
				Components: components,
			}},
			Camera: job.Camera{Focus: focus, View: r.View, Zoom: r.Zoom},
		},
		Outputs: outputs,
	}, nil
}

func normalizedPreset(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "default"
	}
	return value
}

func presetDefaultFocus(preset string) string {
	def, ok := presetDefinitionByName(preset)
	if !ok {
		return ""
	}
	return def.Focus
}

func recipeInputLabel(input job.Input) string {
	switch {
	case input.ID != "":
		return input.ID
	case input.Path != "":
		return input.Path
	case input.URL != "":
		return input.URL
	default:
		return "scene"
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (a app) loadJobOrRecipeBytes(data []byte, name string, renderRequired bool) (job.Job, error) {
	var jobErr error
	if renderRequired {
		if j, err := job.LoadRenderBytes(data, name); err == nil {
			return j, nil
		} else {
			jobErr = err
		}
	} else {
		if j, err := job.LoadBytes(data, name); err == nil {
			return j, nil
		} else {
			jobErr = err
		}
	}
	r, recipeErr := recipe.LoadBytes(data, name)
	if recipeErr != nil || !r.LooksLikeRecipe() {
		return job.Job{}, jobErr
	}
	j, err := a.recipeToJob(r)
	if err != nil {
		return job.Job{}, err
	}
	if renderRequired {
		return j, j.ValidateRender()
	}
	return j, j.ValidateScene()
}
