package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rafflesia-ai/molstar/internal/job"
	"github.com/rafflesia-ai/molstar/internal/mvs"
)

func (a app) jobCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "job",
		Short: "Inspect and generate headless job specs",
	}
	cmd.AddCommand(a.jobSchemaCommand())
	cmd.AddCommand(a.jobInitCommand())
	cmd.AddCommand(a.jobExamplesCommand())
	cmd.AddCommand(a.jobNormalizeCommand())
	cmd.AddCommand(a.jobValidateCommand())
	cmd.AddCommand(a.jobExplainCommand())
	cmd.AddCommand(a.jobMigrateCommand())
	return cmd
}

func (a app) jobSchemaCommand() *cobra.Command {
	var out string
	var jsonReport bool
	var info bool
	cmd := &cobra.Command{
		Use:   "schema",
		Short: "Print the JSON Schema for headless job specs",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("job schema", jsonReport, func() error {
				if len(args) != 0 {
					return markError(kindInvalidInput, fmt.Errorf("job schema does not accept positional arguments"))
				}
				value := any(job.JSONSchema())
				if info {
					value = job.SchemaInfo()
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

func (a app) jobValidateCommand() *cobra.Command {
	var schema bool
	var jsonReport bool
	cmd := &cobra.Command{
		Use:   "validate INPUT",
		Short: "Validate a headless job spec",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("job validate", jsonReport, func() error {
				if err := exactArgs(args, 1, "job validate"); err != nil {
					return markError(kindInvalidInput, err)
				}
				data, name, err := a.readInput(args[0])
				if err != nil {
					return markError(kindInvalidInput, err)
				}
				if schema {
					_, err = job.LoadSchemaRenderBytes(data, name)
				} else {
					_, err = job.LoadRenderBytes(data, name)
				}
				if err != nil {
					return markError(kindValidation, err)
				}
				if jsonReport {
					return writeJSON(a.stdout, map[string]any{"ok": true, "file": name, "schema": schema})
				}
				fmt.Fprintf(a.stdout, "OK     %s\n", name)
				return nil
			})
		},
	}
	cmd.Flags().BoolVar(&schema, "schema", false, "validate with the generated JSON Schema before semantic checks")
	cmd.Flags().BoolVar(&jsonReport, "json", false, "write JSON report")
	return cmd
}

func (a app) jobMigrateCommand() *cobra.Command {
	var writePath string
	var jsonReport bool
	cmd := &cobra.Command{
		Use:   "migrate INPUT",
		Short: "Migrate a headless job spec to the latest version",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("job migrate", jsonReport, func() error {
				if err := exactArgs(args, 1, "job migrate"); err != nil {
					return markError(kindInvalidInput, err)
				}
				data, name, err := a.readInput(args[0])
				if err != nil {
					return markError(kindInvalidInput, err)
				}
				j, err := job.LoadRenderBytes(data, name)
				if err != nil {
					return markError(kindInvalidInput, err)
				}
				migrated, err := job.MigrateToLatest(j)
				if err != nil {
					return markError(kindInvalidInput, err)
				}
				if jsonReport {
					return writeJSON(a.stdout, map[string]any{"ok": true, "version": migrated.Version, "job": migrated})
				}
				output, err := marshalYAML(migrated)
				if err != nil {
					return markError(kindInternal, err)
				}
				return markError(kindRender, writeBytesPath(a.stdout, writePath, output))
			})
		},
	}
	cmd.Flags().StringVar(&writePath, "write", "-", "migrated job output path; use - for stdout")
	cmd.Flags().BoolVar(&jsonReport, "json", false, "write JSON report")
	return cmd
}

func (a app) jobInitCommand() *cobra.Command {
	var out string
	var format string
	cmd := &cobra.Command{
		Use:   "init [ID_OR_PATH_OR_URL]",
		Short: "Write a starter headless job spec",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 1 {
				return markError(kindInvalidInput, fmt.Errorf("job init accepts at most one input"))
			}
			input := "1cbs"
			if len(args) == 1 {
				input = args[0]
			}
			j, err := starterJob(input)
			if err != nil {
				return markError(kindInvalidInput, err)
			}
			switch strings.ToLower(format) {
			case "", "yaml", "yml":
				data, err := marshalYAML(j)
				if err != nil {
					return markError(kindInternal, err)
				}
				return markError(kindRender, writeBytesPath(a.stdout, out, data))
			case "json":
				data, err := marshalJSON(j)
				if err != nil {
					return markError(kindInternal, err)
				}
				return markError(kindRender, writeBytesPath(a.stdout, out, data))
			default:
				return markError(kindInvalidInput, fmt.Errorf("unsupported format %q", format))
			}
		},
	}
	cmd.Flags().StringVarP(&out, "out", "o", "-", "output path; use - for stdout")
	cmd.Flags().StringVar(&format, "format", "yaml", "output format: yaml or json")
	return cmd
}

func (a app) jobExamplesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "examples",
		Short: "List or print built-in job examples",
	}
	var listJSON bool
	list := &cobra.Command{
		Use:   "list",
		Short: "List built-in job examples",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("job examples list", listJSON, func() error {
				if len(args) != 0 {
					return markError(kindInvalidInput, fmt.Errorf("job examples list does not accept positional arguments"))
				}
				names := jobExampleNames()
				if listJSON {
					return writeJSON(a.stdout, map[string]any{"ok": true, "examples": names})
				}
				for _, name := range names {
					fmt.Fprintln(a.stdout, name)
				}
				return nil
			})
		},
	}
	list.Flags().BoolVar(&listJSON, "json", false, "write machine-readable output")
	cmd.AddCommand(list)

	var showJSON bool
	show := &cobra.Command{
		Use:   "show NAME",
		Short: "Print a built-in job example",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("job examples show", showJSON, func() error {
				if err := exactArgs(args, 1, "job examples show"); err != nil {
					return markError(kindInvalidInput, err)
				}
				j, err := namedExampleJob(args[0])
				if err != nil {
					return markError(kindInvalidInput, err)
				}
				if showJSON {
					return writeJSON(a.stdout, map[string]any{"ok": true, "name": strings.ToLower(strings.TrimSpace(args[0])), "job": j})
				}
				data, err := marshalYAML(j)
				if err != nil {
					return markError(kindInternal, err)
				}
				_, err = a.stdout.Write(data)
				return markError(kindRender, err)
			})
		},
	}
	show.Flags().BoolVar(&showJSON, "json", false, "write machine-readable output")
	cmd.AddCommand(show)
	return cmd
}

