package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

type exampleDefinition struct {
	Name        string   `json:"name" yaml:"name"`
	Description string   `json:"description" yaml:"description"`
	Command     string   `json:"command" yaml:"command"`
	Network     bool     `json:"network" yaml:"network"`
	Outputs     []string `json:"outputs,omitempty" yaml:"outputs,omitempty"`
}

func (a app) examplesCommand() *cobra.Command {
	var jsonReport bool
	cmd := &cobra.Command{
		Use:   "examples",
		Short: "Show copy-pasteable headless Mol* examples",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("examples", jsonReport, func() error {
				if len(args) != 0 {
					return markError(kindInvalidInput, fmt.Errorf("examples does not accept positional arguments; use examples show NAME"))
				}
				examples := allExampleDefinitions()
				if jsonReport {
					return writeJSON(a.stdout, map[string]any{"ok": true, "examples": examples})
				}
				for _, example := range examples {
					network := "local"
					if example.Network {
						network = "network"
					}
					fmt.Fprintf(a.stdout, "%-12s %-7s %s\n", example.Name, network, example.Description)
					fmt.Fprintf(a.stdout, "             %s\n", example.Command)
				}
				return nil
			})
		},
	}
	cmd.Flags().BoolVar(&jsonReport, "json", false, "write machine-readable output")
	show := &cobra.Command{
		Use:   "show NAME",
		Short: "Show one example",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("examples show", jsonReport, func() error {
				if err := exactArgs(args, 1, "examples show"); err != nil {
					return markError(kindInvalidInput, err)
				}
				example, ok := exampleDefinitionByName(args[0])
				if !ok {
					return markError(kindInvalidInput, fmt.Errorf("unknown example %q", args[0]))
				}
				if jsonReport {
					return writeJSON(a.stdout, map[string]any{"ok": true, "example": example})
				}
				return writeYAML(a.stdout, example)
			})
		},
	}
	show.Flags().BoolVar(&jsonReport, "json", false, "write machine-readable output")
	cmd.AddCommand(show)
	return cmd
}

func allExampleDefinitions() []exampleDefinition {
	return []exampleDefinition{
		{
			Name:        "demo",
			Description: "Render a tiny built-in local fixture, print a summary, and open it.",
			Command:     "molstar render --demo --out molstar-demo.png --show-report --open",
			Network:     false,
			Outputs:     []string{"molstar-demo.png"},
		},
		{
			Name:        "identifier",
			Description: "Fetch a PDB identifier, cache it, and render a ligand-focused image.",
			Command:     "molstar render 1cbs --preset ligand --cache .molstar-cache --out 1cbs.png --show-report",
			Network:     true,
			Outputs:     []string{"1cbs.png"},
		},
		{
			Name:        "report",
			Description: "Render once and save the full JSON report for CI artifacts.",
			Command:     "molstar render --demo --out outputs/demo.png --report outputs/demo.report.json --show-report",
			Network:     false,
			Outputs:     []string{"outputs/demo.png", "outputs/demo.report.json"},
		},
		{
			Name:        "recipe",
			Description: "Create a friendly ligand recipe, then render it directly.",
			Command:     "molstar recipe init ligand --id 1cbs --out ligand.recipe.yaml && molstar render ligand.recipe.yaml --show-report",
			Network:     true,
			Outputs:     []string{"1cbs-ligand.png"},
		},
		{
			Name:        "confidence",
			Description: "Render an AlphaFold model colored by confidence.",
			Command:     "molstar render examples/alphafold-confidence.recipe.yaml",
			Network:     true,
			Outputs:     []string{"outputs/P05067-confidence.png"},
		},
		{
			Name:        "surface",
			Description: "Render a surface preset with ligand context.",
			Command:     "molstar render examples/surface.recipe.yaml",
			Network:     true,
			Outputs:     []string{"outputs/1cbs-surface.png"},
		},
		{
			Name:        "selectors",
			Description: "Use chain, ligand, and within-radius selector DSL expressions.",
			Command:     "molstar recipe compile examples/selector-dsl.recipe.yaml --out outputs/selector-dsl.mvsj",
			Network:     true,
			Outputs:     []string{"outputs/selector-dsl.mvsj"},
		},
		{
			Name:        "compile",
			Description: "Compile a job YAML file to a reproducible MVSJ scene.",
			Command:     "molstar scene compile examples/1cbs.job.yaml --out outputs/1cbs.mvsj",
			Network:     true,
			Outputs:     []string{"outputs/1cbs.mvsj"},
		},
		{
			Name:        "state",
			Description: "Render an image and save the matching Mol* state snapshot.",
			Command:     "molstar render examples/1cbs.job.yaml --out outputs/1cbs.png --state outputs/1cbs.molj",
			Network:     true,
			Outputs:     []string{"outputs/1cbs.png", "outputs/1cbs.molj"},
		},
		{
			Name:        "batch",
			Description: "Render multiple job specs with retries, skip-existing, and a manifest.",
			Command:     "molstar batch jobs.jsonl --concurrency 4 --retries 1 --skip-existing --manifest renders/manifest.json --out renders/{id}-{index}.{ext}",
			Network:     true,
			Outputs:     []string{"renders/"},
		},
		{
			Name:        "server",
			Description: "Run the HTTP job server with bounded workers and persisted job records.",
			Command:     "molstar serve --workers 2 --queue 8 --job-store .molstar-jobs",
			Network:     true,
			Outputs:     []string{".molstar-jobs/"},
		},
		{
			Name:        "submit-wait",
			Description: "Submit a recipe to a running server, wait, and download outputs.",
			Command:     "molstar server submit examples/ligand.recipe.yaml --url http://127.0.0.1:8080 --wait --download-outputs --out-dir renders",
			Network:     true,
			Outputs:     []string{"renders/"},
		},
		{
			Name:        "release",
			Description: "Build and verify the local distributable artifact.",
			Command:     "npm run package:local && npm run test:artifact",
			Network:     false,
			Outputs:     []string{"dist/headlessmolstar-local-*.tar.gz"},
		},
	}
}

func exampleDefinitionByName(name string) (exampleDefinition, bool) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	for _, example := range allExampleDefinitions() {
		if example.Name == normalized {
			return example, true
		}
	}
	return exampleDefinition{}, false
}
