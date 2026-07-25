package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/sacha-ichbiah/molstar/internal/mvs"
)

func (a app) selectorsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "selectors",
		Short: "Inspect the selector DSL",
	}
	cmd.AddCommand(a.selectorsListCommand())
	cmd.AddCommand(a.selectorsExplainCommand())
	return cmd
}

func (a app) selectorsListCommand() *cobra.Command {
	var jsonReport bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List selector DSL examples",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("selectors list", jsonReport, func() error {
				if len(args) != 0 {
					return markError(kindInvalidInput, fmt.Errorf("selectors list does not accept positional arguments"))
				}
				examples := mvs.SelectorExamples()
				if jsonReport {
					return writeJSON(a.stdout, map[string]any{"ok": true, "selectors": examples})
				}
				for _, example := range examples {
					fmt.Fprintf(a.stdout, "%-28s %s\n", example.Selector, example.Description)
				}
				return nil
			})
		},
	}
	cmd.Flags().BoolVar(&jsonReport, "json", false, "write JSON report")
	return cmd
}

func (a app) selectorsExplainCommand() *cobra.Command {
	var jsonReport bool
	cmd := &cobra.Command{
		Use:   "explain SELECTOR",
		Short: "Explain how a selector compiles to MVS",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("selectors explain", jsonReport, func() error {
				if err := exactArgs(args, 1, "selectors explain"); err != nil {
					return markError(kindInvalidInput, err)
				}
				explanation, err := mvs.ExplainSelector(args[0])
				if err != nil {
					return markError(kindInvalidInput, err)
				}
				if jsonReport {
					return writeJSON(a.stdout, map[string]any{"ok": true, "explanation": explanation})
				}
				return writeYAML(a.stdout, explanation)
			})
		},
	}
	cmd.Flags().BoolVar(&jsonReport, "json", false, "write JSON report")
	return cmd
}
