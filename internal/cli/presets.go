package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rafflesia-ai/molstar/internal/job"
)

type presetDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Focus       string          `json:"focus,omitempty"`
	Components  []job.Component `json:"components"`
}

func (a app) presetsCommand() *cobra.Command {
	var jsonReport bool
	cmd := &cobra.Command{
		Use:   "presets",
		Short: "Inspect built-in render presets",
	}
	list := &cobra.Command{
		Use:   "list",
		Short: "List built-in render presets",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("presets list", jsonReport, func() error {
				if len(args) != 0 {
					return markError(kindInvalidInput, fmt.Errorf("presets list does not accept positional arguments"))
				}
				presets := allPresetDefinitions()
				if jsonReport {
					return writeJSON(a.stdout, map[string]any{"ok": true, "presets": presets})
				}
				for _, preset := range presets {
					fmt.Fprintf(a.stdout, "%s\t%s\n", preset.Name, preset.Description)
				}
				return nil
			})
		},
	}
	show := &cobra.Command{
		Use:   "show PRESET",
		Short: "Show a built-in render preset",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("presets show", jsonReport, func() error {
				if err := exactArgs(args, 1, "presets show"); err != nil {
					return markError(kindInvalidInput, err)
				}
				preset, ok := presetDefinitionByName(args[0])
				if !ok {
					return markError(kindInvalidInput, fmt.Errorf("unsupported preset %q", args[0]))
				}
				if jsonReport {
					return writeJSON(a.stdout, map[string]any{"ok": true, "preset": preset})
				}
				return writeYAML(a.stdout, preset)
			})
		},
	}
	for _, sub := range []*cobra.Command{list, show} {
		sub.Flags().BoolVar(&jsonReport, "json", false, "write machine-readable output")
	}
	cmd.AddCommand(list, show)
	return cmd
}

func allPresetDefinitions() []presetDefinition {
	return []presetDefinition{
		{
			Name:        "default",
			Description: "Cartoon polymer colored by chain plus ball-and-stick ligands.",
			Components: []job.Component{
				{Ref: "polymer", Select: "polymer", Representation: job.Representation{Type: "cartoon", Color: "chain"}},
				{Ref: "ligand", Select: "ligand", Representation: job.Representation{Type: "ball-and-stick", Color: "element"}},
			},
		},
		{
			Name:        "ligand",
			Description: "Muted polymer context with ligands focused in element colors.",
			Focus:       "ligand",
			Components: []job.Component{
				{Ref: "polymer", Select: "polymer", Representation: job.Representation{Type: "cartoon", Color: "#dddddd"}},
				{Ref: "ligand", Select: "ligand", Representation: job.Representation{Type: "ball-and-stick", Color: "element"}},
			},
		},
		{
			Name:        "polymer",
			Description: "Single polymer cartoon colored by chain and focused on the polymer.",
			Focus:       "polymer",
			Components: []job.Component{
				{Ref: "polymer", Select: "polymer", Representation: job.Representation{Type: "cartoon", Color: "chain"}},
			},
		},
		{
			Name:        "surface",
			Description: "Light polymer molecular surface with ligands shown in ball-and-stick.",
			// Focus the polymer, not the ligand. A molecular surface is opaque, so
			// framing a buried ligand puts the camera inside the surface and the
			// render shows an unreadable interior instead of the surface.
			Focus: "polymer",
			Components: []job.Component{
				{Ref: "polymer", Select: "polymer", Representation: job.Representation{Type: "surface", Color: "#d8d8d8"}},
				{Ref: "ligand", Select: "ligand", Representation: job.Representation{Type: "ball-and-stick", Color: "element"}},
			},
		},
		{
			Name:        "confidence",
			Description: "Polymer cartoon colored by model uncertainty/confidence with ligand context.",
			Components: []job.Component{
				{Ref: "polymer", Select: "polymer", Representation: job.Representation{Type: "cartoon", Color: "plddt"}},
				{Ref: "ligand", Select: "ligand", Representation: job.Representation{Type: "ball-and-stick", Color: "element"}},
			},
		},
		{
			Name:        "overview",
			Description: "Broad scene overview with polymer, nucleic acid, ligand, and ion components.",
			Focus:       "all",
			Components: []job.Component{
				{Ref: "polymer", Select: "polymer", Representation: job.Representation{Type: "cartoon", Color: "chain"}},
				{Ref: "nucleic", Select: "nucleic", Representation: job.Representation{Type: "cartoon", Color: "chain"}},
				{Ref: "ligand", Select: "ligand", Representation: job.Representation{Type: "ball-and-stick", Color: "element"}},
				{Ref: "ion", Select: "ion", Representation: job.Representation{Type: "spacefill", Color: "element"}},
			},
		},
	}
}

func presetDefinitionByName(name string) (presetDefinition, bool) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		normalized = "default"
	}
	for _, preset := range allPresetDefinitions() {
		if preset.Name == normalized {
			return preset, true
		}
	}
	return presetDefinition{}, false
}