func jobExampleNames() []string {
	return []string{"default", "ligand", "locked-local"}
}

func (a app) jobExplainCommand() *cobra.Command {
	flags := &renderFlags{}
	var jsonReport bool
	cmd := &cobra.Command{
		Use:   "explain INPUT",
		Short: "Explain fetch/cache/render actions without rendering",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("job explain", jsonReport, func() error {
				if err := exactArgs(args, 1, "job explain"); err != nil {
					return markError(kindInvalidInput, err)
				}
				j, err := a.loadJobForNormalize(args[0], flags, cmd)
				if err != nil {
					return err
				}
				report := explainJobReport(j)
				if jsonReport {
					return writeJSON(a.stdout, report)
				}
				return writeYAML(a.stdout, report)
			})
		},
	}
	bindNormalizeFlags(cmd, flags)
	cmd.Flags().BoolVar(&jsonReport, "json", true, "write JSON report")
	return cmd
}

func mvsCompileForExplain(j job.Job) (int, error) {
	result, err := mvs.Compile(j)
	if err != nil {
		return 0, err
	}
	var count func(mvs.Node) int
	count = func(node mvs.Node) int {
		total := 1
		for _, child := range node.Children {
			total += count(child)
		}
		return total
	}
	return count(result.Document.Root), nil
}

func (a app) jobNormalizeCommand() *cobra.Command {
	flags := &renderFlags{}
	var writePath string
	var format string
	var jsonReport bool
	cmd := &cobra.Command{
		Use:   "normalize INPUT",
		Short: "Print a canonical JSON/YAML job spec from flags or a job file",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("job normalize", jsonReport, func() error {
				if err := exactArgs(args, 1, "job normalize"); err != nil {
					return markError(kindInvalidInput, err)
				}
				if jsonReport {
					format = "json"
				}
				j, err := a.loadJobForNormalize(args[0], flags, cmd)
				if err != nil {
					return err
				}
				switch strings.ToLower(format) {
				case "", "json":
					data, err := marshalJSON(j)
					if err != nil {
						return markError(kindInternal, err)
					}
					return markError(kindRender, writeBytesPath(a.stdout, writePath, data))
				case "yaml", "yml":
					data, err := marshalYAML(j)
					if err != nil {
						return markError(kindInternal, err)
					}
					return markError(kindRender, writeBytesPath(a.stdout, writePath, data))
				default:
					return markError(kindInvalidInput, fmt.Errorf("unsupported format %q", format))
				}
			})
		},
	}
	bindNormalizeFlags(cmd, flags)
	cmd.Flags().StringVar(&writePath, "write", "-", "normalized job output path; use - for stdout")
	cmd.Flags().StringVar(&format, "format", "json", "output format: json or yaml")
	cmd.Flags().BoolVar(&jsonReport, "json", false, "force the normalized job format to JSON and write JSON errors if the command fails")
	return cmd
}

func bindNormalizeFlags(cmd *cobra.Command, flags *renderFlags) {
	cmd.Flags().StringVarP(&flags.out, "out", "o", "", "image/movie output path to place in the normalized job")
	cmd.Flags().StringVar(&flags.provider, "provider", "pdbe", "identifier provider: pdbe, rcsb, alphafold")
	cmd.Flags().StringVar(&flags.format, "format-input", "", "input structure format override")
	cmd.Flags().StringVar(&flags.assembly, "assembly", "", "assembly identifier")
	cmd.Flags().StringVar(&flags.background, "background", "white", "canvas background color")
	cmd.Flags().StringVar(&flags.focus, "focus", "", "component ref or selector to focus")
	cmd.Flags().StringVar(&flags.view, "view", "", "standard view: front, back, top, bottom, left, right")
	cmd.Flags().StringVar(&flags.size, "size", "800x800", "output size as WIDTHxHEIGHT")
	cmd.Flags().StringVar(&flags.preset, "preset", "default", "render preset: default, ligand, polymer, surface, confidence, overview")
	cmd.Flags().StringArrayVar(&flags.selectors, "select", nil, "component selector; repeat to add components")
	cmd.Flags().StringArrayVar(&flags.representations, "repr", nil, "representation for matching --select")
	cmd.Flags().StringArrayVar(&flags.colors, "color", nil, "color or high-level theme for matching --select")
	bindRuntimeFlags(cmd, &flags.runtime)
}

func (a app) loadJobForNormalize(input string, flags *renderFlags, cmd *cobra.Command) (job.Job, error) {
	var j job.Job
	var err error
	if input == "-" {
		data, name, readErr := a.readInput(input)
		if readErr != nil {
			return job.Job{}, markError(kindInvalidInput, readErr)
		}
		j, err = a.loadJobOrRecipeBytes(data, name, false)
	} else if job.PathExists(input) && !looksLikeStructureInput(input) {
		data, _, readErr := a.readInput(input)
		if readErr != nil {
			return job.Job{}, markError(kindInvalidInput, readErr)
		}
		j, err = a.loadJobOrRecipeBytes(data, input, false)
	} else {
		j, err = a.loadOrBuildJob(input, flags)
	}
	if err != nil {
		return job.Job{}, markError(kindInvalidInput, err)
	}
	applyRuntimeFlags(cmd, &j, flags.runtime)
	if cmd.Flags().Changed("out") {
		size, err := parseSize(flags.size)
		if err != nil {
			return job.Job{}, markError(kindInvalidInput, err)
		}
		j.Outputs = []job.Output{{
			Type: outputTypeFromPath(flags.out),
			Path: flags.out,
			Size: size,
		}}
	}
	return j, nil
}

func starterJob(input string) (job.Job, error) {
	flags := &renderFlags{
		provider:   "pdbe",
		background: "white",
		size:       "1200x900",
		preset:     "default",
	}
	components, focus, err := componentsForPreset(flags)
	if err != nil {
		return job.Job{}, err
	}
	source := job.Input{ID: input, Provider: "pdbe"}
	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		source = job.Input{URL: input}
	} else if job.PathExists(input) || looksLikeStructureInput(input) {
		source = job.Input{Path: input}
	}
	return job.Job{
		Version: 1,
		Runtime: job.Runtime{
			Profile: "ci",
			Cache:   ".molstar-cache",
			Strict:  true,
		},
		Inputs: map[string]job.Input{
			"input": source,
		},
		Scene: job.Scene{
			Canvas: job.Canvas{Background: "white"},
			Structures: []job.Structure{{
				Ref:        "structure",
				Source:     "input",
				Components: components,
			}},
			Camera: job.Camera{Focus: focus, View: "front", Zoom: 1.2},
		},
		Outputs: []job.Output{{
			Type: "image",
			Path: "outputs/render.png",
			Size: []int{1200, 900},
		}, {
			Type: "mvsj",
			Path: "outputs/scene.mvsj",
		}},
	}, nil
}

func namedExampleJob(name string) (job.Job, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "default":
		return starterJob("1cbs")
	case "ligand":
		j, err := starterJob("1cbs")
		if err != nil {
			return job.Job{}, err
		}
		j.Scene.Structures[0].Components = []job.Component{
			{Ref: "polymer", Select: "polymer", Representation: job.Representation{Type: "cartoon", Color: "#dddddd"}},
			{Ref: "ligand", Select: "ligand", Representation: job.Representation{Type: "ball-and-stick", Color: "element"}},
		}
		j.Scene.Camera.Focus = "ligand"
		return j, nil
	case "locked-local":
		j, err := starterJob("model.cif")
		if err != nil {
			return job.Job{}, err
		}
		j.Runtime = job.Runtime{Profile: "locked", Strict: true}
		j.Outputs = append(j.Outputs, job.Output{Type: "mvsx", Path: "outputs/scene.mvsx"})
		return j, nil
	default:
		return job.Job{}, fmt.Errorf("unknown example %q", name)
	}
}
